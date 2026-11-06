package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/taste"
)

// TasteHandlers serve the signed-in customer's taste memory and meal preferences.
type TasteHandlers struct {
	Taste *taste.Service
}

// List answers GET /me/tastes.
func (h *TasteHandlers) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	items, err := h.Taste.List(r.Context(), userID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

// Set answers PUT /me/tastes: heart or "not again" a kitchen or dish.
func (h *TasteHandlers) Set(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	var body struct {
		SubjectKind string    `json:"subject_kind"`
		SubjectID   uuid.UUID `json:"subject_id"`
		Sentiment   string    `json:"sentiment"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	t, err := h.Taste.Set(r.Context(), userID, body.SubjectKind, body.SubjectID, body.Sentiment)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", t)
}

// Clear answers DELETE /me/tastes/{kind}/{subjectId}.
func (h *TasteHandlers) Clear(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	subjectID, err := uuid.Parse(chi.URLParam(r, "subjectId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	if err := h.Taste.Clear(r.Context(), userID, chi.URLParam(r, "kind"), subjectID); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Preferences answers GET /me/meal-preferences.
func (h *TasteHandlers) Preferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	p, err := h.Taste.Preferences(r.Context(), userID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", p)
}

// SetPreferences answers PUT /me/meal-preferences.
func (h *TasteHandlers) SetPreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	var body struct {
		Adventure string `json:"adventure"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	p, err := h.Taste.SetPreferences(r.Context(), userID, body.Adventure)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", p)
}
