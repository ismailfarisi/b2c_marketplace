package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/payments"
	"github.com/marketplace/api/internal/modules/subscriptions"
)

// RotationHandlers serve the four-kitchen rotation: preview the week order,
// buy it, pay once, and manage it.
type RotationHandlers struct {
	Service  *subscriptions.Service
	Payments *payments.Service
}

type rotationBody struct {
	PlanIDs   []uuid.UUID `json:"plan_ids"`
	AddressID uuid.UUID   `json:"address_id"`
	StartDate string      `json:"start_date"`
	Weekdays  []int       `json:"delivery_weekdays"`
	MealSlots []string    `json:"meal_slots"`
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
	return subscriptions.RotationInput{
		UserID: userID, AddressID: body.AddressID, PlanIDs: body.PlanIDs,
		StartDate: start, Weekdays: body.Weekdays, Slots: body.MealSlots,
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
		Reason string `json:"reason"`
	}
	if r.ContentLength != 0 {
		if err := httpx.DecodeJSON(r, &body); err != nil {
			httpx.WriteProblem(w, r, err)
			return
		}
	}
	if err := h.Service.CancelRotation(r.Context(), userID, id, body.Reason); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
