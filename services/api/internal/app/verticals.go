package app

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/modules/catalog"
	"github.com/marketplace/api/internal/modules/offers"
	"github.com/marketplace/api/internal/modules/property"
	"github.com/marketplace/api/internal/modules/social"
	"github.com/marketplace/api/internal/modules/vehicles"
	"github.com/marketplace/api/internal/seam/action"
	"github.com/marketplace/api/internal/seam/servicetype"
	"github.com/marketplace/api/internal/seam/subject"
	"github.com/marketplace/api/internal/verticals/laundry"
	"github.com/marketplace/api/internal/verticals/meals"
)

// Registries holds the seams, fully wired.
type Registries struct {
	Actions   *action.Registry
	Verticals *servicetype.Registry
	// Subjects is the upstream seam's other half: what a reel is about, as the
	// overlay is allowed to know it. Separate registry from Actions because the
	// two answer different questions about the same reel — "what can I do here"
	// and "when does this stop being true" — and a vertical can reasonably
	// implement one without the other. A car has a CTA and a freshness signal; a
	// follow button has a CTA and no time dimension at all.
	Subjects *subject.Registry
}

// BuildRegistries registers every vertical and every platform-level action.
//
// One function, called identically by all three binaries. The alternative —
// each main doing its own registration — drifts the moment one of them gains a
// handler the others do not, and the symptom is a CTA that renders in cmd/api's
// preview and resolves to "unavailable" in cmd/feed, which is the surface
// customers actually see.
//
// Both registries panic on a duplicate or a partially-wired definition, so a
// wiring mistake fails at boot rather than on the first order of that type in
// production.
func BuildRegistries(pool *pgxpool.Pool) Registries {
	actions := action.NewRegistry()
	verticals := servicetype.NewRegistry()
	subjects := subject.NewRegistry()

	// Platform-level kinds first — follow, share, open_menu, external. They
	// belong to no vertical: deleting laundry must not delete the follow button.
	social.Register(actions, pool)

	// Laundry is the engineering skeleton, not a launch decision.
	// PLAN-VIDEO-FIRST §5 ranks it last for feed suitability and no phase in §8
	// ships it to customers — docs/README.md open decision #3, still open.
	laundry.Register(verticals)

	// Meals is the launch vertical (PLAN-VIDEO-FIRST §5). Two service types: a
	// recurring plan and a single box. Registering them costs exactly this line,
	// which is the result PLAN.md §5 P2's gate was written to check.
	meals.Register(verticals)

	// Shared CTAs, owned by the module that owns the table.
	//
	// This is where Phase 5 found the (kind, target_type) key to be one level
	// too coarse. Phase 3 let laundry register (book_service, service_offering)
	// because it was the only vertical; a meal box is also a service offering,
	// so meals registering the same pair would have panicked at boot. The
	// registry was right and the ownership was wrong — so the pair is now
	// claimed once by catalog and each vertical contributes only its words.
	labels := map[string]action.Labels{}
	for _, contributed := range []map[string]action.Labels{
		laundry.CTALabels(),
		meals.CTALabels(),
	} {
		for serviceType, l := range contributed {
			labels[serviceType] = l
		}
	}

	eligibility := catalog.NewEligibility(pool)
	actions.Register(action.KindBookService, action.TargetOffering,
		catalog.NewBookingHandler(pool, eligibility, labels))
	// A plan is subscribed to, not ordered — a different kind on a different
	// target, and the highest-value conversion in the product.
	actions.Register(action.KindSubscribePlan, action.TargetPlan,
		catalog.NewPlanHandler(pool))

	// Vehicles (Phase 6). This is the whole of the classifieds engine's contact
	// with the feed.
	//
	// Note what is *absent*: no verticals.Register. A used car binds none of the
	// four service-type strategies — nothing is fulfilled, nothing is
	// scheduled, no capacity is drawn, and the price is agreed at an RTA
	// counter the platform never sees. Registering it as a service type would
	// have meant four stub strategies that exist to satisfy a registry, and the
	// first person to read them would reasonably conclude a car can be ordered.
	//
	// The handler lives in `vehicles` rather than in `catalog` because
	// ownership follows the table, which is the rule Phase 5 arrived at:
	// `service_offering` and `meal_plan` are catalogue rows several verticals
	// write into, so catalog owns those pairs; `vehicle_listing` has exactly one
	// owner and always will.
	actions.Register(action.KindInquireListing, action.TargetListing,
		vehicles.NewInquiryHandler(pool))

	// The subject seam, same ownership rule: the module that owns the table owns
	// the provider. Vehicles is the only vertical registered here today, and
	// that is a statement about what has been built rather than about the
	// design — a meal plan has a daily cutoff and a restaurant offer has a
	// window, and both are one line each when their providers land.
	//
	// A subject type with no provider is legal and common. Its reels play, carry
	// their CTAs, and render no badge and no countdown.
	subjects.Register(subject.TypeListing, vehicles.NewSubjectProvider(pool))
	// And the publish gate: a reel about a car waits for the listing desk.
	//
	// Registered here rather than beside the composers because the *worker*
	// runs the publish job and only cmd/api calls RegisterComposers. A gate
	// registered in the wrong place is a gate that silently never applies,
	// which is precisely the bug it exists to fix.
	subjects.RegisterGate(subject.TypeListing, vehicles.NewPublishGate(pool))

	// Meal plans — the first vertical to arrive *after* the subject seam was
	// finished, and therefore the test of what the seam claimed.
	//
	// This line and one Go file are the whole of it. Both clients gain a "Meals"
	// filter chip, a MEAL SUBSCRIPTION badge and a recurring daily cutoff — the
	// fourth and last time mode, and the first one in production — without a
	// line changing in the feed, the ranker, the overlay or either app. If that
	// had required a mobile release, the seam would have failed.
	//
	// Ownership follows the table, so it lives in catalog beside the plan CTA
	// rather than in verticals/meals: `subscription_plans` is a catalogue row
	// several verticals write into.
	subjects.Register(subject.TypePlan, catalog.NewPlanSubjectProvider(pool))

	// Flash offers — a vendor's time-boxed sale, and the first user of
	// ModeWindow. The seam has carried that mode unexercised since it was
	// written; this is the vertical it was written for.
	actions.Register(action.KindClaimOffer, action.TargetFlashOffer,
		offers.NewClaimHandler(pool))
	subjects.Register(subject.TypeOffer, offers.NewSubjectProvider(pool))

	// Property.
	//
	// A separate kind from a car enquiry rather than a reuse of
	// inquire_listing, because the two produce different things: one opens a
	// thread between private parties, the other creates a regulated lead against
	// a permit. See property.EnquiryHandler.
	actions.Register(action.KindEnquireProperty, action.TargetProperty,
		property.NewEnquiryHandler(pool))
	subjects.Register(subject.TypeProperty, property.NewSubjectProvider(pool))
	// The gate that matters most: an unpermitted property advert must not
	// reach the feed, and a reel of a specific flat with its price on it is an
	// advertisement whatever else it is.
	subjects.RegisterGate(subject.TypeProperty, property.NewPublishGate(pool))

	return Registries{Actions: actions, Verticals: verticals, Subjects: subjects}
}

// RegisterComposers attaches the sell flows.
//
// Separate from BuildRegistries and called only by cmd/api, because a composer
// needs a *Service — quota, threads and fraud signals wired — where a provider
// needs only a pool. cmd/feed and cmd/worker build no such service and have no
// business accepting a listing anyway: the feed process serves reads, and a
// write path mounted there would be a second, unaudited way to create supply.
//
// This is where the "adding a vertical is a Go file and a registration line"
// claim is either true or it is not, for the sell half. Today it is one line.
func RegisterComposers(r Registries, vehicleSvc *vehicles.Service, propertySvc *property.Service) {
	r.Subjects.RegisterComposer(subject.TypeListing, vehicles.NewComposer(vehicleSvc))

	// Property is sellable from the app, and the flow is deliberately different
	// in one place: its trust upload is a gate rather than a badge. See
	// property.Composer — the permit arrives days after the video does.
	r.Subjects.RegisterComposer(subject.TypeProperty, property.NewComposer(propertySvc))

	// Flash offers are absent, and that is the seam working rather than a gap.
	// An offer is authored by a restaurant through a vendor console against its
	// own branch — there is no consumer standing outside a kitchen filming a
	// discount they are entitled to publish. Being visible in the feed and being
	// sellable from a phone are separate capabilities, which is exactly why
	// Composer is a separate optional interface from Provider.
}
