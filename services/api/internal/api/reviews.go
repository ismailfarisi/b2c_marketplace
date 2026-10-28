package api

import (
	"net/http"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/admin"
)

// ---- customer -----------------------------------------------------------

func (h *SubscriptionHandlers) MyReview(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	subID, ok := uuidURLParam(w, r, "subscriptionId")
	if !ok {
		return
	}
	out, err := h.Reviews.Mine(r.Context(), userID, subID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", out)
}

func (h *SubscriptionHandlers) WriteReview(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	subID, ok := uuidURLParam(w, r, "subscriptionId")
	if !ok {
		return
	}
	var body struct {
		Rating  int    `json:"rating"`
		Comment string `json:"comment"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	out, err := h.Reviews.Write(r.Context(), userID, subID, body.Rating, body.Comment)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", out)
}

// ---- storefront ---------------------------------------------------------

func (h *MealsHandlers) KitchenReviews(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidURLParam(w, r, "branchId")
	if !ok {
		return
	}
	out, err := h.Reviews.Public(r.Context(), id, 20)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": out})
}

// ---- kitchen console ----------------------------------------------------

func (h *KitchenHandlers) ListReviews(w http.ResponseWriter, r *http.Request) {
	branchID, ok := h.authorise(w, r, false)
	if !ok {
		return
	}
	out, err := h.Reviews.ForKitchen(r.Context(), branchID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": out})
}

func (h *KitchenHandlers) ReplyReview(w http.ResponseWriter, r *http.Request) {
	branchID, ok := h.authorise(w, r, true)
	if !ok {
		return
	}
	reviewID, ok := uuidURLParam(w, r, "reviewId")
	if !ok {
		return
	}
	var body struct {
		Reply string `json:"reply"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	out, err := h.Reviews.Reply(r.Context(), branchID, reviewID, body.Reply)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", out)
}

// ---- admin --------------------------------------------------------------

func (h *AdminHandlers) MealReviews(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermCommerceRead); !ok {
		return
	}
	out, err := h.Reviews.Admin(r.Context(), r.URL.Query().Get("status"))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": out})
}

func (h *AdminHandlers) HideMealReview(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermCommerceWrite)
	if !ok {
		return
	}
	id, ok := h.merchID(w, r, "reviewId")
	if !ok {
		return
	}
	var body struct {
		Hidden bool   `json:"hidden"`
		Reason string `json:"reason"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	out, err := h.Reviews.SetHidden(r.Context(), grant.UserID, id, body.Hidden, body.Reason)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	action := "meal_review.restore"
	if body.Hidden {
		action = "meal_review.hide"
	}
	h.record(r, grant, action, "meal_review", id, body.Reason, map[string]any{"status": out.Status})
	httpx.WriteJSON(w, http.StatusOK, "", out)
}

func (h *AdminHandlers) FoodSafety(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermCommerceRead); !ok {
		return
	}
	id, ok := h.merchID(w, r, "branchId")
	if !ok {
		return
	}
	out, err := h.Reviews.GetFoodSafety(r.Context(), id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", out)
}

func (h *AdminHandlers) SetFoodSafety(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermCommerceWrite)
	if !ok {
		return
	}
	id, ok := h.merchID(w, r, "branchId")
	if !ok {
		return
	}
	var body struct {
		Verified  bool   `json:"verified"`
		Ref       string `json:"ref"`
		ExpiresOn string `json:"expires_on"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	out, err := h.Reviews.SetFoodSafety(r.Context(), grant.UserID, id, body.Verified, body.Ref, body.ExpiresOn)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	action := "food_safety.clear"
	if body.Verified {
		action = "food_safety.verify"
	}
	h.record(r, grant, action, "vendor_branch", id, "", map[string]any{"ref": out.Ref, "expires_on": out.ExpiresOn})
	httpx.WriteJSON(w, http.StatusOK, "", out)
}
