package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/console"
	"github.com/marketplace/api/internal/modules/kitchen"
)

// KitchenHandlers is the vendor portal's setup surface: dishes, the weekly
// menu, plans and the kitchen's storefront profile.
//
// Reads are open to every member of the branch. Writes need owner or manager —
// a staff account runs the day's queue and does not reprice the plans. A
// non-member gets 404 from AuthorizeBranch before the role is ever looked at,
// so the 403 below only ever tells a member something they already know.
type KitchenHandlers struct {
	Kitchen *kitchen.Service
	Console *console.Service
}

func (h *KitchenHandlers) authorise(w http.ResponseWriter, r *http.Request, write bool) (uuid.UUID, bool) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return uuid.Nil, false
	}
	branchID, ok := branchParam(w, r)
	if !ok {
		return uuid.Nil, false
	}
	_, role, err := h.Console.BranchRole(r.Context(), userID, branchID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return uuid.Nil, false
	}
	if write && !kitchen.CanEdit(role) {
		httpx.WriteProblem(w, r, httpx.Coded(httpx.ErrForbidden, "role_read_only",
			"Only an owner or manager can change the menu, plans or profile."))
		return uuid.Nil, false
	}
	return branchID, true
}

func uuidURLParam(w http.ResponseWriter, r *http.Request, key string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, key))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func (h *KitchenHandlers) MyBranches(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	items, err := h.Console.Branches(r.Context(), userID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

func (h *KitchenHandlers) Overview(w http.ResponseWriter, r *http.Request) {
	branchID, ok := h.authorise(w, r, false)
	if !ok {
		return
	}
	o, err := h.Kitchen.Overview(r.Context(), branchID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", o)
}

func (h *KitchenHandlers) ListDishes(w http.ResponseWriter, r *http.Request) {
	branchID, ok := h.authorise(w, r, false)
	if !ok {
		return
	}
	items, err := h.Kitchen.ListDishes(r.Context(), branchID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items, "allergens": kitchen.Allergens})
}

func (h *KitchenHandlers) CreateDish(w http.ResponseWriter, r *http.Request) {
	branchID, ok := h.authorise(w, r, true)
	if !ok {
		return
	}
	var body kitchen.DishInput
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	d, err := h.Kitchen.CreateDish(r.Context(), branchID, body)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, "", d)
}

func (h *KitchenHandlers) UpdateDish(w http.ResponseWriter, r *http.Request) {
	branchID, ok := h.authorise(w, r, true)
	if !ok {
		return
	}
	dishID, ok := uuidURLParam(w, r, "dishId")
	if !ok {
		return
	}
	var body kitchen.DishInput
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	d, err := h.Kitchen.UpdateDish(r.Context(), branchID, dishID, body)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", d)
}

func (h *KitchenHandlers) Menu(w http.ResponseWriter, r *http.Request) {
	branchID, ok := h.authorise(w, r, false)
	if !ok {
		return
	}
	m, err := h.Kitchen.WeeklyMenu(r.Context(), branchID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", m)
}

func (h *KitchenHandlers) SetMenuSlot(w http.ResponseWriter, r *http.Request) {
	branchID, ok := h.authorise(w, r, true)
	if !ok {
		return
	}
	weekday, err := strconv.Atoi(chi.URLParam(r, "weekday"))
	if err != nil {
		weekday = -1
	}
	var body struct {
		DefaultDishID     uuid.NullUUID `json:"default_dish_id"`
		AlternativeDishID uuid.NullUUID `json:"alternative_dish_id"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	m, err := h.Kitchen.SetMenuSlot(r.Context(), branchID, weekday, chi.URLParam(r, "slot"),
		body.DefaultDishID, body.AlternativeDishID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", m)
}

func (h *KitchenHandlers) ListPlans(w http.ResponseWriter, r *http.Request) {
	branchID, ok := h.authorise(w, r, false)
	if !ok {
		return
	}
	items, err := h.Kitchen.ListPlans(r.Context(), branchID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

func (h *KitchenHandlers) GetPlan(w http.ResponseWriter, r *http.Request) {
	branchID, ok := h.authorise(w, r, false)
	if !ok {
		return
	}
	planID, ok := uuidURLParam(w, r, "planId")
	if !ok {
		return
	}
	p, err := h.Kitchen.GetPlan(r.Context(), branchID, planID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", p)
}

func (h *KitchenHandlers) CreatePlan(w http.ResponseWriter, r *http.Request) {
	branchID, ok := h.authorise(w, r, true)
	if !ok {
		return
	}
	var body kitchen.PlanInput
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	p, err := h.Kitchen.CreatePlan(r.Context(), branchID, body)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, "", p)
}

func (h *KitchenHandlers) UpdatePlan(w http.ResponseWriter, r *http.Request) {
	branchID, ok := h.authorise(w, r, true)
	if !ok {
		return
	}
	planID, ok := uuidURLParam(w, r, "planId")
	if !ok {
		return
	}
	var body kitchen.PlanInput
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	p, err := h.Kitchen.UpdatePlan(r.Context(), branchID, planID, body)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", p)
}

func (h *KitchenHandlers) Profile(w http.ResponseWriter, r *http.Request) {
	branchID, ok := h.authorise(w, r, false)
	if !ok {
		return
	}
	p, err := h.Kitchen.Profile(r.Context(), branchID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", p)
}

func (h *KitchenHandlers) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	branchID, ok := h.authorise(w, r, true)
	if !ok {
		return
	}
	var body kitchen.ProfileInput
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	p, err := h.Kitchen.UpdateProfile(r.Context(), branchID, body)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", p)
}
