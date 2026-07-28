package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/actions"
	"github.com/marketplace/api/internal/modules/attribution"
	"github.com/marketplace/api/internal/modules/boosts"
	"github.com/marketplace/api/internal/modules/comments"
	"github.com/marketplace/api/internal/modules/engagement"
	"github.com/marketplace/api/internal/modules/feed"
	"github.com/marketplace/api/internal/modules/interests"
	"github.com/marketplace/api/internal/modules/moderation"
	"github.com/marketplace/api/internal/modules/ranking"
	"github.com/marketplace/api/internal/modules/reels"
	"github.com/marketplace/api/internal/modules/social"
	"github.com/marketplace/api/internal/seam/action"
)

// ReelHandlers covers the Phase 2 surface: authoring, the feed, reporting,
// event ingest and the moderation queue.
type ReelHandlers struct {
	Reels      *reels.Service
	Feed       *feed.Service
	Ingest     *engagement.Ingest
	Moderation *moderation.Service

	// Phase 3. Nil-safe: a feed served without them renders reels with no CTAs,
	// which is what Phase 2 did and is still a coherent product.
	Actions     *actions.Service
	Attribution *attribution.Service

	// Phase 4. Nil-safe on the same principle — a deployment without the social
	// surface serves a feed with no hearts and no threads, which is a smaller
	// product rather than a broken one.
	Likes     *social.Likes
	Comments  *comments.Service
	Ranking   *ranking.DBProvider
	Interests *interests.Service

	// Phase 9. Nil in every deployment that has not switched advertising on —
	// which PLAN-VIDEO-FIRST §7 says is the correct state until there is enough
	// organic demand that reach is worth buying. A nil here means the feed
	// serves organic reels and view events confirm nothing.
	Boosts *boosts.Service
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

type personalUploadBody struct {
	SubjectType string    `json:"subject_type"`
	SubjectID   uuid.UUID `json:"subject_id"`
	ContentType string    `json:"content_type"`
}

// CreatePersonalUpload is the Phase 6 seller-reel path.
//
// A separate endpoint rather than a nullable branch_id on CreateUpload. The two
// look similar and authorise on completely different grounds — branch
// membership versus owning the subject — and a single endpoint that picks
// between them based on which field is null is one refactor away from letting a
// user post as a vendor.
func (h *ReelHandlers) CreatePersonalUpload(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}

	var body personalUploadBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	if body.ContentType == "" {
		body.ContentType = "video/mp4"
	}

	upload, err := h.Reels.RequestUserUpload(r.Context(), reels.UserUploadRequest{
		UserID:      userID,
		SubjectType: body.SubjectType,
		SubjectID:   body.SubjectID,
		ContentType: body.ContentType,
	})
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, "", map[string]any{
		"reel_id":        upload.ReelID,
		"asset_id":       upload.AssetID,
		"upload_url":     upload.URL,
		"upload_headers": upload.Headers,
		"expires_at":     upload.ExpiresAt.Format(time.RFC3339),
		// Set expectations at the point of upload. Every author on this path is
		// `new` in author_trust, so all of it goes to a human before publish —
		// a seller who expects their video live in ninety seconds and gets it
		// in two hours writes a one-star review about a working system.
		"review_note": "personal videos are reviewed by a person before they appear in the feed",
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
	viewerID, viewerKey := viewerIdentity(r)
	query.ViewerID = viewerID
	query.ViewerKey = viewerKey

	page, err := h.Feed.Get(r.Context(), query)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	reelIDs := make([]uuid.UUID, len(page.Items))
	for i, it := range page.Items {
		reelIDs[i] = it.ID
	}

	// Resolve the CTAs for the whole page in one batch. This is the seam doing
	// its job: the feed hands over (kind, target_type, target_id) triples and
	// gets back labels, availability and routes without ever learning what a
	// laundry offering is.
	var resolved map[uuid.UUID][]actions.Resolved
	if h.Actions != nil && len(reelIDs) > 0 {
		resolved, err = h.Actions.ResolvePage(r.Context(), reelIDs, action.Viewer{
			UserID:  viewerID,
			Lat:     lat,
			Lng:     lng,
			Locale:  httpx.LocaleFromContext(r.Context()),
			Country: "AE",
		})
		if err != nil {
			// A CTA-resolution failure must not blank the feed. Reels still
			// play; they simply render without buttons this request.
			logIfErr(r, "action resolution failed", err)
			resolved = nil
		}
	}

	// Record the impression denominator. Phase 2 kept the seen-set in Redis and
	// out of the serving path, which was right for serving and wrong for
	// measurement: without a durable impression count, "conversion rate"
	// quietly becomes orders-per-click — the flattering one.
	//
	// Best-effort. Losing an impression row costs accuracy on a metric; failing
	// the request to protect that metric would cost the front door.
	if h.Actions != nil && len(reelIDs) > 0 {
		logIfErr(r, "impression recording failed",
			h.Actions.Repo().RecordImpressions(r.Context(), viewerKey, viewerID, "feed", reelIDs))
	}

	// Whether this viewer has already liked, saved or followed each item, in one
	// query for the page. Rendered by the client as a filled heart, so it is
	// answered from the relation tables and never from a counter — a counter is
	// a badge and being 3% low for ten seconds costs nothing, while a heart that
	// un-fills when you scroll away and back is immediately wrong to the one
	// person who knows the answer.
	var state social.ViewerState
	if h.Likes != nil && len(reelIDs) > 0 {
		state, err = h.Likes.StateFor(r.Context(), viewerID, reelIDs)
		if err != nil {
			logIfErr(r, "viewer state lookup failed", err)
		}
	}

	items := make([]map[string]any, 0, len(page.Items))
	for _, it := range page.Items {
		item := feedItemResponse(it)
		item["actions"] = actionsResponse(resolved[it.ID])
		item["viewer"] = map[string]any{
			"liked":    state.Liked[it.ID],
			"saved":    state.Saved[it.ID],
			"followed": state.Followed[it.ID],
		}
		items = append(items, item)
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"items":       items,
		"next_cursor": nullable(page.NextCursor),
		// The ranking configuration that produced this page. Returned so that
		// "the feed looked wrong at 14:20" is answerable rather than debatable.
		"ranking_profile": page.Profile,
	})
}

func actionsResponse(list []actions.Resolved) []map[string]any {
	out := make([]map[string]any, 0, len(list))
	for _, a := range list {
		out = append(out, resolvedActionResponse(a))
	}
	return out
}

// --- events ------------------------------------------------------------------

type eventsBody struct {
	SessionID string `json:"session_id"`
	Events    []struct {
		Type       string         `json:"type"`
		ReelID     *uuid.UUID     `json:"reel_id"`
		OccurredAt string         `json:"occurred_at"`
		WatchedMS  int            `json:"watched_ms"`
		Completed  bool           `json:"completed"`
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
			Completed:  e.Completed,
			Properties: e.Properties,
		})
	}
	if len(fieldErrs) > 0 {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: fieldErrs})
		return
	}

	// The viewer key, not only the user id. Phase 2 carried the id alone, and
	// the consequence was invisible until Phase 4 went looking for it: interest
	// profiles keyed on user id personalise nothing for anonymous viewers, who
	// are exactly the population whose first session decides whether they ever
	// return.
	userID, viewerKey := viewerIdentity(r)

	if err := h.Ingest.Submit(r.Context(), events,
		engagement.Viewer{UserID: userID, Key: viewerKey}); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	// Phase 9 — a view event is also the confirmation that turns a served
	// sponsored impression into a charged one.
	//
	// Only `view`, never `impression`. The distinction is what advertisers are
	// buying: an impression is a reel that entered the page, a view is one the
	// client reports as actually watched past the threshold. Charging on the
	// first would bill a prefetched page nobody scrolled to, which is the
	// difference between an ad product advertisers trust and one they audit.
	//
	// Best-effort and after the ingest. A billing failure must not cost the
	// ranker its engagement data — that data is the product, the billing is a
	// roll-up that the reservation-expiry sweep will reconcile anyway.
	if h.Boosts != nil {
		var viewed []uuid.UUID
		for _, e := range events {
			if e.Type == "view" && e.ReelID != nil {
				viewed = append(viewed, *e.ReelID)
			}
		}
		if len(viewed) > 0 {
			logIfErr(r, "sponsored impression confirmation failed",
				func() error {
					_, err := h.Boosts.Confirm(r.Context(), viewerKey, viewed)
					return err
				}())
		}
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

	out := map[string]any{
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
		// Whether this slot was reserved for exploration rather than earned on
		// score. Surfaced so the impression it produces can be told apart from
		// an earned one — without that distinction, exploration's cost is
		// unmeasurable and the first cost-cutting exercise deletes it.
		"explore": it.Explore,

		// Paid placement. Phase 9 set this flag in the domain and this
		// serializer never emitted it, which meant a client had no way to label
		// advertising — a legal requirement in both launch markets, not a
		// nicety. Found in QA rather than by a test, because every test in the
		// feed package works on the domain type and stops at the boundary.
		"sponsored": it.Sponsored,
	}
	if it.DeliveryID.Valid {
		// Only present on sponsored items, and only so a client can echo it on
		// the view event. Charging does not currently depend on it — `Confirm`
		// matches the served row on (viewer, reel), which is sound because a
		// reel already in the organic page is excluded from the auction — but a
		// client that returns it lets the confirmation stop relying on that
		// argument holding.
		out["delivery_id"] = it.DeliveryID.UUID
	}
	return out
}
