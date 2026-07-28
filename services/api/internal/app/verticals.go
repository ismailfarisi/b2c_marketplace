package app

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/modules/catalog"
	"github.com/marketplace/api/internal/modules/social"
	"github.com/marketplace/api/internal/seam/action"
	"github.com/marketplace/api/internal/seam/servicetype"
	"github.com/marketplace/api/internal/verticals/laundry"
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
	//
	// The eligibility checker is what turns its CTA from "always bookable" into
	// one that tells a customer why they cannot book before they tap.
	laundry.Register(verticals, actions, catalog.NewEligibility(pool))

	return Registries{Actions: actions, Verticals: verticals}
}
