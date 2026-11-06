package api

import (
	"net/http"
	"time"

	"github.com/marketplace/api/internal/httpx"
)

// NextWeek handles GET /me/next-week: who cooks next week and what, per
// active subscription or rotation week.
func (h *RotationHandlers) NextWeek(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	items, err := h.Service.NextWeek(r.Context(), userID, time.Now())
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

// Recaps handles GET /me/rotation-recaps: the end-of-cycle recap of each
// rotation from its week 4 until a few weeks after it ends.
func (h *RotationHandlers) Recaps(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	items, err := h.Service.MyRecaps(r.Context(), userID, time.Now())
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}
