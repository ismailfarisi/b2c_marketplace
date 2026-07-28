// Package servicetype is the strategy registry that makes one engine serve many
// verticals.
//
// A `service_types` row binds four strategies. Adding a vertical is that row
// plus implementations of these four interfaces — it is never a change to
// ordering, pricing, capacity or logistics code. That constraint is the whole
// architecture; when it stops holding, the engine has become N special cases
// wearing a trenchcoat.
//
//	item selection  → what the customer picks, and how it validates
//	fulfillment     → how many physical events, of what kinds (see seam/fulfillment)
//	capacity        → which pool the booking draws down
//	pricing         → how a quote is computed, and whether it can be adjusted later
package servicetype

import (
	"context"
	"fmt"
	"sync"

	"github.com/marketplace/api/internal/seam/fulfillment"
)

// ItemSelection validates the vertical-specific `options` blob on an order
// line. Laundry validates garment counts; meals validate plan and portion
// choices; a salon validates a stylist.
type ItemSelection interface {
	Key() string
	Validate(ctx context.Context, offeringID string, options map[string]any) error
}

// CapacityResource names the pool a booking consumes and how much of it.
//
// Returning a per-order unit count rather than a flat 1 matters: a 40-garment
// laundry pickup and a 3-garment pickup are not the same load on a van.
type CapacityResource interface {
	Key() string
	ResourceKey() string
	Units(ctx context.Context, order fulfillment.OrderContext) (int, error)
}

// Pricing computes a quote.
//
// FinalAtBooking is the field that keeps laundry from being an exception: when
// false, the quote legitimately moves `authorized → adjusted` after handling,
// and the payment flow must authorize a headroom amount rather than capture
// immediately. Modelling that here means no downstream code branches on
// "is this laundry".
type Pricing interface {
	Key() string
	FinalAtBooking() bool
	Quote(ctx context.Context, order fulfillment.OrderContext) (QuoteResult, error)
}

type QuoteResult struct {
	Currency      string
	SubtotalMinor int64
	DeliveryMinor int64
	DiscountMinor int64
	TaxMinor      int64
	TotalMinor    int64
	TaxInclusive  bool
	Lines         []QuoteLine
}

type QuoteLine struct {
	Kind        string // item | delivery | surcharge | discount | tax
	Label       string
	AmountMinor int64
	TaxRateBP   int // basis points: 500 = 5% UAE, 1500 = 15% KSA
}

// Definition is one fully-bound vertical — the code half of a `service_types`
// row.
type Definition struct {
	Key         string
	Selection   ItemSelection
	Fulfillment fulfillment.Pattern
	Capacity    CapacityResource
	Pricing     Pricing
}

func (d Definition) validate() error {
	switch {
	case d.Key == "":
		return fmt.Errorf("servicetype: empty key")
	case d.Selection == nil:
		return fmt.Errorf("servicetype %q: nil item selection", d.Key)
	case d.Fulfillment == nil:
		return fmt.Errorf("servicetype %q: nil fulfillment pattern", d.Key)
	case d.Capacity == nil:
		return fmt.Errorf("servicetype %q: nil capacity resource", d.Key)
	case d.Pricing == nil:
		return fmt.Errorf("servicetype %q: nil pricing", d.Key)
	}
	return nil
}

type Registry struct {
	mu    sync.RWMutex
	types map[string]Definition
}

func NewRegistry() *Registry {
	return &Registry{types: make(map[string]Definition)}
}

// Register binds a vertical at startup. A partially-wired definition panics
// here rather than nil-pointering on the first order of that type in
// production.
func (r *Registry) Register(d Definition) {
	if err := d.validate(); err != nil {
		panic(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.types[d.Key]; exists {
		panic(fmt.Sprintf("servicetype: duplicate registration for %q", d.Key))
	}
	r.types[d.Key] = d
}

func (r *Registry) Get(key string) (Definition, error) {
	r.mu.RLock()
	d, ok := r.types[key]
	r.mu.RUnlock()
	if !ok {
		return Definition{}, fmt.Errorf("servicetype: %q is not registered", key)
	}
	return d, nil
}

func (r *Registry) Keys() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]string, 0, len(r.types))
	for k := range r.types {
		keys = append(keys, k)
	}
	return keys
}
