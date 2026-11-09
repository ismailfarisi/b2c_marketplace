package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/payments"
	"github.com/marketplace/api/internal/modules/promo"
	"github.com/marketplace/api/internal/modules/subscriptions"
)

// RotationHandlers serve the four-kitchen rotation: preview the week order,
// buy it, pay once, and manage it.
type RotationHandlers struct {
	Service  *subscriptions.Service
	Payments *payments.Service
}

type rotationBody struct {
	PlanIDs       []uuid.UUID   `json:"plan_ids"`
	AddressID     uuid.UUID     `json:"address_id"`
	SlotAddresses []slotAddress `json:"slot_addresses"`
	StartDate     string        `json:"start_date"`
	Weekdays      []int         `json:"delivery_weekdays"`
	MealSlots     []string      `json:"meal_slots"`
	PromoCode     string        `json:"promo_code"`
	PromoID       *uuid.UUID    `json:"promo_id"`
}

// slotAddress sends one meal of a rotation or subscription to an address.
type slotAddress struct {
	MealSlot  string    `json:"meal_slot"`
	AddressID uuid.UUID `json:"address_id"`
}

// slotAddressMap turns the request list into the service's per-meal map,
// rejecting blank meals, blank addresses and a meal listed twice.
func slotAddressMap(field string, items []slotAddress) (map[string]uuid.UUID, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make(map[string]uuid.UUID, len(items))
	for i, it := range items {
		if it.MealSlot == "" {
			return nil, &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: fmt.Sprintf("%s[%d].meal_slot", field, i), Code: "required", Message: "meal slot is required",
			}}}
		}
		if it.AddressID == uuid.Nil {
			return nil, &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: fmt.Sprintf("%s[%d].address_id", field, i), Code: "required", Message: "address is required",
			}}}
		}
		if _, dup := out[it.MealSlot]; dup {
			return nil, &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: fmt.Sprintf("%s[%d].meal_slot", field, i), Code: "duplicate",
				Message: fmt.Sprintf("%s is listed twice", it.MealSlot),
			}}}
		}
		out[it.MealSlot] = it.AddressID
	}
	return out, nil
}

func (h *RotationHandlers) input(w http.ResponseWriter, r *http.Request) (subscriptions.RotationInput, bool) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return subscriptions.RotationInput{}, false
	}
	var body rotationBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return subscriptions.RotationInput{}, false
	}
	start, err := time.Parse("2006-01-02", body.StartDate)
	if err != nil {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "start_date", Code: "invalid_date", Message: "expected YYYY-MM-DD",
		}}})
		return subscriptions.RotationInput{}, false
	}
	slotAddresses, err := slotAddressMap("slot_addresses", body.SlotAddresses)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return subscriptions.RotationInput{}, false
	}
	return subscriptions.RotationInput{
		UserID: userID, AddressID: body.AddressID, SlotAddresses: slotAddresses, PlanIDs: body.PlanIDs,
		StartDate: start, Weekdays: body.Weekdays, Slots: body.MealSlots,
		Promo: promo.Ref{Code: body.PromoCode, ID: body.PromoID},
	}, true
}

// Preview answers POST /rotations/preview.
func (h *RotationHandlers) Preview(w http.ResponseWriter, r *http.Request) {
	in, ok := h.input(w, r)
	if !ok {
		return
	}
	q, err := h.Service.PreviewRotation(r.Context(), in)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", q)
}

type suggestionBody struct {
	AddressID     uuid.UUID     `json:"address_id"`
	SlotAddresses []slotAddress `json:"slot_addresses"`
	StartDate     string        `json:"start_date"`
	Weekdays      []int         `json:"delivery_weekdays"`
	MealSlots     []string      `json:"meal_slots"`
	PromoCode     string        `json:"promo_code"`
	PromoID       *uuid.UUID    `json:"promo_id"`
}

// Suggest answers POST /rotations/suggestion: a balanced rotation of four
// kitchens for the address, quoted like preview.
func (h *RotationHandlers) Suggest(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	var body suggestionBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	slotAddresses, err := slotAddressMap("slot_addresses", body.SlotAddresses)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	in := subscriptions.SuggestInput{
		UserID: userID, AddressID: body.AddressID, SlotAddresses: slotAddresses,
		Weekdays: body.Weekdays, Slots: body.MealSlots,
		Promo: promo.Ref{Code: body.PromoCode, ID: body.PromoID},
	}
	if body.StartDate != "" {
		start, err := time.Parse("2006-01-02", body.StartDate)
		if err != nil {
			httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "start_date", Code: "invalid_date", Message: "expected YYYY-MM-DD",
			}}})
			return
		}
		in.StartDate = &start
	}
	out, err := h.Service.SuggestRotation(r.Context(), in)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", out)
}

// Create answers POST /rotations.
func (h *RotationHandlers) Create(w http.ResponseWriter, r *http.Request) {
	in, ok := h.input(w, r)
	if !ok {
		return
	}
	rot, err := h.Service.CreateRotation(r.Context(), in)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, "", rot)
}

// Mine answers GET /me/rotations.
func (h *RotationHandlers) Mine(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	items, err := h.Service.MyRotations(r.Context(), userID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

func rotationTarget(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return uuid.Nil, uuid.Nil, false
	}
	id, err := uuid.Parse(chi.URLParam(r, "rotationId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return uuid.Nil, uuid.Nil, false
	}
	return userID, id, true
}

// Detail answers GET /me/rotations/{rotationId}.
func (h *RotationHandlers) Detail(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := rotationTarget(w, r)
	if !ok {
		return
	}
	rot, err := h.Service.MyRotation(r.Context(), userID, id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", rot)
}

// Cancel answers POST /rotations/{rotationId}/cancel.
func (h *RotationHandlers) Cancel(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := rotationTarget(w, r)
	if !ok {
		return
	}
	var body struct {
		Reason     string `json:"reason"`
		ReasonCode string `json:"reason_code"`
	}
	if r.ContentLength != 0 {
		if err := httpx.DecodeJSON(r, &body); err != nil {
			httpx.WriteProblem(w, r, err)
			return
		}
	}
	var err error
	if body.ReasonCode != "" {
		err = h.Service.CancelRotationWithCode(r.Context(), userID, id, body.ReasonCode, body.Reason)
	} else {
		err = h.Service.CancelRotation(r.Context(), userID, id, body.Reason)
	}
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SwapOptions answers GET /rotations/{rotationId}/weeks/{week}/options: the
// kitchens one future week could change to.
func (h *RotationHandlers) SwapOptions(w http.ResponseWriter, r *http.Request) {
	userID, id, week, ok := rotationWeekTarget(w, r)
	if !ok {
		return
	}
	opts, err := h.Service.SwapOptions(r.Context(), userID, id, week)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", opts)
}

// SwapWeek answers POST /rotations/{rotationId}/weeks/{week}/swap. A dearer
// kitchen is paid for on the spot through the payment flow; the answer carries
// the intent so the client can finish it when the provider needs a redirect.
func (h *RotationHandlers) SwapWeek(w http.ResponseWriter, r *http.Request) {
	userID, id, week, ok := rotationWeekTarget(w, r)
	if !ok {
		return
	}
	var body struct {
		PlanID uuid.UUID `json:"plan_id"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	if body.PlanID == uuid.Nil {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "plan_id", Code: "required", Message: "choose a kitchen"}}})
		return
	}
	res, err := h.Service.SwapRotationWeek(r.Context(), userID, id, week, body.PlanID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	var payment any
	if res.DueMinor > 0 {
		intent, err := h.Payments.Authorize(r.Context(), userID, res.OrderID, r.Header.Get("Idempotency-Key"))
		if err != nil {
			httpx.WriteProblem(w, r, err)
			return
		}
		payment = map[string]any{
			"id":       intent.ID,
			"status":   intent.Status,
			"provider": intent.Provider,
			"amount": map[string]any{
				"amount":   intent.AmountMinor,
				"currency": intent.Currency,
			},
			"client_secret": nullable(intent.ClientSecret),
			"redirect_url":  nullable(intent.RedirectURL),
		}
		if res.Rotation, err = h.Service.MyRotation(r.Context(), userID, id); err != nil {
			httpx.WriteProblem(w, r, err)
			return
		}
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"rotation":  res.Rotation,
		"due_minor": res.DueMinor,
		"currency":  res.Currency,
		"payment":   payment,
	})
}

func rotationWeekTarget(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, int, bool) {
	userID, id, ok := rotationTarget(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, 0, false
	}
	week, err := strconv.Atoi(chi.URLParam(r, "week"))
	if err != nil || week < 1 || week > subscriptions.RotationKitchens {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return uuid.Nil, uuid.Nil, 0, false
	}
	return userID, id, week, true
}

// Pay answers POST /rotations/{rotationId}/payment-intent: one payment for all
// four weeks.
func (h *RotationHandlers) Pay(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := rotationTarget(w, r)
	if !ok {
		return
	}
	intent, err := h.Payments.AuthorizeRotation(r.Context(), userID, id, r.Header.Get("Idempotency-Key"))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, "", map[string]any{
		"id":       intent.ID,
		"status":   intent.Status,
		"provider": intent.Provider,
		"amount": map[string]any{
			"amount":   intent.AmountMinor,
			"currency": intent.Currency,
		},
		"client_secret": nullable(intent.ClientSecret),
		"redirect_url":  nullable(intent.RedirectURL),
	})
}

// SetRenewal answers PUT /rotations/{rotationId}/renewal.
func (h *RotationHandlers) SetRenewal(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := rotationTarget(w, r)
	if !ok {
		return
	}
	var body struct {
		Auto    *bool `json:"auto"`
		Refresh *bool `json:"refresh_on_renew"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	if body.Auto == nil && body.Refresh == nil {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "auto", Code: "required", Message: "auto or refresh_on_renew is required"}}})
		return
	}
	if body.Auto != nil {
		if err := h.Service.SetRotationRenewal(r.Context(), userID, id, *body.Auto); err != nil {
			httpx.WriteProblem(w, r, err)
			return
		}
	}
	if body.Refresh != nil {
		if err := h.Service.SetRotationRefresh(r.Context(), userID, id, *body.Refresh); err != nil {
			httpx.WriteProblem(w, r, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// Proposal answers GET /me/rotations/{rotationId}/renewal-proposal: the next
// four kitchens suggested for the renewal and everything they can be swapped with.
func (h *RotationHandlers) Proposal(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := rotationTarget(w, r)
	if !ok {
		return
	}
	p, err := h.Service.RenewalProposal(r.Context(), userID, id, time.Now())
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", p)
}

// Renew answers POST /rotations/{rotationId}/renew: the next rotation, pending
// payment. The body may name the four plans (an edited proposal); without one
// the same four kitchens come back.
func (h *RotationHandlers) Renew(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := rotationTarget(w, r)
	if !ok {
		return
	}
	var body struct {
		PlanIDs []uuid.UUID `json:"plan_ids"`
	}
	if r.ContentLength != 0 {
		if err := httpx.DecodeJSON(r, &body); err != nil {
			httpx.WriteProblem(w, r, err)
			return
		}
	}
	rot, err := h.Service.RenewRotation(r.Context(), userID, id, time.Now(), subscriptions.RenewOptions{PlanIDs: body.PlanIDs})
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, "", rot)
}

// CheaperPlans answers GET /me/rotations/{rotationId}/cheaper-plans.
func (h *RotationHandlers) CheaperPlans(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := rotationTarget(w, r)
	if !ok {
		return
	}
	res, err := h.Service.CheaperPlansForRotation(r.Context(), userID, id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", res)
}

// ChangeAddresses answers PUT /rotations/{rotationId}/addresses.
func (h *RotationHandlers) ChangeAddresses(w http.ResponseWriter, r *http.Request) {
	userID, id, ok := rotationTarget(w, r)
	if !ok {
		return
	}
	changes, ok := decodeAddressChanges(w, r)
	if !ok {
		return
	}
	out, err := h.Service.ChangeRotationAddresses(r.Context(), userID, id, changes, time.Now())
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", out)
}
