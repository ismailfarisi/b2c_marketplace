package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/actions"
	"github.com/marketplace/api/internal/modules/engagement"
	"github.com/marketplace/api/internal/modules/reels"
	"github.com/marketplace/api/internal/seam/action"
	"github.com/marketplace/api/internal/seam/subject"
)

// SellHandlers is the authoring flow as a phone uses it.
//
// # Why this is not just the existing endpoints
//
// Everything here could be assembled by a client from routes that already exist:
// POST /listings, POST /reels/uploads/personal, PUT /reels/{id}/actions, POST
// /reels/{id}, POST /listings/{id}/submit. Five calls in a required order, three
// of which have ordering constraints the client cannot see.
//
// The constraint that matters is this one, from actions.Service.Set:
//
//	actions are fixed once moderation has begun
//
// A CTA must be attached while the reel is still pre-moderation. Moderation
// begins when the bytes and the metadata have both landed, and the bytes land
// via an S3 event on a schedule the client does not control. So a client that
// uploads first and attaches the CTA afterwards has a race it cannot win on a
// fast connection — and it loses by publishing a reel with no button on it,
// which looks like a feed bug and is discovered by the buyer.
//
// Composing it server-side removes the race by construction: the CTA is attached
// at draft time, before a single byte exists. That is the reason these handlers
// exist, and it is worth more than the round trips they save.
type SellHandlers struct {
	Reels    *reels.Service
	Actions  *actions.Service
	Subjects *subject.Registry

	// Counters is the live screen's numbers. Nil-safe: the screen renders with
	// dashes rather than not rendering, because "your reel is live" is the
	// message and the view count is the decoration.
	Counters *engagement.Counters
}

// --- what can I sell ---------------------------------------------------------

// SellVerticals is the tile grid on the Sell tab.
//
// Built from subject.Registry.Sellable, which is the same source the filter row
// uses. A vertical that registers a composer gets a tile on the next request;
// there is no list of tiles in either client, and there must not be, because the
// mock's four tiles are one built vertical and three intentions.
func (h *SellHandlers) SellVerticals(w http.ResponseWriter, r *http.Request) {
	facets := h.Subjects.Sellable()
	out := make([]map[string]any, 0, len(facets))
	for _, f := range facets {
		out = append(out, map[string]any{
			"key":      f.Key,
			"label":    f.Label,
			"label_ar": f.LabelAr,
			"icon":     f.Icon,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"verticals": out})
}

// SellForm hands back the composition for one vertical.
//
// Fetched after the seller picks a tile rather than bundled with the tile list,
// because the form is the larger payload and most sellers only ever see one of
// them. Cacheable — it changes when a vertical is deployed, not per seller.
func (h *SellHandlers) SellForm(w http.ResponseWriter, r *http.Request) {
	composer, ok := h.Subjects.Composer(subject.Type(chi.URLParam(r, "vertical")))
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", compositionResponse(composer.Compose()))
}

func compositionResponse(c subject.Composition) map[string]any {
	fields := make([]map[string]any, 0, len(c.Fields))
	for _, f := range c.Fields {
		fields = append(fields, fieldResponse(f))
	}

	clocks := make([]map[string]any, 0, len(c.Clocks))
	for _, k := range c.Clocks {
		clocks = append(clocks, map[string]any{
			"key":          k.Key,
			"mode":         k.Mode,
			"label":        k.Label,
			"label_ar":     k.LabelAr,
			"detail":       k.Detail,
			"detail_ar":    k.DetailAr,
			"needs_window": k.NeedsWindow,
			"default":      k.Default,
		})
	}

	out := map[string]any{
		"fields": fields,
		"clocks": clocks,
		"tip":    c.Tip,
		"tip_ar": c.TipAr,
		"trust":  nil,
	}
	if c.Trust != nil {
		trust := map[string]any{
			"key":      c.Trust.Key,
			"label":    c.Trust.Label,
			"label_ar": c.Trust.LabelAr,
			"earns":    c.Trust.Earns,
			"earns_ar": c.Trust.EarnsAr,
			"accept":   c.Trust.Accept,
			// The identifier that travels with the document, when the artefact
			// has one. Rendered by the same code that draws a field, because it
			// is one — see subject.TrustUpload.Reference for why it lives here
			// rather than among the four.
			"reference": nil,
		}
		if c.Trust.Reference != nil {
			trust["reference"] = fieldResponse(*c.Trust.Reference)
		}
		out["trust"] = trust
	}
	return out
}

// fieldResponse renders one input.
//
// Extracted so the trust slot's reference goes over the wire in exactly the
// same shape as the four fields above it. The client already knows how to draw
// this object; giving the permit number a shape of its own would have meant a
// second renderer for a text box.
func fieldResponse(f subject.Field) map[string]any {
	out := map[string]any{
		"key":      f.Key,
		"label":    f.Label,
		"label_ar": f.LabelAr,
		"kind":     f.Kind,
		"required": f.Required,
		"unit":     f.Unit,
		"unit_ar":  f.UnitAr,
		"hint":     f.Hint,
		"hint_ar":  f.HintAr,
		// Always present, empty when unset. A client that has to distinguish
		// absent from empty for a placeholder is a client with a branch it
		// does not need.
		"placeholder": f.Placeholder,
		"source":      f.Source,
		"min":         f.Min,
		"max":         f.Max,
	}
	opts := make([]map[string]any, 0, len(f.Options))
	for _, o := range f.Options {
		opts = append(opts, map[string]any{
			"value": o.Value, "label": o.Label, "label_ar": o.LabelAr,
		})
	}
	out["options"] = opts
	return out
}

// --- the draft ---------------------------------------------------------------

type sellDraftBody struct {
	SubjectType string            `json:"subject_type"`
	Values      map[string]string `json:"values"`

	Clock       string     `json:"clock"`
	WindowStart *time.Time `json:"window_start"`
	WindowEnd   *time.Time `json:"window_end"`

	Lat    float64 `json:"lat"`
	Lng    float64 `json:"lng"`
	Region string  `json:"region"`
	Place  string  `json:"place"`

	ContentType string `json:"content_type"`
}

// CreateSellDraft is one call for the whole form step: the subject row, the reel
// draft, the presigned PUT and the CTA.
//
// It runs in that order and the order is load-bearing. The subject must exist
// before the reel, because a personal upload authorises through owning the
// subject. The CTA must be attached after the reel and before any bytes, for the
// reason in the type comment.
//
// It is deliberately *not* one database transaction. Three of the four steps
// have their own, and the presign in the middle is an operation against the
// object store's SDK — holding a transaction open across it is how one slow
// dependency becomes connection-pool exhaustion, which is the same call
// reels.RequestUpload already makes and documents.
//
// What that costs is a failure window: a crash between the listing and the reel
// leaves a listing draft with no video. That is survivable and cheap — drafts
// are free and unlimited by design, the seller retries, and the orphan expires
// with every other abandoned draft. The alternative failure, a published reel
// with no button, is the one that reaches a buyer.
func (h *SellHandlers) CreateSellDraft(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}

	var body sellDraftBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	subjectType := subject.Type(body.SubjectType)
	composer, ok := h.Subjects.Composer(subjectType)
	if !ok {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "subject_type", Code: "unsupported",
			Message: "nothing of this kind can be sold from the app yet",
		}}})
		return
	}

	composition := composer.Compose()
	clockKey := body.Clock
	if clockKey == "" {
		clockKey = composition.DefaultClock().Key
	}
	clock, known := composition.Clock(clockKey)
	if !known {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "clock", Code: "unsupported",
			Message: "this vertical does not offer that listing term",
		}}})
		return
	}
	// Checked here rather than left to the vertical, because it is a property of
	// the ClockOption the vertical already declared and every composer would
	// otherwise re-implement it. A window with one end is a listing that either
	// never opens or never closes.
	if clock.NeedsWindow && (body.WindowStart == nil || body.WindowEnd == nil) {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "window_start", Code: "required",
			Message: "this listing term runs between two times",
		}}})
		return
	}

	if body.Lat == 0 && body.Lng == 0 {
		// Null Island, and it is worth its own error rather than being left to
		// each vertical's validator. A subject with no point is invisible to
		// every geo query, so the seller would publish successfully and then
		// never appear to anyone — which they would report as a feed bug.
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "lat", Code: "required", Message: "pick where it is",
		}}})
		return
	}

	in := subject.DraftInput{
		SellerID: userID,
		Values:   body.Values,
		Clock:    clockKey,
		Lat:      body.Lat, Lng: body.Lng,
		Region: body.Region, Place: body.Place,
		Locale:  httpx.LocaleFromContext(r.Context()),
		Country: "AE",
	}
	if body.WindowStart != nil {
		in.WindowStart = *body.WindowStart
	}
	if body.WindowEnd != nil {
		in.WindowEnd = *body.WindowEnd
	}

	drafted, err := composer.Draft(r.Context(), in)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	contentType := body.ContentType
	if contentType == "" {
		contentType = "video/mp4"
	}
	upload, err := h.Reels.RequestUserUpload(r.Context(), reels.UserUploadRequest{
		UserID:      userID,
		SubjectType: string(subjectType),
		SubjectID:   drafted.SubjectID,
		ContentType: contentType,
	})
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	// The CTA, while the reel is still empty. See the type comment — this line
	// is the reason this handler exists.
	if h.Actions != nil && drafted.CTAKind != "" {
		targetID := drafted.SubjectID
		if err := h.Actions.Set(r.Context(), userID, upload.ReelID, []actions.Input{{
			Kind:       action.Kind(drafted.CTAKind),
			TargetType: action.TargetType(drafted.CTATargetType),
			TargetID:   &targetID,
		}}); err != nil {
			httpx.WriteProblem(w, r, err)
			return
		}
	}

	httpx.WriteJSON(w, http.StatusCreated, "", map[string]any{
		"reel_id":        upload.ReelID,
		"subject_id":     drafted.SubjectID,
		"subject_type":   subjectType,
		"asset_id":       upload.AssetID,
		"upload_url":     upload.URL,
		"upload_headers": upload.Headers,
		"expires_at":     upload.ExpiresAt.Format(time.RFC3339),
		// Repeated from CreatePersonalUpload deliberately. Every author on this
		// path is `new` in author_trust, so all of it goes to a human before
		// publish; a seller who expects their video live in ninety seconds and
		// gets it in two hours writes a one-star review about a working system.
		"review_note": "personal videos are reviewed by a person before they appear in the feed",
	})
}

// --- publish -----------------------------------------------------------------

// PublishSellDraft is the Publish button.
//
// Two writes, and the order was arrived at by getting it wrong first.
//
// The subject publishes *before* the reel's metadata is submitted. The intuition
// runs the other way — submit the metadata first, because that is the write that
// can 409 — and it is wrong, because of what the two failures actually leave
// behind:
//
//   - Metadata first, then the subject. If the subject refuses — a listing quota
//     with no credit on it, say — the metadata is already stamped, the pipeline
//     is already running, and a reel publishes into the feed pointing at a
//     listing still in `draft`. Its provider projects no time signal and its CTA
//     resolves unavailable, so it appears as a reel of a car with no badge, no
//     price and a dead button.
//
//   - Subject first, then metadata. If the metadata refuses, it is because
//     moderation has already begun — which is the *only* thing that produces
//     that 409, and it means the metadata was submitted by an earlier attempt
//     and a video is already on its way. A listing in review is then correct
//     rather than orphaned, and a retry is idempotent.
//
// The second failure is survivable and the first reaches a buyer.
func (h *SellHandlers) PublishSellDraft(w http.ResponseWriter, r *http.Request) {
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

	// The reel is read first, unpublished, purely to learn which subject it
	// points at. Reels.Get authorises, so this doubles as the authorship check
	// before anything is written.
	reel, err := h.Reels.Get(r.Context(), userID, reelID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	published := "none"
	if reel.SubjectType != nil && reel.SubjectID != nil {
		if composer, ok := h.Subjects.Composer(subject.Type(*reel.SubjectType)); ok {
			if err := composer.Publish(r.Context(), userID, *reel.SubjectID); err != nil {
				// Nothing has been written yet, so a refusal here — no listing
				// credit, a quota, a vertical's own rule — leaves the draft
				// exactly as it was. The seller fixes it and taps Publish again.
				httpx.WriteProblem(w, r, err)
				return
			}
			published = "submitted"
		}
	}

	// No body. There is nothing left to say: the fields were answered at draft
	// time and there is no caption in this flow — the overlay's words are
	// composed by the vertical's Summarize, which is the whole point of the read
	// half of this seam. A caption box here would be a second, worse source of
	// the same three lines.
	reel, err = h.Reels.SubmitMetadata(r.Context(), userID, reelID, reels.MetadataInput{
		Visibility: "public",
	})
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	// Ask the object store whether the video landed, rather than waiting for a
	// bucket notification to tell us. The seller tapped Publish milliseconds
	// after the last byte and is watching this request; see
	// reels.Service.ConfirmBytes for why this does not replace the S3 event and
	// why, in this deployment, it is the only thing that answers at all.
	//
	// **After** SubmitMetadata, and that is not arbitrary. SubmitMetadata calls
	// Advance and throws away the job it returns; with both preconditions
	// already satisfied it would walk the reel to `probing` inside this request
	// and discard the JobProbe that starts the work, leaving the worker's sweep
	// to find a reel one state past anything it can act on. Stamping the bytes
	// second means the sweep picks the reel up from `draft` with the job in
	// hand. See the note on ConfirmBytes — this is the same trap, and the
	// discarded job in SubmitMetadata is still live for any caller that reverses
	// these two.
	//
	// A failure here is logged and not returned. The listing still goes to
	// review and the event path — once wired — picks the reel up; failing the
	// publish over a HEAD would throw away work that has already been done.
	if arrived, err := h.Reels.ConfirmBytes(r.Context(), reelID); err != nil {
		logIfErr(r, "confirm uploaded bytes", err)
	} else if arrived {
		// Re-read so the response carries the state the confirmation produced
		// rather than the one from before it. A client that showed `draft` here
		// would poll for a change that had already happened.
		if fresh, err := h.Reels.Get(r.Context(), userID, reelID); err == nil {
			reel = fresh
		}
	}

	out := authorReelResponse(reel)
	out["subject_state"] = published
	httpx.WriteJSON(w, http.StatusOK, "", out)
}

// --- the live screen ---------------------------------------------------------

// SellReelStatus is what the seller sees after publishing, and every time they
// come back to it.
//
// It reports the pipeline state honestly rather than claiming success. A reel in
// `review_pending` is not live and telling the seller it is produces the worst
// possible sequence: they share the link, their friend sees nothing, and they
// conclude the platform is broken. The state is returned raw and the client
// renders a sentence for it.
func (h *SellHandlers) SellReelStatus(w http.ResponseWriter, r *http.Request) {
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

	// Authorship is checked by the service, so a stranger cannot read a seller's
	// numbers by guessing a reel id.
	reel, err := h.Reels.Get(r.Context(), userID, reelID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	out := authorReelResponse(reel)
	out["counters"] = map[string]any{"views": 0, "likes": 0, "comments": 0, "shares": 0, "saves": 0}
	if h.Counters != nil {
		if counts, err := h.Counters.Batch(r.Context(), []uuid.UUID{reelID}); err == nil {
			if c, present := counts[reelID]; present {
				out["counters"] = map[string]any{
					"views": c.Views, "likes": c.Likes, "comments": c.Comments,
					"shares": c.Shares, "saves": c.Saves,
				}
			}
		} else {
			// Logged, not returned. The counters are read from a hot store that
			// is allowed to be down; the seller still needs to know their reel is
			// live, and failing this whole response over a view count would take
			// that away.
			logIfErr(r, "sell status counters unavailable", err)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, "", out)
}

// --- the car picker ----------------------------------------------------------

// SuggestModels serves the lookup field vehicles declared.
//
// It lives on VehicleHandlers rather than on SellHandlers, and that is the seam
// holding: the sell flow knows a field has a Source and GETs it, and has no idea
// this one returns cars. A vertical whose lookup is neighbourhoods or cuisines
// serves its own path and nothing here changes.
func (h *VehicleHandlers) SuggestModels(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	suggestions, err := h.Listings.SuggestModels(r.Context(), r.URL.Query().Get("q"), limit)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	out := make([]map[string]any, 0, len(suggestions))
	for _, s := range suggestions {
		out = append(out, map[string]any{
			"value": s.Value, "label": s.Label, "label_ar": s.LabelAr,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"options": out})
}
