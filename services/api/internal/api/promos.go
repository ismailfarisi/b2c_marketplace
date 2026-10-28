package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/admin"
	"github.com/marketplace/api/internal/modules/promo"
)

// ---- storefront ---------------------------------------------------------

// Offers answers GET /meals/kitchens/{branchId}/offers: the public promos and
// trials usable at a kitchen right now.
func (h *MealsHandlers) Offers(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "branchId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	out, err := h.Promos.Offers(r.Context(), id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": out})
}

// ---- admin --------------------------------------------------------------
//
// Promos: reads need commerce.read, edits commerce.write. A promo spends the
// platform's money, so every write is audited.

func (h *AdminHandlers) ListPromos(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermCommerceRead); !ok {
		return
	}
	out, err := h.Promos.List(r.Context())
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": out})
}

func (h *AdminHandlers) Promo(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermCommerceRead); !ok {
		return
	}
	id, ok := h.merchID(w, r, "promoId")
	if !ok {
		return
	}
	p, err := h.Promos.Get(r.Context(), id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", p)
}

func (h *AdminHandlers) SavePromo(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermCommerceWrite)
	if !ok {
		return
	}
	var id *uuid.UUID
	if chi.URLParam(r, "promoId") != "" {
		parsed, ok := h.merchID(w, r, "promoId")
		if !ok {
			return
		}
		id = &parsed
	}
	var in promo.Input
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	p, err := h.Promos.Save(r.Context(), id, grant.UserID, in)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	action, status := "promo.update", http.StatusOK
	if id == nil {
		action, status = "promo.create", http.StatusCreated
	}
	h.record(r, grant, action, "promo", p.ID, "", map[string]any{
		"name": p.Name, "code": p.Code, "discount_type": p.DiscountType,
		"percent_bp": p.PercentBP, "amount_minor": p.AmountMinor, "trial_days": p.TrialDays,
		"kitchen_share_bp": p.KitchenShareBP, "is_active": p.IsActive,
	})
	httpx.WriteJSON(w, status, "", p)
}

func (h *AdminHandlers) DeletePromo(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermCommerceWrite)
	if !ok {
		return
	}
	id, ok := h.merchID(w, r, "promoId")
	if !ok {
		return
	}
	if err := h.Promos.Delete(r.Context(), id); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	h.record(r, grant, "promo.delete", "promo", id, "", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (h *AdminHandlers) PromoRedemptions(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermCommerceRead); !ok {
		return
	}
	id, ok := h.merchID(w, r, "promoId")
	if !ok {
		return
	}
	out, err := h.Promos.Redemptions(r.Context(), id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": out})
}
