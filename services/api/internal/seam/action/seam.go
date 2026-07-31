// Package action defines the upstream seam: the boundary between the feed and
// commerce.
//
// It is the exact mirror of the fulfillment seam. Where the driver app never
// learns what a meal is, the feed never learns what a car is. Everything the
// feed knows about commerce is the triple (Kind, TargetType, TargetID).
//
// Adding a vertical means registering an action kind and its handler. It does
// **not** mean touching feed, ranking, or moderation code. If it does, the seam
// has leaked.
package action

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
)

type Kind string

const (
	KindBookService    Kind = "book_service"
	KindSubscribePlan  Kind = "subscribe_plan"
	KindInquireListing Kind = "inquire_listing"
	// KindClaimOffer is a flash offer's CTA. Distinct from booking because there
	// is nothing to schedule and nothing to configure — the whole interaction is
	// "yes, before it closes", and a booking flow in front of that would lose the
	// urgency the format exists to create.
	KindClaimOffer Kind = "claim_offer"
	// KindEnquireProperty is deliberately not KindInquireListing.
	//
	// They look like the same verb and they are not the same act: a car enquiry
	// opens a thread between two private parties, while a property enquiry is a
	// regulated lead handed to a licensed broker and logged against a permit.
	// Sharing the kind would mean one handler doing both, and the compliance
	// obligations of the second leaking into the first.
	KindEnquireProperty Kind = "enquire_property"
	KindOpenMenu        Kind = "open_menu"
	KindFollow          Kind = "follow"
	KindShare           Kind = "share"
	KindExternal        Kind = "external"
)

type TargetType string

const (
	TargetOffering     TargetType = "service_offering"
	TargetPlan         TargetType = "meal_plan"
	TargetListing      TargetType = "vehicle_listing"
	TargetFlashOffer   TargetType = "flash_offer"
	TargetProperty     TargetType = "property_listing"
	TargetVendorBranch TargetType = "vendor_branch"
	TargetVendor       TargetType = "vendor"
	TargetURL          TargetType = "url"
)

// Action is a CTA attached to a reel. Position orders them in the UI.
type Action struct {
	ID         uuid.UUID
	ReelID     uuid.UUID
	Kind       Kind
	TargetType TargetType
	TargetID   uuid.UUID
	// TargetURL is set only when TargetType is TargetURL. It is the one target
	// that cannot be a UUID, and carrying it here rather than in a generic
	// metadata bag keeps handlers from having to type-assert their own input.
	TargetURL string
	Position  int
	Label     string
	LabelAr   string
}

// Resolved is what a handler returns: enough for a client to render the CTA and
// route the tap. The feed renders this without knowing which vertical produced
// it.
type Resolved struct {
	Label     string
	LabelAr   string
	Available bool
	// Reason is shown when Available is false — "outside delivery area",
	// "sold", "fully booked". A greyed-out CTA with no explanation reads as a
	// bug.
	Reason string
	// Route is a client-side deep link, e.g. "/offerings/{id}/book".
	Route string
	Meta  map[string]any
}

// Viewer is the context a handler may consider. Location matters because
// eligibility is not the same as proximity: a vendor who cannot deliver to this
// address is noise regardless of distance.
type Viewer struct {
	UserID    uuid.NullUUID
	AddressID uuid.NullUUID
	Lat, Lng  float64
	Locale    string
	Country   string
}

// Handler owns one (Kind, TargetType) pair. It lives in the vertical's module,
// never here.
type Handler interface {
	Resolve(ctx context.Context, a Action, v Viewer) (Resolved, error)
}

// Labels is a vertical's presentation of a shared action kind.
//
// It exists because Phase 5 found the (Kind, TargetType) key to be one level
// too coarse. `book_service` on a `service_offering` is not laundry's — a meal
// box is also a service offering, and so is a haircut. Phase 3 let laundry
// claim the pair because it was the only vertical; the second one to arrive
// would have panicked at boot on a duplicate registration.
//
// The fix keeps the seam intact rather than widening the key. One handler owns
// the pair — it lives in the module that owns the table, which is catalog — and
// each vertical contributes only the words and the route. A vertical still
// imports nothing but this package.
type Labels struct {
	Label   string
	LabelAr string
	// RoutePrefix is the client-side deep link this CTA opens, with the target
	// id appended: "/offerings" produces "/offerings/{id}/book".
	RoutePrefix string
	// Verb completes the route, e.g. "book" or "order".
	Verb string
}

// Registry binds handlers to (Kind, TargetType). Registration happens once at
// startup from each module's init wiring; lookups are concurrent and read-only
// after that.
type Registry struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

func NewRegistry() *Registry {
	return &Registry{handlers: make(map[string]Handler)}
}

func key(k Kind, t TargetType) string { return string(k) + ":" + string(t) }

// Register panics on a duplicate. Two modules claiming the same action kind is
// a wiring bug that must fail at boot, not resolve nondeterministically by map
// iteration order in production.
func (r *Registry) Register(k Kind, t TargetType, h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.handlers[key(k, t)]; exists {
		panic(fmt.Sprintf("action: duplicate handler for %s", key(k, t)))
	}
	r.handlers[key(k, t)] = h
}

// Has reports whether a handler is registered for this pair.
//
// Used to reject a CTA at authoring time rather than at read time. Without it
// an author can attach an action nobody implemented, it stores cleanly, and the
// failure surfaces weeks later as a greyed-out button in the feed — which
// everyone reads as a feed bug.
func (r *Registry) Has(k Kind, t TargetType) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.handlers[key(k, t)]
	return ok
}

func (r *Registry) Resolve(ctx context.Context, a Action, v Viewer) (Resolved, error) {
	r.mu.RLock()
	h, ok := r.handlers[key(a.Kind, a.TargetType)]
	r.mu.RUnlock()
	if !ok {
		return Resolved{}, fmt.Errorf("action: no handler for %s", key(a.Kind, a.TargetType))
	}
	return h.Resolve(ctx, a, v)
}

// ResolveBatch resolves every action on a page of reels.
//
// Batching is not an optimisation here, it is a requirement: a feed page of 10
// reels with 2 actions each is 20 resolutions, and doing them one at a time
// puts 20 sequential round-trips on the hottest read path in the product.
// Handlers should implement BatchHandler once a vertical has real volume.
func (r *Registry) ResolveBatch(ctx context.Context, actions []Action, v Viewer) ([]Resolved, error) {
	out := make([]Resolved, len(actions))
	for i, a := range actions {
		res, err := r.Resolve(ctx, a, v)
		if err != nil {
			// One unresolvable action must not blank the whole feed page.
			out[i] = Resolved{Available: false, Reason: "unavailable"}
			continue
		}
		out[i] = res
	}
	return out, nil
}
