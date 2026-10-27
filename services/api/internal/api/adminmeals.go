package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/admin"
	"github.com/marketplace/api/internal/modules/kitchen"
	"github.com/marketplace/api/internal/modules/subscriptions"
)

// Meals: the staff view of kitchens and subscriptions. Reads need
// commerce.read; changing a plan's sale state or cancelling a subscription
// needs commerce.write, and both are audited.

func (h *AdminHandlers) MealKitchens(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermCommerceRead); !ok {
		return
	}
	items, err := h.Kitchen.AdminKitchens(r.Context(), r.URL.Query().Get("q"),
		r.URL.Query().Get("rotation") == "true")
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	if items == nil {
		items = []kitchen.KitchenSummary{}
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

func (h *AdminHandlers) MealKitchen(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermCommerceRead); !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "branchId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	ctx := r.Context()
	profile, err := h.Kitchen.Profile(ctx, id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	overview, err := h.Kitchen.Overview(ctx, id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	menu, err := h.Kitchen.WeeklyMenu(ctx, id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	plans, err := h.Kitchen.ListPlans(ctx, id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	dishes, err := h.Kitchen.ListDishes(ctx, id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"profile": profile, "overview": overview, "menu": menu, "plans": plans, "dishes": dishes,
	})
}

func (h *AdminHandlers) SetMealPlanActive(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermCommerceWrite)
	if !ok {
		return
	}
	planID, err := uuid.Parse(chi.URLParam(r, "planId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	var body struct {
		Active *bool  `json:"active"`
		Reason string `json:"reason"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	if body.Active == nil {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{Field: "active", Code: "required"}}})
		return
	}
	if len(body.Reason) < 4 {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{Field: "reason", Code: "required"}}})
		return
	}
	plan, err := h.Kitchen.SetPlanActive(r.Context(), planID, *body.Active)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	action := "meal_plan.withdraw"
	if *body.Active {
		action = "meal_plan.restore"
	}
	h.record(r, grant, action, "subscription_plan", planID, body.Reason, map[string]any{"is_active": *body.Active})
	httpx.WriteJSON(w, http.StatusOK, "", plan)
}

func (h *AdminHandlers) MealSubscriptions(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermCommerceRead); !ok {
		return
	}
	q := r.URL.Query()
	f := subscriptions.AdminFilter{Status: q.Get("status"), Query: q.Get("q")}
	if b := q.Get("branch_id"); b != "" {
		id, err := uuid.Parse(b)
		if err != nil {
			httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{Field: "branch_id", Code: "invalid"}}})
			return
		}
		f.BranchID = uuid.NullUUID{UUID: id, Valid: true}
	}
	if l, err := strconv.Atoi(q.Get("limit")); err == nil {
		f.Limit = l
	}
	items, err := h.Subscriptions.AdminList(r.Context(), f)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	if items == nil {
		items = []subscriptions.AdminRow{}
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

func (h *AdminHandlers) MealSubscription(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermCommerceRead); !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "subscriptionId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	d, err := h.Subscriptions.AdminDetail(r.Context(), id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", d)
}

func (h *AdminHandlers) CancelMealSubscription(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermCommerceWrite)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "subscriptionId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	if len(body.Reason) < 4 {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{Field: "reason", Code: "required"}}})
		return
	}
	if err := h.Subscriptions.CancelByStaff(r.Context(), grant.UserID, id, body.Reason); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	h.record(r, grant, "subscription.cancel", "subscription", id, body.Reason, nil)
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"status": "cancelled",
		"note":   "future deliveries cancelled and the unearned part of the paid cycle credited back. Delivered meals stay delivered.",
	})
}
