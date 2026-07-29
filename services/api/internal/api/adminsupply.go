package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/admin"
)

// The supply endpoints — PLAN-ADMIN-CONSOLE §4.4.
//
// The one that matters most is `ReelMedia`. Until it existed, a moderator was
// asked to approve or reject a video they could not watch: the queue gave them a
// caption, a poster key and a report count. Approving on a thumbnail is not
// moderation, it is a coin toss with an audit trail attached.

// MediaSigner issues short-lived URLs for objects that are not public.
//
// An interface rather than the concrete store, and named for what the console
// needs rather than what S3 offers, so that the console can only *read*. There
// is no upload method on this type and there will not be one — a staff tool that
// can write to the media bucket is a staff tool that can replace a vendor's
// video with something else.
type MediaSigner interface {
	PresignDownload(ctx context.Context, key string, ttl time.Duration) (string, error)
}

// mediaTTL is how long a staff playback URL lives.
//
// Two minutes. Long enough to start playing a sixty-second reel and seek within
// it, short enough that a URL pasted into a chat is dead before anyone clicks
// it. The alternative — an hour, for convenience — turns every moderation
// session into a set of shareable links to unpublished private video.
const mediaTTL = 2 * time.Minute

func (h *AdminHandlers) Reel(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermContentReview)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "reelId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	reel, err := h.View.LoadReel(r.Context(), id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	renditions := make([]map[string]any, 0, len(reel.Renditions))
	for _, rd := range reel.Renditions {
		renditions = append(renditions, map[string]any{
			"label": rd.Label, "width": rd.Width, "height": rd.Height,
			"bitrate_kbps": rd.BitrateKbps, "playlist_url": rd.PlaylistURL,
		})
	}

	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"id":           reel.ID,
		"state":        reel.State,
		"visibility":   reel.Visibility,
		"caption":      nilIfBlank(reel.Caption),
		"locale":       nilIfBlank(reel.Locale),
		"author_type":  reel.AuthorType,
		"author_id":    reel.AuthorID,
		"author":       reel.AuthorName,
		"category_id":  reel.CategoryID,
		"duration_ms":  reel.DurationMS,
		"created_at":   reel.CreatedAt,
		"published_at": reel.PublishedAt,
		"removed_at":   reel.RemovedAt,
		// The pipeline in the order it runs. Four timestamps and a failure code
		// are the whole diagnostic for "why is this stuck" — a reel that never
		// probed failed somewhere different to one that probed and never
		// transcoded, and a state name alone cannot tell them apart.
		"pipeline": map[string]any{
			"bytes_ready_at":    reel.BytesReadyAt,
			"metadata_ready_at": reel.MetadataReadyAt,
			"upload_expires_at": reel.UploadExpiresAt,
			"probed_at":         reel.ProbedAt,
			"attempts":          reel.Attempts,
			"failure_code":      nilIfBlank(reel.FailureCode),
			"failure_detail":    nilIfBlank(reel.FailureDetail),
		},
		"asset": map[string]any{
			"id":        reel.AssetID,
			"mime_type": nilIfBlank(reel.MimeType),
			"bytes":     reel.Bytes,
			"width":     reel.Width,
			"height":    reel.Height,
			// The key itself, not a URL. Signing happens on the separate
			// endpoint below so that *looking at this page* is not the same
			// event as *watching the video*, and the audit trail can tell them
			// apart.
			"has_source": reel.S3Key != "",
			"has_poster": reel.PosterKey != "",
		},
		"coverage": map[string]any{
			"kind":     nilIfBlank(reel.CoverageKind),
			"resolved": reel.HasCoverage,
		},
		"renditions": renditions,
		"reports":    reel.Reports,
		"engagement": map[string]any{
			"shares":   reel.Engagement.Shares,
			"views":    reel.Engagement.Views,
			"likes":    reel.Engagement.Likes,
			"comments": reel.Engagement.Comments,
			"saves":    reel.Engagement.Saves,
		},
	})

	h.recordIfSensitive(r, grant, lensFor(grant), "reel", id)
}

func (h *AdminHandlers) ReelTimeline(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermContentReview); !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "reelId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	entries, err := h.View.ReelTimeline(r.Context(), id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": timelineJSON(entries)})
}

// ReelMedia issues a short-lived playback URL for staff.
//
// Three things about this endpoint are deliberate.
//
// It is **separate from the detail page**, so that opening a reel and watching
// it are different events. "Who looked at this video" is a question that gets
// asked after a complaint, and a single endpoint would answer it wrongly for
// everyone who merely loaded the page.
//
// It **always records**, regardless of the caller's lens. Everything else in
// this console audits reads of PII; this audits reads of *content*, which is
// somebody's unpublished video and is at least as sensitive. The recording is
// not conditional on a permission because the sensitivity is not conditional on
// who is looking.
//
// It **signs the source, not a rendition**. A moderator needs to see what was
// uploaded, not what survived transcoding — a video whose transcode dropped the
// last four seconds is exactly the case where the two differ and exactly the
// case where it matters.
func (h *AdminHandlers) ReelMedia(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermContentReview)
	if !ok {
		return
	}
	if h.Media == nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "reelId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	ref, err := h.View.ResolveReelMedia(r.Context(), id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	if ref.S3Key == "" {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	want := r.URL.Query().Get("kind")
	key := ref.S3Key
	if want == "poster" && ref.PosterKey != "" {
		key = ref.PosterKey
	}

	url, err := h.Media.PresignDownload(r.Context(), key, mediaTTL)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	h.Admin.RecordAccess(r.Context(), admin.Access{
		ActorID:    grant.UserID,
		Permission: admin.PermContentReview,
		TargetType: "reel_media",
		TargetID:   id,
		IP:         clientIP(r),
		UserAgent:  r.UserAgent(),
		RequestID:  httpx.RequestIDFromContext(r.Context()),
	})
	slog.InfoContext(r.Context(), "staff media access",
		"actor", grant.UserID, "reel", id, "key", key)

	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"url":        url,
		"mime_type":  nilIfBlank(ref.MimeType),
		"expires_in": int(mediaTTL.Seconds()),
		"state":      ref.State,
	})
}

func (h *AdminHandlers) SupplyGaps(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermContentReview); !ok {
		return
	}

	gaps, err := h.View.LoadSupplyGaps(r.Context(),
		intParam(r.URL.Query().Get("quiet_days"), 14),
		intParam(r.URL.Query().Get("limit"), 100))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	items := make([]map[string]any, 0, len(gaps))
	for _, g := range gaps {
		items = append(items, map[string]any{
			"vendor_id":      g.VendorID,
			"vendor":         g.Vendor,
			"city":           nilIfBlank(g.City),
			"published":      g.Published,
			"pending_review": g.PendingReview,
			"last_published": g.LastPublished,
			// Null rather than a large number for "never published". They are
			// different problems and the more urgent one is the null; merging
			// them into 999 days sorts them together and hides that.
			"days_quiet": g.DaysQuiet,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

func (h *AdminHandlers) Coverage(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermContentReview); !ok {
		return
	}
	areas, err := h.View.LoadCoverage(r.Context())
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	items := make([]map[string]any, 0, len(areas))
	for _, a := range areas {
		items = append(items, map[string]any{
			"emirate": a.Emirate, "city": a.City,
			"reels": a.Reels, "vendors": a.Vendors,
			"last_published": a.LastPublic,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}
