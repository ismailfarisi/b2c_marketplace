package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/auth"
	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/admin"
)

// View-as — PLAN-ADMIN-CONSOLE §3.7.
//
// "The vendor says the button is missing" is otherwise unanswerable, and an
// agent guessing at what a customer can see is an agent who eventually tells
// them something untrue.
//
// The read-only guarantee is not made here. It is made once, by
// `auth.ReadOnlyWhenImpersonating` on the whole authenticated router, which
// refuses every unsafe method for a view token before routing. This handler only
// mints the token and records that it did — if the guarantee lived in handlers,
// it would protect the handlers somebody remembered.

func (h *AdminHandlers) ViewAs(w http.ResponseWriter, r *http.Request) {
	// `commerce.contact` rather than a permission of its own, and deliberately.
	// Seeing the app as a customer reveals exactly what seeing their record
	// reveals — their addresses, their orders, their name — so a separate
	// permission would let someone hold one and not the other while the
	// disclosure is identical. The audit is what distinguishes them, not the
	// gate.
	grant, ok := h.caller(w, r, admin.PermCommerceContact)
	if !ok {
		return
	}
	if h.Issuer == nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	subject, err := uuid.Parse(chi.URLParam(r, "userId"))
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
	if body.Reason == "" {
		httpx.WriteProblem(w, r, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "reason", Code: "required",
			Message: "wearing a customer's identity needs a stated reason — this is the " +
				"line in the audit log that explains why someone was inside their account",
		}}})
		return
	}

	// Refuse to view as another staff member. The token cannot reach /admin
	// anyway (`RejectImpersonation` guards that subtree), but issuing it at all
	// would put a staff member's identity into a token held by someone else,
	// and the next question is always whether that token can be widened.
	if staff, err := h.Admin.IsStaff(r.Context(), subject); err == nil && staff {
		httpx.WriteProblem(w, r, httpx.ErrForbidden)
		return
	}

	locale := "en"
	if l, err := h.View.UserLocale(r.Context(), subject); err == nil && l != "" {
		// The customer's own locale, not the agent's. An agent debugging "the
		// text is wrong" in English while the customer reads Arabic is
		// debugging a different screen.
		locale = l
	}

	token, err := h.Issuer.IssueViewToken(subject, grant.UserID, locale)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	// Recorded before the token is returned, so a failure to record is a failure
	// to impersonate rather than an unlogged impersonation.
	if err := h.Admin.RecordOutside(r.Context(), admin.Action{
		ActorID:     grant.UserID,
		Action:      "user.view_as",
		SubjectType: "user",
		SubjectID:   uuid.NullUUID{UUID: subject, Valid: true},
		Reason:      body.Reason,
		After: map[string]any{
			"expires_in_seconds": int(auth.ViewTTL.Seconds()),
			"locale":             locale,
		},
		RequestID: httpx.RequestIDFromContext(r.Context()),
	}); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"access_token": token,
		"expires_in":   int(auth.ViewTTL.Seconds()),
		"locale":       locale,
		"subject_id":   subject,
		"read_only":    true,
		"note": "read-only and not refreshable. Every write is refused server-side, not " +
			"hidden in the interface — if something needs doing on their behalf, do it " +
			"from the console with your own name on it.",
	})
}
