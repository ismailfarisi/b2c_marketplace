package meals

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/marketplace/api/internal/seam/fulfillment"
	"github.com/marketplace/api/internal/seam/servicetype"
	"github.com/marketplace/api/internal/verticals/laundry"
)

func TestRegisteringMealsCostsNoEngineChanges(t *testing.T) {
	// PLAN.md §5 P2's gate, executed rather than asserted: "if adding meals
	// required changing commerce or scheduling internals rather than adding
	// strategies, stop and fix the abstraction."
	//
	// Both verticals register into one registry, and both are fully bound. The
	// registry panics on a partially-wired definition, so reaching the end of
	// this function is the evidence.
	registry := servicetype.NewRegistry()
	laundry.Register(registry)
	Register(registry)

	for _, key := range []string{TypeSubscription, TypeOneOff} {
		def, err := registry.Get(key)
		if err != nil {
			t.Fatalf("%s did not register: %v", key, err)
		}
		if def.Selection == nil || def.Fulfillment == nil ||
			def.Capacity == nil || def.Pricing == nil {
			t.Fatalf("%s is partially wired", key)
		}
	}
}

func TestMealsAndLaundryDoNotContendForCapacity(t *testing.T) {
	// Two verticals at one branch either share a capacity pool or they do not,
	// and migration 000005 makes that a data decision. These genuinely do not: a
	// kitchen runs out of covers, a laundry runs out of van slots, and pooling
	// them would let a busy lunch service block tomorrow's pickups.
	registry := servicetype.NewRegistry()
	laundry.Register(registry)
	Register(registry)

	meal, _ := registry.Get(TypeOneOff)
	wash, _ := registry.Get(laundry.TypeWashFold)

	if meal.Capacity.ResourceKey() == wash.Capacity.ResourceKey() {
		t.Fatalf("both verticals draw on %q", meal.Capacity.ResourceKey())
	}
}

func TestAPlanCannotBeOrdered(t *testing.T) {
	// A subscription's physical work is materialised by the rolling scheduler,
	// not decided at order time. Returning zero requests instead of an error
	// would let POST /v1/orders against a meal plan succeed quietly — producing
	// a paid order that delivers nothing, and a customer who finds out at
	// lunchtime.
	_, err := subscriptionPattern{}.Plan(context.Background(), fulfillment.OrderContext{
		Slots: []fulfillment.Slot{{Kind: fulfillment.KindDelivery, Date: time.Now()}},
	})
	if err == nil {
		t.Fatal("ordering a meal plan through the one-off path was accepted")
	}
	if !strings.Contains(err.Error(), "subscriptions") {
		t.Errorf("the error does not tell the caller what to do instead: %v", err)
	}
}

func TestAMealBoxProducesExactlyOneFulfillment(t *testing.T) {
	// The entire difference between a meal box and a laundry order as far as
	// dispatch is concerned.
	got, err := singleDelivery{}.Plan(context.Background(), fulfillment.OrderContext{
		Slots: []fulfillment.Slot{
			{Kind: fulfillment.KindDelivery, Date: time.Now(), WindowID: uuid.New()},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d fulfillments, want 1", len(got))
	}
	if got[0].Kind != fulfillment.KindDelivery {
		t.Errorf("kind = %s, want delivery", got[0].Kind)
	}
	if got[0].ResourceKey != resourceKey {
		t.Errorf("resource = %q, want %q", got[0].ResourceKey, resourceKey)
	}
}

func TestABoxWithNoDeliverySlotIsRefused(t *testing.T) {
	_, err := singleDelivery{}.Plan(context.Background(), fulfillment.OrderContext{})
	if err == nil {
		t.Fatal("a meal box with no delivery slot was accepted")
	}
}

func TestCapacityCountsPortionsNotOrders(t *testing.T) {
	// A kitchen's constraint is how many boxes it can plate. Counting orders
	// would let one office order for forty consume the same capacity as one
	// person's lunch, and the kitchen would be oversold by mid-morning on
	// exactly the days that matter.
	c := covers{key: TypeOneOff}

	single, err := c.Units(context.Background(), fulfillment.OrderContext{})
	if err != nil {
		t.Fatal(err)
	}
	if single != 1 {
		t.Fatalf("a box with no portions option drew %d covers, want 1", single)
	}

	office, err := c.Units(context.Background(), fulfillment.OrderContext{
		Options: map[string]any{"portions": float64(40)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if office != 40 {
		t.Fatalf("a forty-portion order drew %d covers, want 40", office)
	}
}

func TestPortionsMustBeAWholeNumber(t *testing.T) {
	// JSON numbers arrive as float64. Truncating 2.7 to 2 silently under-bills;
	// rejecting it surfaces the client bug as a 422.
	sel := oneOffSelection{}

	if err := sel.Validate(context.Background(), "", map[string]any{"portions": 2.7}); err == nil {
		t.Error("a fractional portion count was accepted")
	}
	if err := sel.Validate(context.Background(), "", map[string]any{"portions": float64(3)}); err != nil {
		t.Errorf("a whole portion count was rejected: %v", err)
	}
	if err := sel.Validate(context.Background(), "", map[string]any{"portions": float64(0)}); err == nil {
		t.Error("zero portions was accepted")
	}
	if err := sel.Validate(context.Background(), "", map[string]any{"portions": "many"}); err == nil {
		t.Error("a non-numeric portion count was accepted")
	}
}

func TestOmittingPortionsIsTheCommonCase(t *testing.T) {
	// One portion is overwhelmingly the normal order. Requiring the field would
	// fail the simplest request anyone makes.
	var sel oneOffSelection
	if err := sel.Validate(context.Background(), "", nil); err != nil {
		t.Fatalf("an order with no options was rejected: %v", err)
	}
}

// --- pricing ------------------------------------------------------------------

func planContext(priceMinor int64) servicetype.PricingContext {
	return servicetype.PricingContext{
		Currency: "AED",
		Lines: []servicetype.PricingLine{{
			Name: "5-day lunch plan", UnitPriceMinor: priceMinor,
			TaxRateBP: standardVATBP, Quantity: 1,
		}},
	}
}

func TestAPlanCarriesNoDeliveryFee(t *testing.T) {
	// The price on the card is the price of the plan. A per-delivery fee on a
	// twenty-delivery cycle reads as a hidden charge even when it is disclosed,
	// and it makes the headline number in the feed a lie.
	q, err := planPricing{}.Quote(context.Background(), planContext(44_900))
	if err != nil {
		t.Fatal(err)
	}
	if q.DeliveryMinor != 0 {
		t.Fatalf("a plan quote carried %d in delivery fees", q.DeliveryMinor)
	}
	if q.TotalMinor != 44_900 {
		t.Fatalf("total = %d, want 44900", q.TotalMinor)
	}
}

func TestPlanVATIsExtractedNotAdded(t *testing.T) {
	// VAT is displayed tax-inclusive in every GCC market. Getting this backwards
	// misprices everything by the VAT rate, and it is the single most expensive
	// arithmetic mistake available in this codebase.
	q, err := planPricing{}.Quote(context.Background(), planContext(44_900))
	if err != nil {
		t.Fatal(err)
	}
	if !q.TaxInclusive {
		t.Fatal("the quote is not marked tax-inclusive")
	}
	if q.TaxMinor >= q.TotalMinor {
		t.Fatalf("tax %d is not a portion of the total %d", q.TaxMinor, q.TotalMinor)
	}
	// 5% of 449.00 inclusive is 449.00 × 5/105 = 21.38.
	if q.TaxMinor != 2_138 {
		t.Fatalf("VAT = %d, want 2138 extracted from an inclusive total", q.TaxMinor)
	}
}

func TestAPlanCapturesImmediately(t *testing.T) {
	// Unlike laundry by weight, nothing is measured after the fact — so there is
	// no headroom, and the payment flow must not hold an inflated amount on a
	// customer's card for weeks.
	q, err := planPricing{}.Quote(context.Background(), planContext(44_900))
	if err != nil {
		t.Fatal(err)
	}
	if q.AuthorizeMinor != q.TotalMinor {
		t.Fatalf("authorize %d differs from total %d on a final-at-booking plan",
			q.AuthorizeMinor, q.TotalMinor)
	}
	var p planPricing
	if !p.FinalAtBooking() {
		t.Fatal("a plan reports its price as provisional")
	}
}

func TestABoxBelowTheMinimumIsRefused(t *testing.T) {
	// Enforced in pricing rather than in the client, so it cannot be bypassed by
	// calling the API directly.
	_, err := boxPricing{}.Quote(context.Background(), servicetype.PricingContext{
		Currency: "AED",
		Lines: []servicetype.PricingLine{{
			Name: "Side salad", UnitPriceMinor: 1_500,
			TaxRateBP: standardVATBP, Quantity: 1,
		}},
	})
	if err == nil {
		t.Fatal("an order below the minimum was priced")
	}
}

func TestABoxCarriesADeliveryFee(t *testing.T) {
	q, err := boxPricing{}.Quote(context.Background(), servicetype.PricingContext{
		Currency: "AED",
		Lines: []servicetype.PricingLine{{
			Name: "Chicken biryani", UnitPriceMinor: 3_500,
			TaxRateBP: standardVATBP, Quantity: 1,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if q.DeliveryMinor != deliveryFeeMinor {
		t.Fatalf("delivery = %d, want %d", q.DeliveryMinor, deliveryFeeMinor)
	}
	if q.TotalMinor != 3_500+deliveryFeeMinor {
		t.Fatalf("total = %d, want %d", q.TotalMinor, 3_500+deliveryFeeMinor)
	}
}

func TestOnlyTheOneOffTypeHasAFeedCTA(t *testing.T) {
	// A plan is subscribed to, not ordered: its CTA is a different action kind
	// on a different target, owned by catalog.PlanHandler. Contributing a
	// book_service label for it would produce a button that routes into a
	// booking flow the plan cannot complete.
	labels := CTALabels()
	if _, ok := labels[TypeSubscription]; ok {
		t.Fatal("the subscription type contributed a book_service label")
	}
	if _, ok := labels[TypeOneOff]; !ok {
		t.Fatal("the one-off type contributed no label")
	}
}

func TestVerticalLabelsDoNotCollide(t *testing.T) {
	// The Phase 5 finding, guarded. Both verticals contribute labels for the
	// shared (book_service, service_offering) pair, keyed by service type. Two
	// verticals claiming the same service-type key would silently shadow one
	// another at merge time in app.BuildRegistries.
	seen := map[string]string{}
	for key := range laundry.CTALabels() {
		seen[key] = "laundry"
	}
	for key := range CTALabels() {
		if owner, taken := seen[key]; taken {
			t.Fatalf("service type %q is claimed by both %s and meals", key, owner)
		}
	}
}
