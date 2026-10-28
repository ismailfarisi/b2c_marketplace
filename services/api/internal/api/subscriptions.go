package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/promo"
	"github.com/marketplace/api/internal/modules/reviews"
	"github.com/marketplace/api/internal/modules/subscriptions"
)

// SubscriptionHandlers covers the Phase 5 recurring surface.
//
// Separate from ReelHandlers and from the order handlers because subscriptions
// are neither: they are the third way money enters this system, and folding
// them into the order handlers would put a rolling scheduler behind a struct
// named for one-off purchases.
type SubscriptionHandlers struct {
	Service *subscriptions.Service
	// Reviews lets a subscriber rate the kitchen. Nil disables it.
	Reviews *reviews.Service
}

type subscribeBody struct {
	PlanID    uuid.UUID  `json:"plan_id"`
	TierID    *uuid.UUID `json:"tier_id"`
	AddressID uuid.UUID  `json:"address_id"`
	StartDate string     `json:"start_date"`
	// Auto by default. "none" ends the subscription after one cycle, which is
	// how a trial is sold without building a trial feature.
	RenewalMode string `json:"renewal_mode"`

	Schedule []struct {
		Weekday   int        `json:"weekday"`
		Slot      string     `json:"meal_slot"`
		WindowID  uuid.UUID  `json:"window_id"`
		AddressID *uuid.UUID `json:"address_id"`
	} `json:"schedule"`

	SourceReelID   *uuid.UUID `json:"source_reel_id"`
	SourceActionID *uuid.UUID `json:"source_action_id"`

	PromoCode string     `json:"promo_code"`
	PromoID   *uuid.UUID `json:"promo_id"`
}

// input decodes a checkout body; shared by Subscribe and Preview.
func (h *SubscriptionHandlers) input(w http.ResponseWriter, r *http.Request) (subscriptions.SubscribeInput, bool) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return subscriptions.SubscribeInput{}, false
	}

	var body subscribeBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return subscriptions.SubscribeInput{}, false
	}

	start, err := time.Parse("2006-01-02", body.StartDate)
	if err != nil {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "start_date", Code: "invalid_date",
			Message: "expected YYYY-MM-DD",
		}}})
		return subscriptions.SubscribeInput{}, false
	}

	rules := make([]subscriptions.Rule, 0, len(body.Schedule))
	for _, s := range body.Schedule {
		rule := subscriptions.Rule{
			// 0 = Sunday. The GCC working week starts Sunday, and ISO-8601's
			// Monday-first assumption delivers every meal one day late.
			Weekday:  subscriptions.Weekday(s.Weekday),
			Slot:     s.Slot,
			WindowID: s.WindowID,
		}
		if s.AddressID != nil {
			rule.AddressID = uuid.NullUUID{UUID: *s.AddressID, Valid: true}
		}
		rules = append(rules, rule)
	}

	var tierID uuid.NullUUID
	if body.TierID != nil {
		tierID = uuid.NullUUID{UUID: *body.TierID, Valid: true}
	}

	_, viewerKey := viewerIdentity(r)

	return subscriptions.SubscribeInput{
		UserID:         userID,
		PlanID:         body.PlanID,
		TierID:         tierID,
		AddressID:      body.AddressID,
		StartDate:      start,
		Rules:          rules,
		RenewalMode:    body.RenewalMode,
		Promo:          promo.Ref{Code: body.PromoCode, ID: body.PromoID},
		SourceReelID:   body.SourceReelID,
		SourceActionID: body.SourceActionID,
		ViewerKey:      viewerKey,
	}, true
}

// Preview answers POST /subscriptions/preview: the first cycle's price,
// with any promo applied, without creating anything.
func (h *SubscriptionHandlers) Preview(w http.ResponseWriter, r *http.Request) {
	in, ok := h.input(w, r)
	if !ok {
		return
	}
	p, err := h.Service.PreviewSubscription(r.Context(), in)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", p)
}

func (h *SubscriptionHandlers) Subscribe(w http.ResponseWriter, r *http.Request) {
	in, ok := h.input(w, r)
	if !ok {
		return
	}
	res, err := h.Service.Subscribe(r.Context(), in)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	// 201 with the billing order attached. The client's next call is to create a
	// payment intent against that order — the same endpoint a one-off order
	// uses, because a subscription's money moves through the same path as any
	// other money in the system.
	httpx.WriteJSON(w, http.StatusCreated, "", map[string]any{
		"id":              res.ID,
		"subscription_no": res.SubscriptionNo,
		"status":          res.Status,
		"start_date":      res.StartDate.Format("2006-01-02"),
		"order_id":        res.OrderID,
		"amount_minor":    res.AmountMinor,
		"discount_minor":  res.DiscountMinor,
		"trial_days":      res.TrialDays,
		"currency":        res.Currency,
		"first_cycle": map[string]any{
			"cycle_no":         res.FirstCycle.No,
			"starts_on":        res.FirstCycle.Starts.Format("2006-01-02"),
			"ends_on":          res.FirstCycle.Ends.Format("2006-01-02"),
			"deliverable_days": res.FirstCycle.DeliverableDays,
		},
		// Stated explicitly because it is the one thing a client would otherwise
		// get wrong: nothing is delivered until the billing order is paid.
		"next_step": "pay the billing order — deliveries are scheduled once payment succeeds",
	})
}

type pauseBody struct {
	FromDate   string `json:"from_date"`
	ToDate     string `json:"to_date"`
	CreditMode string `json:"credit_mode"`
}

func (h *SubscriptionHandlers) Pause(w http.ResponseWriter, r *http.Request) {
	userID, subID, ok := userAndSubscription(w, r)
	if !ok {
		return
	}

	var body pauseBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	from, ferr := time.Parse("2006-01-02", body.FromDate)
	to, terr := time.Parse("2006-01-02", body.ToDate)
	if ferr != nil || terr != nil {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "from_date", Code: "invalid_date", Message: "expected YYYY-MM-DD",
		}}})
		return
	}

	if err := h.Service.Pause(r.Context(), subscriptions.PauseInput{
		UserID:         userID,
		SubscriptionID: subID,
		From:           from,
		To:             to,
		CreditMode:     body.CreditMode,
	}); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"status": "paused"})
}

type skipBody struct {
	Date   string `json:"date"`
	Slot   string `json:"meal_slot"`
	Reason string `json:"reason"`
}

func (h *SubscriptionHandlers) Skip(w http.ResponseWriter, r *http.Request) {
	userID, subID, ok := userAndSubscription(w, r)
	if !ok {
		return
	}

	var body skipBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	day, err := time.Parse("2006-01-02", body.Date)
	if err != nil {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "date", Code: "invalid_date", Message: "expected YYYY-MM-DD",
		}}})
		return
	}

	if err := h.Service.Skip(r.Context(), userID, subID, day, body.Slot, body.Reason); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"status": "skipped"})
}

func (h *SubscriptionHandlers) Cancel(w http.ResponseWriter, r *http.Request) {
	userID, subID, ok := userAndSubscription(w, r)
	if !ok {
		return
	}

	var body struct {
		Reason string `json:"reason"`
	}
	// A cancellation with no body is valid — a reason is useful and not
	// required, and refusing to cancel because the client sent no JSON would be
	// the worst possible moment to be pedantic.
	_ = httpx.DecodeJSON(r, &body)

	if err := h.Service.Cancel(r.Context(), userID, subID, body.Reason); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"status": "cancelled"})
}

func userAndSubscription(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return uuid.Nil, uuid.Nil, false
	}
	subID, err := uuid.Parse(chi.URLParam(r, "subscriptionId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return uuid.Nil, uuid.Nil, false
	}
	return userID, subID, true
}
