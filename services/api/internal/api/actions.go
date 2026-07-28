package api

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/actions"
	"github.com/marketplace/api/internal/modules/attribution"
	"github.com/marketplace/api/internal/seam/action"
)

// --- authoring ---------------------------------------------------------------

type setActionsBody struct {
	Actions []struct {
		Kind       string     `json:"kind"`
		TargetType string     `json:"target_type"`
		TargetID   *uuid.UUID `json:"target_id"`
		TargetURL  string     `json:"target_url"`
		Label      string     `json:"label"`
		LabelAr    string     `json:"label_ar"`
	} `json:"actions"`
}

func (h *ReelHandlers) SetActions(w http.ResponseWriter, r *http.Request) {
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

	var body setActionsBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	inputs := make([]actions.Input, 0, len(body.Actions))
	for _, a := range body.Actions {
		inputs = append(inputs, actions.Input{
			Kind:       action.Kind(a.Kind),
			TargetType: action.TargetType(a.TargetType),
			TargetID:   a.TargetID,
			TargetURL:  a.TargetURL,
			Label:      a.Label,
			LabelAr:    a.LabelAr,
		})
	}

	if err := h.Actions.Set(r.Context(), userID, reelID, inputs); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"count": len(inputs)})
}

// --- click -------------------------------------------------------------------

type clickBody struct {
	SessionID string `json:"session_id"`
	// Where the press happened. A click from the feed and one from a shared
	// link are not the same event, and averaging them hides whether the *feed*
	// is working.
	Surface string `json:"surface"`
	// How much of the reel had been watched. A press at 400ms is a mis-tap; one
	// at 12s is intent, and Phase 4 needs to tell them apart.
	WatchedMS int `json:"watched_ms"`
}

// ClickAction records the press and returns where to route.
//
// Recording and routing are the same call deliberately. If the client routed
// itself and reported the click separately, every dropped analytics request
// would be a conversion the server never saw — and those drops are not random,
// they cluster in exactly the bad-network conditions where conversion is
// already lowest. Coupling them means the number cannot quietly under-count.
func (h *ReelHandlers) ClickAction(w http.ResponseWriter, r *http.Request) {
	actionID, err := uuid.Parse(chi.URLParam(r, "actionId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	var body clickBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	viewerID, viewerKey := viewerIdentity(r)
	if viewerKey == "" {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "X-Device-Id", Code: "required",
			Message: "sign in or send a device id so this click can be attributed",
		}}})
		return
	}

	lat, _ := strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
	lng, _ := strconv.ParseFloat(r.URL.Query().Get("lng"), 64)

	resolved, err := h.Actions.Click(r.Context(), actions.ClickRequest{
		ActionID:  actionID,
		UserID:    viewerID,
		ViewerKey: viewerKey,
		SessionID: body.SessionID,
		Surface:   body.Surface,
		WatchedMS: body.WatchedMS,
		Viewer: action.Viewer{
			UserID: viewerID,
			Lat:    lat,
			Lng:    lng,
			Locale: httpx.LocaleFromContext(r.Context()),
		},
	})
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, "", resolvedActionResponse(*resolved))
}

// --- social ------------------------------------------------------------------

type followBody struct {
	FolloweeType string    `json:"followee_type"`
	FolloweeID   uuid.UUID `json:"followee_id"`
}

func (h *ReelHandlers) Follow(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	var body followBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	if err := h.Actions.Follow(r.Context(), userID, body.FolloweeType, body.FolloweeID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ReelHandlers) Unfollow(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	followeeID, err := uuid.Parse(chi.URLParam(r, "followeeId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	followeeType := r.URL.Query().Get("type")
	if followeeType == "" {
		followeeType = "vendor_branch"
	}
	if err := h.Actions.Unfollow(r.Context(), userID, followeeType, followeeID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ReelHandlers) SaveReel(w http.ResponseWriter, r *http.Request) {
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
	if err := h.Actions.Save(r.Context(), userID, reelID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ReelHandlers) UnsaveReel(w http.ResponseWriter, r *http.Request) {
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
	if err := h.Actions.Unsave(r.Context(), userID, reelID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- funnel ------------------------------------------------------------------

// ReelFunnel is the vendor-facing answer to "did this reel do anything".
func (h *ReelHandlers) ReelFunnel(w http.ResponseWriter, r *http.Request) {
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

	// Authorship check via the reels service, so a vendor cannot read a
	// competitor's conversion numbers by guessing a reel ID.
	if _, err := h.Reels.Get(r.Context(), userID, reelID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	from, to := dateRange(r, 30)
	f, err := h.Attribution.Funnel(r.Context(), reelID, from, to)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", funnelResponse(f))
}

// PlatformFunnel is the Phase 3 decision itself: across every reel, does the
// feed produce bookings? Admin-only, because it is the number that decides
// whether to keep funding the feed and it must not be quoted out of context.
func (h *ReelHandlers) PlatformFunnel(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	if err := h.Moderation.RequireStaff(r.Context(), userID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	from, to := dateRange(r, 30)
	p, err := h.Attribution.PlatformFunnel(r.Context(), from, to)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"from":        from.Format(time.RFC3339),
		"to":          to.Format(time.RFC3339),
		"impressions": p.Impressions,
		"clicks":      p.Clicks,
		"orders": map[string]any{
			"last_click":   p.OrdersLastClick,
			"declared":     p.OrdersDeclared,
			"view_through": p.OrdersViewThrough,
			"organic":      p.OrdersOrganic,
		},
		"gmv_last_click_minor":   p.GMVLastClickMinor,
		"gmv_total_minor":        p.GMVTotalMinor,
		"currency":               p.Currency,
		"reels_with_impressions": p.ReelsWithImpressions,
		"reels_with_any_order":   p.ReelsWithAnyOrder,
		// The one number the phase exists to produce, computed here rather than
		// left to a dashboard so that everyone quotes the same definition:
		// verifiable feed-driven orders over all orders placed.
		"feed_share_of_orders": p.FeedShareOfOrders(),
		"definitions": map[string]any{
			"last_click":                "server recorded a CTA press by this viewer within the click window",
			"declared":                  "client claimed a source reel; no server-recorded click corroborates it",
			"view_through":              "reel was served to the viewer but no CTA was pressed",
			"organic":                   "no reel matched — browse, search or repeat order",
			"click_window_hours":        int(attribution.ClickWindow.Hours()),
			"view_through_window_hours": int(attribution.ViewThroughWindow.Hours()),
		},
	})
}

// --- helpers -----------------------------------------------------------------

func funnelResponse(f *attribution.Funnel) map[string]any {
	return map[string]any{
		"reel_id":     f.ReelID,
		"from":        f.From.Format(time.RFC3339),
		"to":          f.To.Format(time.RFC3339),
		"impressions": f.Impressions,
		"views":       f.Views,
		"clicks":      f.Clicks,
		"orders": map[string]any{
			"last_click":   f.OrdersLastClick,
			"declared":     f.OrdersDeclared,
			"view_through": f.OrdersViewThrough,
		},
		"off_feed_orders":      f.OffFeedOrders,
		"gmv_last_click_minor": f.GMVLastClickMinor,
		"currency":             f.Currency,
		// Rates are named for their denominators. A single "conversion_rate"
		// field would let the flattering one be quoted by accident — orders per
		// click runs roughly ten times orders per impression.
		"rates": map[string]any{
			"clicks_per_impression": f.ClickThroughRate(),
			"orders_per_click":      f.OrderRatePerClick(),
			"orders_per_impression": f.OrderRatePerImpression(),
		},
	}
}

func resolvedActionResponse(a actions.Resolved) map[string]any {
	return map[string]any{
		"id":          a.ID,
		"kind":        a.Kind,
		"target_type": a.TargetType,
		"target_id":   a.TargetID,
		"position":    a.Position,
		"label":       a.Resolved.Label,
		"label_ar":    a.Resolved.LabelAr,
		"available":   a.Resolved.Available,
		"reason":      nullable(a.Resolved.Reason),
		"route":       a.Resolved.Route,
		"meta":        a.Resolved.Meta,
	}
}

// viewerIdentity resolves who is looking, signed in or not.
//
// Both forms matter. An anonymous viewer who clicks a CTA, signs up, and then
// orders is the feed doing the entire job including acquisition — and keying
// attribution only on user_id would credit that to nothing at all.
func viewerIdentity(r *http.Request) (uuid.NullUUID, string) {
	if userID, ok := httpx.UserIDFromContext(r.Context()); ok {
		return uuid.NullUUID{UUID: userID, Valid: true}, "u:" + userID.String()
	}
	if device := r.Header.Get("X-Device-Id"); device != "" {
		return uuid.NullUUID{}, "d:" + device
	}
	return uuid.NullUUID{}, ""
}

// dateRange parses ?from&to, defaulting to the last defaultDays.
func dateRange(r *http.Request, defaultDays int) (time.Time, time.Time) {
	q := r.URL.Query()
	to := time.Now().UTC()
	from := to.AddDate(0, 0, -defaultDays)

	if raw := q.Get("from"); raw != "" {
		if t, err := time.Parse("2006-01-02", raw); err == nil {
			from = t.UTC()
		}
	}
	if raw := q.Get("to"); raw != "" {
		if t, err := time.Parse("2006-01-02", raw); err == nil {
			// Inclusive of the named day. A range of 2026-07-01..2026-07-01
			// meaning "no data" is the kind of thing that gets reported as a
			// missing-numbers bug.
			to = t.UTC().AddDate(0, 0, 1)
		}
	}
	// An inverted range would return zeroes across the board, which reads as
	// "this reel converted nothing" rather than "you asked the wrong question".
	// Fall back to the default window instead.
	if !from.Before(to) {
		from = to.AddDate(0, 0, -defaultDays)
	}
	return from, to
}

func logIfErr(r *http.Request, msg string, err error) {
	if err != nil {
		slog.WarnContext(r.Context(), msg, "error", err)
	}
}
