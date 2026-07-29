package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/admin"
	"github.com/marketplace/api/internal/modules/identity"
)

// Account lifecycle actions — merge, phone change, erasure.
//
// All three need `identity.review`, which is deliberately never in the same
// default role as `money.write`. Someone who can change the phone number on an
// account and someone who can move money out of it should be two people; if they
// are one, every account is one support ticket away from being drained.

// Accounts is the subset of identity.Service the console calls.
type Accounts interface {
	ChangePhone(ctx context.Context, in identity.ChangePhoneInput) error
	Merge(ctx context.Context, in identity.MergeInput) (*identity.MergeResult, error)
	Erase(ctx context.Context, in identity.EraseInput) error
}

func (h *AdminHandlers) ChangePhone(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermIdentityReview)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	var body struct {
		Phone  string `json:"phone"`
		Reason string `json:"reason"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	if err := h.Act.Accounts.ChangePhone(r.Context(), identity.ChangePhoneInput{
		UserID: id, Phone: body.Phone, ActorID: grant.UserID, Reason: body.Reason,
	}); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"status": "changed",
		// Both consequences named, because neither is obvious from the button
		// and both will generate a support call if unexpected.
		"note": "the new number is unverified until they sign in with it, and every existing " +
			"session has been revoked — they will be signed out on every device.",
	})
}

func (h *AdminHandlers) MergeAccounts(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermIdentityReview)
	if !ok {
		return
	}
	duplicate, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	var body struct {
		SurvivorID string `json:"survivor_id"`
		Reason     string `json:"reason"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	survivor, err := uuid.Parse(body.SurvivorID)
	if err != nil {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "survivor_id", Code: "invalid",
			Message: "the id of the account that keeps going is required",
		}}})
		return
	}

	result, err := h.Act.Accounts.Merge(r.Context(), identity.MergeInput{
		DuplicateID: duplicate, SurvivorID: survivor,
		ActorID: grant.UserID, Reason: body.Reason,
	})
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"status": "merged",
		"moved": map[string]any{
			"addresses":  result.AddressesMoved,
			"favourites": result.FavouritesMoved,
			"searches":   result.SearchesMoved,
		},
		// The cost, returned rather than buried. An operator who does not see
		// this discovers it when the customer asks where their receipts went.
		"stayed": map[string]any{
			"orders":   result.OrdersLeft,
			"invoices": result.InvoicesLeft,
		},
		"note": "orders and invoices stay with the account that earned them — an invoice " +
			"whose buyer changed after issue is a different document from the one the " +
			"customer holds. Their history is visible here, not in the app.",
	})
}

func (h *AdminHandlers) EraseAccount(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermIdentityReview)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "userId"))
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

	if err := h.Act.Accounts.Erase(r.Context(), identity.EraseInput{
		UserID: id, ActorID: grant.UserID, Reason: body.Reason,
	}); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"status": "erased",
		"note": "name, email, phone, addresses and device tokens are gone and cannot be " +
			"recovered. Invoices and ledger entries are retained as the law requires, " +
			"attached to a subject that no longer resolves to a person.",
	})
}
