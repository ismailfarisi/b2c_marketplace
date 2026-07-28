package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/engagement"
	"github.com/marketplace/api/internal/modules/feed"
	"github.com/marketplace/api/internal/modules/moderation"
	"github.com/marketplace/api/internal/modules/reels"
)

// ReelHandlers covers the Phase 2 surface: authoring, the feed, reporting,
// event ingest and the moderation queue.
type ReelHandlers struct {
	Reels      *reels.Service
	Feed       *feed.Service
	Ingest     *engagement.Ingest
	Moderation *moderation.Service
}

// --- authoring ---------------------------------------------------------------

type createUploadBody struct {
	BranchID    uuid.UUID  `json:"branch_id"`
	ContentType string     `json:"content_type"`
	SubjectType *string    `json:"subject_type"`
	SubjectID   *uuid.UUID `json:"subject_id"`
}

func (h *ReelHandlers) CreateUpload(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}

	var body createUploadBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	if body.ContentType == "" {
		body.ContentType = "video/mp4"
	}

	upload, err := h.Reels.RequestUpload(r.Context(), reels.UploadRequest{
		UserID:      userID,
		BranchID:    body.BranchID,
		ContentType: body.ContentType,
		SubjectType: body.SubjectType,
		SubjectID:   body.SubjectID,
	})
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, "", map[string]any{
		"reel_id":    upload.ReelID,
		"asset_id":   upload.AssetID,
		"upload_url": upload.URL,
		// Returned explicitly because a presigned URL signs the headers it was
		// created with: a client that omits Content-Type gets a signature
		// mismatch that reads like an auth failure and is not one.
		"upload_headers": upload.Headers,
		"expires_at":     upload.ExpiresAt.Format(time.RFC3339),
	})
}

type reelMetadataBody struct {
	Caption       string     `json:"caption"`
	CaptionLocale string     `json:"caption_locale"`
	CategoryID    *uuid.UUID `json:"category_id"`
	Tags          []string   `json:"tags"`
	Visibility    string     `json:"visibility"`
	SubjectType   *string    `json:"subject_type"`
	SubjectID     *uuid.UUID `json:"subject_id"`
}

func (h *ReelHandlers) UpdateReel(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	reelID, err := uuid.Parse(chi.URLParam(r, "reelId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	var body reelMetadataBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	reel, err := h.Reels.SubmitMetadata(r.Context(), userID, reelID, reels.MetadataInput{
		Caption:       body.Caption,
		CaptionLocale: body.CaptionLocale,
		CategoryID:    body.CategoryID,
		Tags:          body.Tags,
		Visibility:    body.Visibility,
		SubjectType:   body.SubjectType,
		SubjectID:     body.SubjectID,
	})
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, "", authorReelResponse(reel))
}

func (h *ReelHandlers) GetReel(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	reelID, err := uuid.Parse(chi.URLParam(r, "reelId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	reel, err := h.Reels.Get(r.Context(), userID, reelID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", authorReelResponse(reel))
}

func (h *ReelHandlers) DeleteReel(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	reelID, err := uuid.Parse(chi.URLParam(r, "reelId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	if err := h.Reels.Remove(r.Context(), userID, reelID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type reportBody struct {
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}

func (h *ReelHandlers) ReportReel(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	reelID, err := uuid.Parse(chi.URLParam(r, "reelId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	var body reportBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	if err := h.Reels.Report(r.Context(), userID, reelID, body.Reason, body.Detail); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	// 202 rather than 201: the report is accepted, and what happens to it is a
	// moderator's decision, not this request's outcome.
	httpx.WriteJSON(w, http.StatusAccepted, "", map[string]any{"status": "received"})
}

// --- feed --------------------------------------------------------------------

func (h *ReelHandlers) GetFeed(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	lat, latErr := strconv.ParseFloat(q.Get("lat"), 64)
	lng, lngErr := strconv.ParseFloat(q.Get("lng"), 64)
	if latErr != nil || lngErr != nil {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "lat", Code: "required",
			Message: "lat and lng are required — the feed is local by definition",
		}}})
		return
	}

	cursor, err := feed.DecodeCursor(q.Get("cursor"))
	if err != nil {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "cursor", Code: "malformed",
		}}})
		return
	}

	var category *uuid.UUID
	if raw := q.Get("category"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "category", Code: "malformed",
			}}})
			return
		}
		category = &id
	}

	limit, _ := strconv.Atoi(q.Get("limit"))

	query := feed.Query{
		Lat:      lat,
		Lng:      lng,
		Category: category,
		Cursor:   cursor,
		Limit:    limit,
	}

	// Optional auth: a signed-in viewer gets block filtering and a durable
	// seen-set; an anonymous one gets the feed keyed by device. Requiring login
	// here would put a wall in front of the product's front door.
	if userID, ok := httpx.UserIDFromContext(r.Context()); ok {
		query.ViewerID = uuid.NullUUID{UUID: userID, Valid: true}
		query.ViewerKey = "u:" + userID.String()
	} else if device := r.Header.Get("X-Device-Id"); device != "" {
		query.ViewerKey = "d:" + device
	}

	page, err := h.Feed.Get(r.Context(), query)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	items := make([]map[string]any, 0, len(page.Items))
	for _, it := range page.Items {
		items = append(items, feedItemResponse(it))
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"items":       items,
		"next_cursor": nullable(page.NextCursor),
	})
}

// --- events ------------------------------------------------------------------

type eventsBody struct {
	SessionID string `json:"session_id"`
	Events    []struct {
		Type       string         `json:"type"`
		ReelID     *uuid.UUID     `json:"reel_id"`
		OccurredAt string         `json:"occurred_at"`
		WatchedMS  int            `json:"watched_ms"`
		Properties map[string]any `json:"properties"`
	} `json:"events"`
}

func (h *ReelHandlers) SubmitEvents(w http.ResponseWriter, r *http.Request) {
	var body eventsBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	var fieldErrs []httpx.FieldError
	events := make([]engagement.Event, 0, len(body.Events))
	for i, e := range body.Events {
		at, err := time.Parse(time.RFC3339, e.OccurredAt)
		if err != nil {
			fieldErrs = append(fieldErrs, httpx.FieldError{
				Field: "events[" + strconv.Itoa(i) + "].occurred_at",
				Code:  "invalid_timestamp",
			})
			continue
		}
		events = append(events, engagement.Event{
			Type:       e.Type,
			ReelID:     e.ReelID,
			SessionID:  body.SessionID,
			OccurredAt: at,
			WatchedMS:  e.WatchedMS,
			Properties: e.Properties,
		})
	}
	if len(fieldErrs) > 0 {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: fieldErrs})
		return
	}

	var userID uuid.NullUUID
	if id, ok := httpx.UserIDFromContext(r.Context()); ok {
		userID = uuid.NullUUID{UUID: id, Valid: true}
	}

	if err := h.Ingest.Submit(r.Context(), events, userID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// --- moderation (admin) -------------------------------------------------------

func (h *ReelHandlers) ModerationQueue(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	items, err := h.Moderation.Queue(r.Context(), userID, limit)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		out = append(out, map[string]any{
			"reel_id":     it.ReelID,
			"author_type": it.AuthorType,
			"author_id":   it.AuthorID,
			"caption":     it.Caption,
			"poster_key":  it.PosterKey,
			"categories":  it.Categories,
			"tier":        it.Tier,
			"reports":     it.ReportCount,
			"entered_at":  it.EnteredAt.Format(time.RFC3339),
			// Surfaced so a queue that is breaching its two-hour SLA is
			// visible in the tool the moderator already has open, rather than
			// only in a dashboard nobody watches.
			"waiting_seconds": int(it.WaitingFor.Seconds()),
			"sla_breached":    it.WaitingFor > moderation.ReviewSLA,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": out})
}

type decideBody struct {
	Decision string `json:"decision"`
	Notes    string `json:"notes"`
}

func (h *ReelHandlers) DecideModeration(w http.ResponseWriter, r *http.Request) {
	moderatorID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	reelID, err := uuid.Parse(chi.URLParam(r, "reelId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	var body decideBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	if err := h.Moderation.Decide(r.Context(), moderatorID, reelID,
		moderation.Verdict(body.Decision), body.Notes); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"status": "recorded"})
}

// --- response shapes ---------------------------------------------------------

// authorReelResponse is the author's view. It includes the failure code and
// detail on purpose: exit criterion #2 is that a rejected author sees *why*, and
// "your reel was rejected" with no reason is the single most common complaint
// about every content platform.
func authorReelResponse(r *reels.Reel) map[string]any {
	out := map[string]any{
		"id":           r.ID,
		"state":        r.State,
		"visibility":   r.Visibility,
		"caption":      r.Caption,
		"tags":         r.Tags,
		"poster_key":   nullable(r.PosterKey),
		"preview_key":  nullable(r.PreviewKey),
		"lqip":         nullable(r.LQIP),
		"duration_ms":  r.DurationMS,
		"created_at":   r.CreatedAt.Format(time.RFC3339),
		"published_at": nil,
	}
	if r.PublishedAt != nil {
		out["published_at"] = r.PublishedAt.Format(time.RFC3339)
	}
	if r.FailureCode != "" {
		out["failure"] = map[string]any{
			"code":   r.FailureCode,
			"detail": r.FailureDetail,
		}
	}
	return out
}

func feedItemResponse(it feed.Item) map[string]any {
	renditions := make([]map[string]any, 0, len(it.Renditions))
	for _, r := range it.Renditions {
		renditions = append(renditions, map[string]any{
			"label":        r.Label,
			"width":        r.Width,
			"height":       r.Height,
			"bitrate_kbps": r.BitrateKbps,
			"playlist_url": r.PlaylistURL,
		})
	}

	return map[string]any{
		"id": it.ID,
		"author": map[string]any{
			"type": it.AuthorType,
			"id":   it.AuthorID,
			"name": it.AuthorName,
		},
		"subject_type": it.SubjectType,
		"subject_id":   it.SubjectID,
		"category_id":  it.CategoryID,
		"caption":      it.Caption,
		"duration_ms":  it.DurationMS,
		"poster_key":   it.PosterKey,
		"preview_key":  it.PreviewKey,
		"lqip":         it.LQIP,
		"renditions":   renditions,
		"counters": map[string]any{
			"views":    it.Counters.Views,
			"likes":    it.Counters.Likes,
			"comments": it.Counters.Comments,
			"shares":   it.Counters.Shares,
			"saves":    it.Counters.Saves,
		},
		"published_at": it.PublishedAt.Format(time.RFC3339),
	}
}
