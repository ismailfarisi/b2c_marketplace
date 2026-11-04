package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/admin"
	"github.com/marketplace/api/internal/modules/support"
)

// SupportHandlers is the customer side of help tickets.
type SupportHandlers struct {
	Service *support.Service
}

func ticketParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "ticketId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func (h *SupportHandlers) ListMine(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	before, err := httpx.DecodeKeysetCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	items, next, err := h.Service.Mine(r.Context(), userID, before)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	for i := range items {
		items[i].UserID, items[i].Phone = nil, ""
	}
	var cursor *string
	if next != nil {
		c := next.Encode()
		cursor = &c
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items, "next_cursor": cursor})
}

func (h *SupportHandlers) Open(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	var body struct {
		Subject        string     `json:"subject"`
		Body           string     `json:"body"`
		SubscriptionID *uuid.UUID `json:"subscription_id"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	t, err := h.Service.Open(r.Context(), userID, body.Subject, body.Body, body.SubscriptionID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, "", t)
}

func (h *SupportHandlers) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	id, ok := ticketParam(w, r)
	if !ok {
		return
	}
	t, err := h.Service.Thread(r.Context(), id, &userID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", t)
}

func (h *SupportHandlers) Reply(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	id, ok := ticketParam(w, r)
	if !ok {
		return
	}
	var body struct {
		Body string `json:"body"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	t, err := h.Service.Reply(r.Context(), id, userID, &userID, body.Body)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", t)
}

// --- staff ------------------------------------------------------------------
// commerce.contact: the support role's permission to see a customer's phone
// and talk to them.

func (h *AdminHandlers) SupportQueue(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermCommerceContact); !ok {
		return
	}
	items, err := h.Support.Queue(r.Context(), r.URL.Query().Get("status"))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

func (h *AdminHandlers) SupportTicket(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermCommerceContact); !ok {
		return
	}
	id, ok := ticketParam(w, r)
	if !ok {
		return
	}
	t, err := h.Support.Thread(r.Context(), id, nil)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", t)
}

func (h *AdminHandlers) SupportReply(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermCommerceContact)
	if !ok {
		return
	}
	id, ok := ticketParam(w, r)
	if !ok {
		return
	}
	var body struct {
		Body string `json:"body"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	t, err := h.Support.Reply(r.Context(), id, grant.UserID, nil, body.Body)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	// Who answered a customer is part of the record; the message text is
	// already in the thread, so only the outcome is audited.
	h.record(r, grant, "support.reply", "support_ticket", id, "staff reply to customer", map[string]any{"status": t.Status})
	httpx.WriteJSON(w, http.StatusOK, "", t)
}

func (h *AdminHandlers) SupportStatus(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermCommerceContact)
	if !ok {
		return
	}
	id, ok := ticketParam(w, r)
	if !ok {
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	if err := h.Support.SetStatus(r.Context(), id, body.Status); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	h.record(r, grant, "support.status", "support_ticket", id, "staff set ticket "+body.Status, map[string]any{"status": body.Status})
	w.WriteHeader(http.StatusNoContent)
}
