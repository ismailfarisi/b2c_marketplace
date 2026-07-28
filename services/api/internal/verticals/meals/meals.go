// Package meals implements the launch vertical.
//
// PLAN-VIDEO-FIRST §5 reverses the earlier ordering and puts food first: it is
// the most watchable category on every platform, vendor motivation is good with
// enablement, and it is the only vertical whose content problem is solvable at
// cold start. Laundry stays as the engineering skeleton it always was.
//
// Like laundry, this package contains strategies and no other logic, and imports
// nothing but the seams and money. Everything recurrence-shaped —
// materialisation, billing cycles, pauses, dunning — lives in
// internal/modules/subscriptions, because none of it is about food. That split
// is the main finding of Phase 5; see docs/PHASE-5-FOOD-AND-SUBSCRIPTIONS.md §2.
package meals

import (
	"context"
	"fmt"

	"github.com/marketplace/api/internal/money"
	"github.com/marketplace/api/internal/seam/action"
	"github.com/marketplace/api/internal/seam/fulfillment"
	"github.com/marketplace/api/internal/seam/servicetype"
)

const (
	// A recurring meal plan. Physical work is materialised by the rolling
	// scheduler, not at order time.
	TypeSubscription = "meals_subscription"
	// A single meal box for a chosen day. Fits the Phase 1 engine exactly as it
	// stands — this is the half of the vertical that passed the architecture's
	// exam with zero engine changes.
	TypeOneOff = "meals_one_off"

	// What a kitchen runs out of. Not "delivery slots": a kitchen with four
	// drivers and one chef is limited by the chef, and portions are the unit
	// that actually gets exhausted.
	//
	// Deliberately distinct from laundry's `pickup_slot`. A branch that somehow
	// did both would contend on neither — two verticals sharing a pool or not is
	// a data decision (migration 000005), and these two genuinely do not share.
	resourceKey = "kitchen_covers"

	// One-off boxes carry a delivery fee; plans do not. That is how meal
	// subscriptions are sold in this market — the price on the card is the price
	// of the plan, and a per-delivery fee on a 20-delivery cycle reads as a
	// hidden charge even when it is disclosed.
	deliveryFeeMinor = 1_200 // 12.00 AED
	standardVATBP    = 500   // 5% — UAE. KSA is 1500.

	// Below this, a single box costs more to deliver than it earns. Enforced in
	// pricing rather than in the client so the number cannot be bypassed by
	// calling the API directly.
	minimumOrderMinor = 2_500 // 25.00 AED
)

// Register wires both meal service types into the engine.
func Register(types *servicetype.Registry) {
	types.Register(servicetype.Definition{
		Key:         TypeSubscription,
		Selection:   subscriptionSelection{},
		Fulfillment: subscriptionPattern{},
		Capacity:    covers{key: TypeSubscription},
		Pricing:     planPricing{},
	})

	types.Register(servicetype.Definition{
		Key:         TypeOneOff,
		Selection:   oneOffSelection{},
		Fulfillment: singleDelivery{},
		Capacity:    covers{key: TypeOneOff},
		Pricing:     boxPricing{},
	})
}

// CTALabels is the feed's knowledge of meals: words and a route.
//
// Only the one-off type appears here. A plan is subscribed to rather than
// ordered, and its CTA is a different action kind on a different target — see
// PlanHandler below.
func CTALabels() map[string]action.Labels {
	return map[string]action.Labels{
		TypeOneOff: {
			Label: "Order", LabelAr: "اطلب",
			RoutePrefix: "/offerings", Verb: "order",
		},
	}
}

// --- item selection ---------------------------------------------------------

type subscriptionSelection struct{}

func (subscriptionSelection) Key() string { return TypeSubscription }

// Validate is a no-op for the subscription type, and that is not laziness.
//
// A subscription's selections are per-delivery-day dish choices, made after
// signup and changed weekly. They are validated by the subscriptions module
// against the menu cycle for that date — a validator here would have no date to
// validate against, since the "order" that carries a subscription's money is a
// billing row with no meals on it.
func (subscriptionSelection) Validate(context.Context, string, map[string]any) error {
	return nil
}

type oneOffSelection struct{}

func (oneOffSelection) Key() string { return TypeOneOff }

// Validate checks the portion count on a one-off box.
func (oneOffSelection) Validate(_ context.Context, _ string, options map[string]any) error {
	raw, ok := options["portions"]
	if !ok {
		// One portion is the overwhelmingly common case and requiring the field
		// would fail the simplest possible order.
		return nil
	}

	// JSON numbers arrive as float64. Converting via int silently truncates 2.7
	// to 2; rejecting non-integers means a client bug surfaces as a 422 rather
	// than as an under-billed order.
	f, ok := raw.(float64)
	if !ok {
		return fmt.Errorf("meals: `portions` must be a number")
	}
	if f != float64(int(f)) {
		return fmt.Errorf("meals: `portions` must be a whole number, got %v", f)
	}
	if n := int(f); n < 1 || n > 20 {
		return fmt.Errorf("meals: `portions` must be between 1 and 20, got %d", n)
	}
	return nil
}

// --- fulfillment patterns ---------------------------------------------------

type subscriptionPattern struct{}

func (subscriptionPattern) Key() string { return "recurring_delivery" }

// Plan refuses, deliberately and with a message a client can act on.
//
// A subscription's physical work is materialised by the rolling scheduler over
// a fourteen-day window (PLAN.md §4), not decided at order time — the order
// that carries a cycle's money has no fulfillments at all. Returning zero
// requests instead would let `POST /v1/orders` against a meal plan succeed
// quietly, producing a paid order that delivers nothing and a customer who
// finds out at lunchtime.
//
// So the strategy is registered and it says no. That is the seam being used
// correctly: the engine asks the vertical what physical work an order becomes,
// and for this type the honest answer is "that is not how this is bought."
func (subscriptionPattern) Plan(context.Context, fulfillment.OrderContext) ([]fulfillment.Request, error) {
	return nil, fmt.Errorf(
		"meals: a plan is subscribed to, not ordered — use POST /v1/subscriptions")
}

type singleDelivery struct{}

func (singleDelivery) Key() string { return "single_delivery" }

// Plan produces exactly one fulfillment. The entire difference between a meal
// box and a laundry order, as far as dispatch is concerned.
func (singleDelivery) Plan(_ context.Context, order fulfillment.OrderContext) ([]fulfillment.Request, error) {
	for _, s := range order.Slots {
		if s.Kind != fulfillment.KindDelivery {
			continue
		}
		return []fulfillment.Request{{
			Kind:          fulfillment.KindDelivery,
			Sequence:      1,
			ScheduledDate: s.Date,
			WindowID:      s.WindowID,
			ResourceKey:   resourceKey,
		}}, nil
	}
	return nil, fmt.Errorf("meals: a delivery slot is required")
}

// --- capacity ---------------------------------------------------------------

type covers struct{ key string }

func (c covers) Key() string         { return c.key }
func (c covers) ResourceKey() string { return resourceKey }

// Units returns portions, not orders.
//
// A kitchen's constraint is how many boxes it can plate, so an eight-portion
// family order draws eight covers and a single box draws one. Counting orders
// instead would let one office order for forty consume the same capacity as one
// person's lunch, and the kitchen would be oversold by mid-morning on exactly
// the days that matter.
func (c covers) Units(_ context.Context, order fulfillment.OrderContext) (int, error) {
	portions := 1
	if raw, ok := order.Options["portions"].(float64); ok && raw >= 1 {
		portions = int(raw)
	}
	return portions, nil
}

// --- pricing ----------------------------------------------------------------

type planPricing struct{}

func (planPricing) Key() string { return TypeSubscription }

// FinalAtBooking is true. A plan's price is known when the customer signs up —
// unlike laundry by weight, nothing is measured afterwards. The whole cycle is
// charged up front (PLAN.md §6) and recognised daily as meals are delivered.
func (planPricing) FinalAtBooking() bool { return true }

// Quote prices one billing cycle.
//
// No delivery line. A plan's price is the price: adding a per-delivery fee to a
// twenty-delivery cycle reads as a hidden charge even when it is disclosed, and
// it makes the headline number in the feed a lie.
func (planPricing) Quote(_ context.Context, in servicetype.PricingContext) (servicetype.QuoteResult, error) {
	if len(in.Lines) == 0 {
		return servicetype.QuoteResult{}, fmt.Errorf("meals: a plan quote requires a line")
	}

	out := servicetype.QuoteResult{Currency: in.Currency, TaxInclusive: true}

	for _, line := range in.Lines {
		lineTotal := line.UnitPriceMinor * int64(line.Quantity)
		// Tax is extracted from the line, not added to it, and per line rather
		// than once on the subtotal: ZATCA e-invoicing requires per-line detail
		// that a single aggregate figure cannot be decomposed back into.
		tax := money.ExtractTax(money.New(lineTotal, in.Currency), line.TaxRateBP)

		out.SubtotalMinor += lineTotal
		out.TaxMinor += tax.Minor
		out.Lines = append(out.Lines, servicetype.QuoteLine{
			Kind:        "item",
			Label:       line.Name,
			AmountMinor: lineTotal,
			TaxRateBP:   line.TaxRateBP,
		})
	}

	out.TotalMinor = out.SubtotalMinor
	// No headroom: the price is final, so authorize equals total and the payment
	// flow captures immediately rather than holding an inflated amount.
	out.AuthorizeMinor = out.TotalMinor
	return out, nil
}

type boxPricing struct{}

func (boxPricing) Key() string          { return TypeOneOff }
func (boxPricing) FinalAtBooking() bool { return true }

func (boxPricing) Quote(_ context.Context, in servicetype.PricingContext) (servicetype.QuoteResult, error) {
	if len(in.Lines) == 0 {
		return servicetype.QuoteResult{}, fmt.Errorf("meals: quote requires at least one line")
	}

	out := servicetype.QuoteResult{Currency: in.Currency, TaxInclusive: true}

	for _, line := range in.Lines {
		lineTotal := line.UnitPriceMinor * int64(line.Quantity)
		tax := money.ExtractTax(money.New(lineTotal, in.Currency), line.TaxRateBP)

		out.SubtotalMinor += lineTotal
		out.TaxMinor += tax.Minor
		out.Lines = append(out.Lines, servicetype.QuoteLine{
			Kind:        "item",
			Label:       line.Name,
			AmountMinor: lineTotal,
			TaxRateBP:   line.TaxRateBP,
		})
	}

	// Enforced here rather than in the client, so it cannot be bypassed by
	// calling the API directly. Below this a box costs more to deliver than it
	// earns, and the vendor absorbs the difference on every one.
	if out.SubtotalMinor < minimumOrderMinor {
		return servicetype.QuoteResult{}, fmt.Errorf(
			"meals: minimum order is %s", money.New(minimumOrderMinor, in.Currency).Major())
	}

	out.DeliveryMinor = deliveryFeeMinor
	deliveryTax := money.ExtractTax(money.New(deliveryFeeMinor, in.Currency), standardVATBP)
	out.TaxMinor += deliveryTax.Minor
	out.Lines = append(out.Lines, servicetype.QuoteLine{
		Kind:        "delivery",
		Label:       "Delivery",
		AmountMinor: deliveryFeeMinor,
		TaxRateBP:   standardVATBP,
	})

	out.TotalMinor = out.SubtotalMinor + out.DeliveryMinor - out.DiscountMinor
	out.AuthorizeMinor = out.TotalMinor
	return out, nil
}
