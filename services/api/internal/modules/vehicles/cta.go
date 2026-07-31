package vehicles

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/seam/action"
)

// InquiryHandler owns the (inquire_listing, vehicle_listing) CTA.
//
// This file is the whole of Phase 6's contact with the feed. Registering it is
// one line in app.BuildRegistries; nothing in feed, ranking, moderation or
// media changes. That is the claim PLAN-VIDEO-FIRST §2 makes about the action
// seam — "the feed never learns what a car is" — and this is where it is either
// true or it is not.
//
// Unlike the booking and plan CTAs, it does not live in `catalog`. The reason
// is the one Phase 5 discovered: ownership follows the table. `service_offering`
// and `meal_plan` are catalogue rows that several verticals write into, so
// catalog owns those pairs and verticals contribute labels. `vehicle_listing`
// has exactly one owner and always will, so the handler lives with it.
type InquiryHandler struct {
	pool *pgxpool.Pool
}

func NewInquiryHandler(pool *pgxpool.Pool) *InquiryHandler { return &InquiryHandler{pool: pool} }

// Resolve renders the inquire CTA.
//
// Availability is answered before the tap, exactly as it is for booking and
// subscribing. The failure this prevents is specific to classifieds and it is
// the worst one in the category: tapping through to a car that sold three weeks
// ago. Every marketplace that lets that happen trains buyers to distrust the
// whole feed, because a sold car looks identical to a scam.
func (h *InquiryHandler) Resolve(ctx context.Context, a action.Action, v action.Viewer) (action.Resolved, error) {
	label, labelAr := "Message seller", "راسل البائع"
	label, labelAr = action.Override(label, labelAr, a.Label, a.LabelAr)

	res := action.Resolved{
		Label:     label,
		LabelAr:   labelAr,
		Available: true,
		Route:     fmt.Sprintf("/listings/%s/inquire", a.TargetID),
	}

	var (
		status       string
		priceMinor   int64
		currency     string
		year         int
		mileageKM    int
		regionalSpec string
		title        string
		sellerKind   string
		restricted   bool
		distanceM    *float64
	)

	err := h.pool.QueryRow(ctx, `
		SELECT l.status, l.price_minor, l.currency, l.year, l.mileage_km,
		       l.regional_spec, COALESCE(l.title, ''), l.seller_kind,
		       EXISTS (SELECT 1 FROM trust_scores ts
		                WHERE ts.subject_type = 'listing' AND ts.subject_id = l.id
		                  AND ts.band IN ('restricted', 'blocked')),
		       CASE
		           WHEN $2::float8 IS NULL OR l.point IS NULL THEN NULL
		           ELSE ST_Distance(l.point,
		                    ST_SetSRID(ST_MakePoint($3, $2), 4326)::geography)
		       END
		  FROM vehicle_listings l
		 WHERE l.id = $1`,
		a.TargetID, nullableFloat(v.Lat), nullableFloat(v.Lng),
	).Scan(&status, &priceMinor, &currency, &year, &mileageKM,
		&regionalSpec, &title, &sellerKind, &restricted, &distanceM)

	if errors.Is(err, pgx.ErrNoRows) {
		res.Available = false
		res.Reason = "This listing is no longer available"
		return res, nil
	}
	if err != nil {
		// Optimistic on error, same as the other two CTAs: the inquiry endpoint
		// refuses properly and can explain why. A greyed-out button caused by a
		// transient database blip is a conversion lost for no reason.
		return res, nil
	}

	// The price, year and mileage ride along so the tile renders "2021 · 95,000
	// km · AED 145,000" without a second round-trip. Regional spec is in there
	// too, and it belongs there: it swings resale 20–30% and a buyer who finds
	// out only on the detail page feels misled even when nothing was hidden.
	res.Meta = map[string]any{
		"price_minor":   priceMinor,
		"currency":      currency,
		"year":          year,
		"mileage_km":    mileageKM,
		"regional_spec": regionalSpec,
		"seller_kind":   sellerKind,
	}
	if title != "" {
		res.Meta["title"] = title
	}
	if distanceM != nil {
		res.Meta["distance_m"] = int64(*distanceM)
	}

	switch {
	case restricted:
		// No detail. Telling a scammer which signal caught them is free
		// tuning advice, and the buyer does not need to know why — they need
		// the button not to work.
		res.Available = false
		res.Reason = "This listing is under review"
	case Status(status) == StatusSold:
		res.Available = false
		res.Reason = "Sold"
	case Status(status) == StatusExpired:
		res.Available = false
		res.Reason = "This listing has expired"
	case !Status(status).Live():
		res.Available = false
		res.Reason = "This listing is not currently available"
	}
	return res, nil
}

// nullableFloat turns "no viewer location" into SQL NULL.
//
// Zero is a real coordinate — it is in the Gulf of Guinea — so passing 0
// through would compute a distance of four thousand kilometres for every
// anonymous viewer rather than declining to compute one.
func nullableFloat(f float64) any {
	if f == 0 {
		return nil
	}
	return f
}

// CTALabel is what this vertical contributes to the shared label map, in the
// same shape the other verticals use. It is not a label for a `catalog`-owned
// pair — vehicles owns its pair outright — but keeping the shape uniform means
// app.BuildRegistries reads the same for every vertical.
func CTALabel() action.Labels {
	return action.Labels{
		Label:       "Message seller",
		LabelAr:     "راسل البائع",
		RoutePrefix: "/listings",
		Verb:        "inquire",
	}
}
