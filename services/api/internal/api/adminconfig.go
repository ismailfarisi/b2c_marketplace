package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/admin"
)

// Versioned configuration — PLAN-ADMIN-CONSOLE §4.5.
//
// Tax rates, boost rate cards and notification templates. All three were left
// read-only in the first pass with the reason recorded: editing in place would
// restate documents already issued. These endpoints **supersede** rather than
// update — the old row is closed and a new one opened, in one transaction — so
// an invoice raised last month still references the rate that was in force when
// it was raised.

func (h *AdminHandlers) TaxRates(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermHealthRead)
	if !ok {
		return
	}
	rates, err := h.Admin.ListTaxRates(r.Context(), grant.UserID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(rates))
	for _, t := range rates {
		items = append(items, map[string]any{
			"id": t.ID, "country": t.Country, "region": nilIfBlank(t.Region),
			"category": t.Category, "rate_bp": t.RateBP, "label": t.Label,
			"from": t.From.Format("2006-01-02"), "to": dateOrNil(t.To),
			"current": t.Current,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

func (h *AdminHandlers) SupersedeTaxRate(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermConfigWrite)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "rateId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	var body struct {
		RateBP        int    `json:"rate_bp"`
		Label         string `json:"label"`
		EffectiveFrom string `json:"effective_from"`
		Reason        string `json:"reason"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	from, err := parseDay(body.EffectiveFrom)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	if err := h.Admin.SupersedeTaxRate(r.Context(), grant.UserID, admin.SupersedeTaxRateInput{
		ReplacesID: id, RateBP: body.RateBP, Label: body.Label,
		EffectiveFrom: from, Reason: body.Reason,
	}); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"status": "superseded",
		"note": "the old rate is closed, not deleted. Documents issued under it still " +
			"reference the rate that was in force when they were issued.",
	})
}

func (h *AdminHandlers) RateCards(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermHealthRead)
	if !ok {
		return
	}
	cards, err := h.Admin.ListRateCards(r.Context(), grant.UserID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(cards))
	for _, c := range cards {
		items = append(items, map[string]any{
			"id": c.ID, "placement": c.Placement, "country": c.Country,
			"region": nilIfBlank(c.Region),
			"floor":  amount(int64(c.FloorCPM), c.Currency),
			"from":   c.From.Format("2006-01-02"), "to": dateOrNil(c.To),
			"current": c.Current,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

func (h *AdminHandlers) SupersedeRateCard(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermConfigWrite)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "cardId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	var body struct {
		FloorCPMMinor int    `json:"floor_cpm_minor"`
		EffectiveFrom string `json:"effective_from"`
		Reason        string `json:"reason"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	from, err := parseDay(body.EffectiveFrom)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	if err := h.Admin.SupersedeRateCard(r.Context(), grant.UserID, admin.SupersedeRateCardInput{
		ReplacesID: id, FloorCPM: body.FloorCPMMinor,
		EffectiveFrom: from, Reason: body.Reason,
	}); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"status": "superseded"})
}

func (h *AdminHandlers) Templates(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermHealthRead)
	if !ok {
		return
	}
	templates, err := h.Admin.ListTemplates(r.Context(), grant.UserID,
		r.URL.Query().Get("all") != "true")
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(templates))
	for _, t := range templates {
		items = append(items, map[string]any{
			"id": t.ID, "key": t.Key, "channel": t.Channel, "locale": t.Locale,
			"version": t.Version, "category": t.Category,
			"subject": nilIfBlank(t.Subject), "body": t.Body,
			"active": t.IsActive,
			// The cost of one send, carried with the text. Arabic sits entirely
			// outside GSM-7, so every Arabic template is UCS-2 and its budget is
			// 70 characters rather than 160 — which a character count in an
			// editor hides completely.
			"segments":     t.Segments,
			"placeholders": t.Placeholders,
			"updated_at":   t.UpdatedAt,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

func (h *AdminHandlers) ReviseTemplate(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermConfigWrite)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "templateId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	var body struct {
		Subject string `json:"subject"`
		Body    string `json:"body"`
		Reason  string `json:"reason"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	if err := h.Admin.ReviseTemplate(r.Context(), grant.UserID, admin.ReviseTemplateInput{
		ReplacesID: id, Subject: body.Subject, Body: body.Body, Reason: body.Reason,
	}); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"status":   "revised",
		"segments": admin.SMSSegments(body.Body),
		"note": "the previous version is kept and deactivated. A message already sent can " +
			"still be shown as the text that was actually sent.",
	})
}

// PreviewSegments costs a body without saving it.
//
// Exists so the editor can show the segment count as the operator types, which
// is the only moment the number changes anyone's mind. Computed server-side
// rather than in the browser, so the number shown and the number charged come
// from the same code.
func (h *AdminHandlers) PreviewSegments(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermHealthRead); !ok {
		return
	}
	var body struct {
		Body string `json:"body"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"segments":     admin.SMSSegments(body.Body),
		"characters":   len([]rune(body.Body)),
		"placeholders": admin.Placeholders(body.Body),
	})
}

func parseDay(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "effective_from", Code: "invalid",
			Message: "a date is required, as YYYY-MM-DD",
		}}}
	}
	return t, nil
}
