package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/seam/action"
)

// BookingHandler owns the (book_service, service_offering) CTA for every
// vertical.
//
// It moved here in Phase 5, and the move is a finding rather than a tidy-up.
// Phase 3 let laundry register this pair because laundry was the only vertical;
// the moment meals arrived, the second registration would have panicked at boot
// on a duplicate — which is the registry working correctly and telling us the
// key was wrong. A meal box is a service offering. So is a haircut.
//
// So the pair is owned once, by the module that owns the table, and each
// vertical contributes only its words through action.Labels. The verticals
// still import nothing but the seam, which was the property worth protecting.
type BookingHandler struct {
	pool        *pgxpool.Pool
	eligibility *Eligibility
	labels      map[string]action.Labels
}

func NewBookingHandler(pool *pgxpool.Pool, eligibility *Eligibility, labels map[string]action.Labels) *BookingHandler {
	if labels == nil {
		labels = map[string]action.Labels{}
	}
	return &BookingHandler{pool: pool, eligibility: eligibility, labels: labels}
}

// defaultLabels are used for an offering whose service type contributed none.
//
// Generic rather than absent: a vertical that forgets to register its words
// should produce a working button with a dull label, not a blank one. A CTA
// with no text is indistinguishable from a rendering bug.
var defaultLabels = action.Labels{
	Label: "Book", LabelAr: "احجز", RoutePrefix: "/offerings", Verb: "book",
}

func (h *BookingHandler) Resolve(ctx context.Context, a action.Action, v action.Viewer) (action.Resolved, error) {
	labels := defaultLabels
	if serviceType, err := h.serviceTypeOf(ctx, a.TargetID); err == nil {
		if l, ok := h.labels[serviceType]; ok {
			labels = l
		}
	}

	label, labelAr := labels.Label, labels.LabelAr
	// The author's own words win over the vertical's default. A vendor who
	// wrote "Get tonight's biryani" knows their audience better than a table of
	// generic verbs does.
	if a.Label != "" {
		label, labelAr = a.Label, a.LabelAr
	}

	res := action.Resolved{
		Label:     label,
		LabelAr:   labelAr,
		Available: true,
		Route:     fmt.Sprintf("%s/%s/%s", labels.RoutePrefix, a.TargetID, labels.Verb),
	}

	if h.eligibility == nil {
		return res, nil
	}

	ok, reason, err := h.eligibility.OfferingBookable(ctx, a.TargetID, v.Lat, v.Lng)
	if err != nil {
		// Render optimistically on error. The booking flow refuses properly and
		// can explain why; a feed tile cannot, and blanking CTAs across a page
		// because one query timed out is a far more visible failure than one
		// customer reaching a screen that says no.
		return res, nil
	}
	if !ok {
		res.Available = false
		res.Reason = reason
	}
	return res, nil
}

// serviceTypeOf resolves which vertical an offering belongs to.
//
// A second query per CTA, which on a 20-reel page is up to 60 round-trips —
// exactly the N+1 the eligibility check was written as one query to avoid. It
// is acceptable only because it is a tiny primary-key lookup that Postgres
// serves from cache, and it is flagged here because the moment CTAs have real
// volume this belongs in the same query as the eligibility check, or in
// ResolveBatch. See docs/PHASE-5-FOOD-AND-SUBSCRIPTIONS.md §7.
func (h *BookingHandler) serviceTypeOf(ctx context.Context, offeringID uuid.UUID) (string, error) {
	var serviceType string
	err := h.pool.QueryRow(ctx,
		`SELECT service_type FROM service_offerings WHERE id = $1`, offeringID).Scan(&serviceType)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	if err != nil {
		return "", fmt.Errorf("catalog: offering service type: %w", err)
	}
	return serviceType, nil
}
