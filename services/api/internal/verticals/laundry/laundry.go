// Package laundry implements the first vertical.
//
// It exists to prove the engine, not to launch a business — PLAN-VIDEO-FIRST §5
// ranks laundry last for feed suitability, and docs/README.md open decision #3
// notes that no phase actually ships it to customers. It is here because it is
// the most demanding *shape* available: two fulfillments, a price that is not
// final at booking, and a capacity pool that is genuinely scarce. A vertical
// that fits this engine fits meals and vehicles trivially.
//
// This package contains four types and no other logic. If it ever needs to
// import ordering, pricing or logistics, the seam has leaked.
package laundry

import (
	"context"
	"fmt"

	"github.com/marketplace/api/internal/money"
	"github.com/marketplace/api/internal/seam/action"
	"github.com/marketplace/api/internal/seam/fulfillment"
	"github.com/marketplace/api/internal/seam/servicetype"
)

const (
	TypeWashFold = "laundry_wash_fold"
	TypeDryClean = "laundry_dry_clean"

	resourceKey = "pickup_slot"

	// A pickup consumes one slot up to this many garments; beyond it, the van
	// load justifies a second. Flat 1-per-order would let a 200-garment
	// commercial pickup and a 3-shirt pickup consume identical capacity, and
	// the branch would be oversold by lunchtime.
	garmentsPerSlot = 40

	// Fee schedule. Hard-coded for Phase 1; becomes a rate-card lookup when
	// promotions land in Phase 8.
	deliveryFeeMinor           = 1_500  // 15.00 AED
	freeDeliveryThresholdMinor = 10_000 // 100.00 AED
	standardVATBP              = 500    // 5% — UAE. KSA is 1500.
)

// Register wires the vertical into the engine. Called once from main.
func Register(types *servicetype.Registry, actions *action.Registry) {
	for _, key := range []string{TypeWashFold, TypeDryClean} {
		types.Register(servicetype.Definition{
			Key:         key,
			Selection:   selection{key: key},
			Fulfillment: pattern{key: key},
			Capacity:    capacity{key: key},
			Pricing:     pricing{key: key, finalAtBooking: key == TypeDryClean},
		})
	}

	// The feed's only knowledge of laundry: a CTA kind and a target type.
	actions.Register(action.KindBookService, action.TargetOffering, ctaHandler{})
}

// --- item selection ---------------------------------------------------------

type selection struct{ key string }

func (s selection) Key() string { return s.key }

func (s selection) Validate(_ context.Context, offeringID string, options map[string]any) error {
	raw, ok := options["garments"]
	if !ok {
		return fmt.Errorf("laundry: `garments` is required")
	}

	// JSON numbers arrive as float64 through encoding/json. Converting via int
	// silently truncates 2.7 to 2; rejecting non-integers instead means a
	// client bug surfaces as a 422 rather than as an under-billed order.
	f, ok := raw.(float64)
	if !ok {
		return fmt.Errorf("laundry: `garments` must be a number")
	}
	if f != float64(int(f)) {
		return fmt.Errorf("laundry: `garments` must be a whole number, got %v", f)
	}
	n := int(f)
	if n < 1 || n > 500 {
		return fmt.Errorf("laundry: `garments` must be between 1 and 500, got %d", n)
	}
	return nil
}

// --- fulfillment pattern ----------------------------------------------------

type pattern struct{ key string }

func (p pattern) Key() string { return "pickup_delivery" }

// Plan produces exactly two fulfillments. This is the entire difference between
// laundry and a single-visit service as far as dispatch is concerned.
func (p pattern) Plan(_ context.Context, order fulfillment.OrderContext) ([]fulfillment.Request, error) {
	var pickup, delivery *fulfillment.Slot

	for i := range order.Slots {
		switch order.Slots[i].Kind {
		case fulfillment.KindPickup:
			pickup = &order.Slots[i]
		case fulfillment.KindDelivery:
			delivery = &order.Slots[i]
		}
	}

	if pickup == nil || delivery == nil {
		return nil, fmt.Errorf("laundry: requires both a pickup and a delivery slot")
	}
	if delivery.Date.Before(pickup.Date) {
		return nil, fmt.Errorf("laundry: delivery %s is before pickup %s",
			delivery.Date.Format("2006-01-02"), pickup.Date.Format("2006-01-02"))
	}

	return []fulfillment.Request{
		{
			Kind:          fulfillment.KindPickup,
			Sequence:      1,
			ScheduledDate: pickup.Date,
			WindowID:      pickup.WindowID,
			ResourceKey:   resourceKey,
		},
		{
			Kind:          fulfillment.KindDelivery,
			Sequence:      2,
			ScheduledDate: delivery.Date,
			WindowID:      delivery.WindowID,
			ResourceKey:   resourceKey,
		},
	}, nil
}

// --- capacity ---------------------------------------------------------------

type capacity struct{ key string }

func (c capacity) Key() string         { return c.key }
func (c capacity) ResourceKey() string { return resourceKey }

func (c capacity) Units(_ context.Context, order fulfillment.OrderContext) (int, error) {
	garments := 0
	if raw, ok := order.Options["garments"].(float64); ok {
		garments = int(raw)
	}
	if garments <= 0 {
		return 1, nil
	}
	units := (garments + garmentsPerSlot - 1) / garmentsPerSlot
	if units < 1 {
		units = 1
	}
	return units, nil
}

// --- pricing ----------------------------------------------------------------

type pricing struct {
	key            string
	finalAtBooking bool
}

func (p pricing) Key() string { return p.key }

// FinalAtBooking is false for wash & fold: the true price depends on weight
// measured at the facility. Modelling that here rather than as a downstream
// special case is what lets the quote legitimately move authorized → adjusted
// without any other module knowing laundry exists.
func (p pricing) FinalAtBooking() bool { return p.finalAtBooking }

// headroomBP is the authorization margin for estimate-priced work.
//
// The card is authorized for the estimate plus this margin so the true amount
// can be captured after weighing. Capturing more than was authorized fails at
// the gateway, and re-authorizing later fails because the customer has closed
// the app — so the headroom is taken up front or not at all.
//
// 30% is a guess pending real data. It trades a larger hold on the customer's
// card against failed captures; the right number comes from the observed
// distribution of estimate-to-actual once there are real orders.
const headroomBP = 3_000

func (p pricing) Quote(_ context.Context, in servicetype.PricingContext) (servicetype.QuoteResult, error) {
	if len(in.Lines) == 0 {
		return servicetype.QuoteResult{}, fmt.Errorf("laundry: quote requires at least one line")
	}

	out := servicetype.QuoteResult{
		Currency:     in.Currency,
		TaxInclusive: true,
	}

	for _, line := range in.Lines {
		lineTotal := line.UnitPriceMinor * int64(line.Quantity)
		amount := money.New(lineTotal, in.Currency)

		// Tax is extracted from the line, not added to it, and per line rather
		// than once on the subtotal: VAT treatment differs by item class, and
		// ZATCA e-invoicing requires per-line detail that a single aggregate
		// figure cannot be decomposed back into.
		tax := money.ExtractTax(amount, line.TaxRateBP)

		out.SubtotalMinor += lineTotal
		out.TaxMinor += tax.Minor
		out.Lines = append(out.Lines, servicetype.QuoteLine{
			Kind:        "item",
			Label:       line.Name,
			AmountMinor: lineTotal,
			TaxRateBP:   line.TaxRateBP,
		})
	}

	// Delivery is free above a threshold. Hard-coded here rather than read from
	// a promotions table because promotions are Phase 8 — when that lands, this
	// becomes a lookup and the shape of the function does not change.
	if out.SubtotalMinor < freeDeliveryThresholdMinor {
		out.DeliveryMinor = deliveryFeeMinor
		deliveryTax := money.ExtractTax(money.New(deliveryFeeMinor, in.Currency), standardVATBP)
		out.TaxMinor += deliveryTax.Minor
		out.Lines = append(out.Lines, servicetype.QuoteLine{
			Kind:        "delivery",
			Label:       "Delivery",
			AmountMinor: deliveryFeeMinor,
			TaxRateBP:   standardVATBP,
		})
	}

	out.TotalMinor = out.SubtotalMinor + out.DeliveryMinor - out.DiscountMinor

	// The one place FinalAtBooking changes an amount. Everything downstream
	// reads AuthorizeMinor without knowing why it differs from TotalMinor.
	out.AuthorizeMinor = out.TotalMinor
	if !p.finalAtBooking {
		out.AuthorizeMinor = out.TotalMinor +
			money.Percentage(money.New(out.TotalMinor, in.Currency), headroomBP).Minor
	}

	return out, nil
}

// --- feed action ------------------------------------------------------------

type ctaHandler struct{}

func (ctaHandler) Resolve(_ context.Context, a action.Action, v action.Viewer) (action.Resolved, error) {
	label, labelAr := "Book pickup", "احجز الاستلام"
	if a.Label != "" {
		label, labelAr = a.Label, a.LabelAr
	}
	return action.Resolved{
		Label:     label,
		LabelAr:   labelAr,
		Available: true,
		Route:     fmt.Sprintf("/offerings/%s/book", a.TargetID),
	}, nil
}
