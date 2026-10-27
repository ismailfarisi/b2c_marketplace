package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/addresses"
)

// AddressHandlers is the customer's saved delivery addresses.
type AddressHandlers struct {
	Addresses *addresses.Service
}

func (h *AddressHandlers) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	items, err := h.Addresses.List(r.Context(), userID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

func (h *AddressHandlers) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	var in addresses.Input
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	a, err := h.Addresses.Create(r.Context(), userID, in)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, "", a)
}

func (h *AddressHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	id, ok := uuidURLParam(w, r, "addressId")
	if !ok {
		return
	}
	if err := h.Addresses.Delete(r.Context(), userID, id); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Mine is the customer's "My subscriptions".
func (h *SubscriptionHandlers) Mine(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	items, err := h.Service.Mine(r.Context(), userID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

func (h *SubscriptionHandlers) MyDetail(w http.ResponseWriter, r *http.Request) {
	userID, subID, ok := userAndSubscription(w, r)
	if !ok {
		return
	}
	d, err := h.Service.MyDetail(r.Context(), userID, subID, time.Now())
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", d)
}

// ChooseMeal picks between the day's default dish and its alternative.
func (h *SubscriptionHandlers) ChooseMeal(w http.ResponseWriter, r *http.Request) {
	userID, subID, ok := userAndSubscription(w, r)
	if !ok {
		return
	}
	date, err := time.Parse("2006-01-02", chi.URLParam(r, "date"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	var body struct {
		DishID string `json:"dish_id"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	dishID, err := uuid.Parse(body.DishID)
	if err != nil {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "dish_id", Code: "required", Message: "choose a dish",
		}}})
		return
	}
	meal, err := h.Service.ChooseMeal(r.Context(), userID, subID, date, chi.URLParam(r, "slot"), dishID, time.Now())
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", meal)
}

// PrepSheet is the kitchen's dish counts for one day.
func (h *KitchenHandlers) PrepSheet(w http.ResponseWriter, r *http.Request) {
	branchID, ok := h.authorise(w, r, false)
	if !ok {
		return
	}
	date := time.Now()
	if d := r.URL.Query().Get("date"); d != "" {
		var err error
		if date, err = time.Parse("2006-01-02", d); err != nil {
			httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "date", Code: "invalid_date", Message: "expected YYYY-MM-DD",
			}}})
			return
		}
	}
	lines, err := h.Kitchen.PrepSheet(r.Context(), branchID, date)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"date": date.Format("2006-01-02"), "items": lines,
	})
}
