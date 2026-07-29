package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/admin"
)

// Configuration and health — PLAN-ADMIN-CONSOLE §4.5 and §4.6.
//
// Phase D removes the last routine reason to open `psql` against production.
// Phase E answers the question the plan puts most sharply: a worker that stops
// is invisible today. The API stays green, `/healthz` returns ok, and routes go
// unplanned until a customer rings.

func (h *AdminHandlers) Flags(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermHealthRead)
	if !ok {
		return
	}
	flags, err := h.Admin.ListFlags(r.Context(), grant.UserID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	items := make([]map[string]any, 0, len(flags))
	for _, f := range flags {
		items = append(items, map[string]any{
			"key": f.Key, "description": f.Description,
			"enabled": f.IsEnabled, "rollout_pct": f.RolloutPct,
			"enabled_for": len(f.EnabledFor),
			"updated_by":  nilIfBlank(f.UpdatedBy),
			"updated_at":  f.UpdatedAt,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

func (h *AdminHandlers) SetFlag(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermConfigWrite)
	if !ok {
		return
	}
	var body struct {
		Enabled    bool   `json:"enabled"`
		RolloutPct int    `json:"rollout_pct"`
		Reason     string `json:"reason"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	if err := h.Admin.SetFlag(r.Context(), grant.UserID, admin.SetFlagInput{
		Key:        chi.URLParam(r, "key"),
		IsEnabled:  body.Enabled,
		RolloutPct: body.RolloutPct,
		Reason:     body.Reason,
	}); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"status": "updated"})
}

func (h *AdminHandlers) Settings(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermHealthRead)
	if !ok {
		return
	}
	settings, err := h.Admin.ListSettings(r.Context(), grant.UserID)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	items := make([]map[string]any, 0, len(settings))
	for _, s := range settings {
		items = append(items, map[string]any{
			"key": s.Key, "value": json.RawMessage(s.Value),
			"description": s.Description, "sensitive": s.IsSensitive,
			"updated_by": nilIfBlank(s.UpdatedBy), "updated_at": s.UpdatedAt,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}

func (h *AdminHandlers) SetSetting(w http.ResponseWriter, r *http.Request) {
	grant, ok := h.caller(w, r, admin.PermConfigWrite)
	if !ok {
		return
	}
	var body struct {
		Value  json.RawMessage `json:"value"`
		Reason string          `json:"reason"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	if err := h.Admin.SetSetting(r.Context(), grant.UserID, admin.SetSettingInput{
		Key:    chi.URLParam(r, "key"),
		Value:  body.Value,
		Reason: body.Reason,
	}); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"status": "updated"})
}

// Health is one request for the whole page.
//
// Five queries composed server-side rather than five endpoints, for the §3.6
// reason and one more specific to this page: these numbers are read *together*.
// "Is the outbox behind" and "did notifications stop" are the same question
// asked twice, and five independent fetches settling at five different moments
// give an operator five different instants to reason about during an incident.
func (h *AdminHandlers) Health(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r, admin.PermHealthRead); !ok {
		return
	}
	ctx := r.Context()

	lag, err := h.View.LoadOutboxLag(ctx)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	outbox := make([]map[string]any, 0, len(lag))
	for _, l := range lag {
		var age any
		if l.OldestAge != nil {
			age = int(l.OldestAge.Seconds())
		}
		outbox = append(outbox, map[string]any{
			"aggregate_type": l.AggregateType,
			"unpublished":    l.Unpublished,
			// Age, not count. Ten thousand events published within the last
			// minute is a busy system; three from yesterday is a dead consumer,
			// and only the age tells them apart.
			"oldest_seconds": age,
			"failing":        l.Failing,
			"max_attempts":   l.MaxAttempt,
			"last_error":     nilIfBlank(l.LastError),
		})
	}

	parts, err := h.View.LoadPartitions(ctx)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	partitions := make([]map[string]any, 0, len(parts))
	for _, p := range parts {
		partitions = append(partitions, map[string]any{
			"parent": p.Parent, "name": p.Name, "bound": p.UpperBound,
			// A DEFAULT partition holding rows is the worst case on this page:
			// once a row lands there the range it occupies can never be
			// attached as a real partition without moving it, so a late
			// partition job becomes a permanent one.
			"is_default": p.IsDefault,
			"rows":       p.LiveRows,
			"trapped":    p.IsDefault && p.LiveRows > 0,
		})
	}

	loops, _ := h.View.LoadLoopHealth(ctx)
	loopItems := make([]map[string]any, 0, len(loops))
	for _, l := range loops {
		loopItems = append(loopItems, map[string]any{
			"loop": l.Loop, "last_seen": l.LastSeen,
			"pending": l.Pending, "stale": l.Stale,
		})
	}

	delivery, _ := h.View.LoadDeliveryStats(ctx, 24*time.Hour)
	deliveryItems := make([]map[string]any, 0, len(delivery))
	for _, d := range delivery {
		deliveryItems = append(deliveryItems, map[string]any{
			"channel": d.Channel, "sent": d.Sent, "failed": d.Failed,
			"suppressed": d.Suppressed, "queued": d.Queued,
			"last_sent_at": d.LastSentAt,
		})
	}

	sla, _ := h.View.LoadModerationSLA(ctx, intParam(r.URL.Query().Get("sla_days"), 14))
	slaItems := make([]map[string]any, 0, len(sla))
	for _, s := range sla {
		slaItems = append(slaItems, map[string]any{
			"day": s.Day.Format("2006-01-02"), "decided": s.Decided,
			"breached": s.Breached, "median_seconds": s.MedianSec,
		})
	}

	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
		"outbox":       outbox,
		"partitions":   partitions,
		"loops":        loopItems,
		"delivery":     deliveryItems,
		"moderation":   slaItems,
		"generated_at": time.Now(),
	})
}
