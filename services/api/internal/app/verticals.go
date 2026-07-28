package app

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/modules/catalog"
	"github.com/marketplace/api/internal/modules/social"
	"github.com/marketplace/api/internal/modules/vehicles"
	"github.com/marketplace/api/internal/seam/action"
	"github.com/marketplace/api/internal/seam/servicetype"
	"github.com/marketplace/api/internal/verticals/laundry"
	"github.com/marketplace/api/internal/verticals/meals"
)

// Registries holds the two seams, fully wired.
type Registries struct {
	Actions   *action.Registry
	Verticals *servicetype.Registry
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

	return Registries{Actions: actions, Verticals: verticals}
}
