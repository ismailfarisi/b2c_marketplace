package api

import (
	"net/http"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/addresses"
	"github.com/marketplace/api/internal/modules/identity"
)

// GetMe is the account screen's profile header: who is signed in, on which
// number.
func (h *Handlers) GetMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	p, err := h.Identity.GetProfile(r.Context(), userID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", p)
}

// UpdateMe edits name, email and language. The phone number is the sign-in
// identity and changes only through support (identity.ChangePhone).
func (h *Handlers) UpdateMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	var in identity.ProfileInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	p, err := h.Identity.UpdateProfile(r.Context(), userID, in)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", p)
}

// Update sets the default address. Only is_default is editable here; changing
// where an address is means saving a new one, so past orders keep pointing at
// the place they were delivered to.
func (h *AddressHandlers) Update(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	id, ok := uuidURLParam(w, r, "addressId")
	if !ok {
		return
	}
	var body struct {
		IsDefault *bool `json:"is_default"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	if body.IsDefault == nil || !*body.IsDefault {
		httpx.WriteProblem(w, r, addresses.DefaultOnly(r.Context()))
		return
	}
	a, err := h.Addresses.SetDefault(r.Context(), userID, id)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", a)
}

// Preferences reads the marketing switches so the account screen can show
// them in their current position.
func (h *ComplianceHandlers) Preferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	items, err := h.Notify.Preferences(r.Context(), userID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}
