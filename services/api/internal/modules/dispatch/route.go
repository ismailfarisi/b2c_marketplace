// Package dispatch turns a day's fulfillments into driver routes.
//
// PLAN.md §1 made a claim about the Fulfillment primitive and repeated it above
// `CREATE TABLE fulfillments`:
//
//	Dispatch, routing and the driver app read this table and nothing else.
//	None of them ever learns what a laundry bag or a meal box is.
//
// This package is where that is either true or it is not. Read the imports: no
// vertical, no ordering, no subscriptions, no catalog. The Stop type below is a
// strict subset of the fulfillment seam, and `internal/app/seam_test.go` fails
// the build if this package ever imports something that knows what is in the
// bag.
//
// The one place it looked like vertical knowledge would be needed is
// precedence — laundry must be collected before it is returned. It turned out
// not to be: the seam already carries `(parent, sequence)`, so dispatch learns
// that sequence 1 precedes sequence 2 within a parent and never learns why.
package dispatch

import (
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
)

// Point is a coordinate. Nothing here uses PostGIS: routing runs in Go over a
// few dozen stops and a round trip to the database per distance calculation
// would dominate the cost of the algorithm several times over.
type Point struct {
	Lat, Lng float64
}

// Stop is one fulfillment, as routing sees it.
//
// Every field is on the fulfillment seam. There is deliberately no `Items`, no
// `Value`, no `ServiceType` and no `Weight` — the first three because routing
// does not need them, and `Weight` because a bike rider's real constraint is
// stop count and volume, not mass, and inventing a field the callers would have
// to populate with a guess is worse than not having it.
type Stop struct {
	FulfillmentID uuid.UUID
	// ParentID is the order or subscription the fulfillment belongs to.
	// Precedence is computed within a parent and nowhere else.
	ParentID uuid.UUID
	Sequence int
	Kind     string
	At       Point

	// WindowStart and WindowEnd come from the fulfillment's time window,
	// resolved to absolute times in the branch's zone by the caller.
	WindowStart time.Time
	WindowEnd   time.Time

	// ServiceSeconds is how long the driver spends at the door. A meal handover
	// is 90 seconds; a laundry pickup with a count is longer. The caller reads
	// it from the service type — which is a number, not a vertical concept.
	ServiceSeconds int
}

// Options tunes the sequencer.
type Options struct {
	// AverageSpeedKMH over road distance, not straight-line.
	AverageSpeedKMH float64
	// DetourFactor converts great-circle distance to road distance.
	//
	// 1.35 is the standard urban figure and it is a lie everywhere, but it is a
	// consistent lie, which is what an ETA needs. The alternative is a routing
	// API call per leg — 400 calls to plan twenty routes, on every replan.
	DetourFactor float64
	// MaxStops caps a route. Zero means unlimited.
	MaxStops int
	// DefaultServiceSeconds applies to stops that do not carry their own.
	DefaultServiceSeconds int
}

func DefaultOptions() Options {
	return Options{
		// Deliberately pessimistic for a Gulf city at delivery hours. An ETA
		// that is usually early costs nothing; one that is usually late costs a
		// support contact per order.
		AverageSpeedKMH:       24,
		DetourFactor:          1.35,
		MaxStops:              20,
		DefaultServiceSeconds: 180,
	}
}

func (o Options) normalise() Options {
	if o.AverageSpeedKMH <= 0 {
		o.AverageSpeedKMH = 24
	}
	if o.DetourFactor < 1 {
		o.DetourFactor = 1.35
	}
	if o.DefaultServiceSeconds <= 0 {
		o.DefaultServiceSeconds = 180
	}
	return o
}

// ─────────────────────────────────────────────────────────────────────────────
// Distance
// ─────────────────────────────────────────────────────────────────────────────

const earthRadiusM = 6_371_000.0

// HaversineM is great-circle distance in metres.
func HaversineM(a, b Point) float64 {
	lat1 := a.Lat * math.Pi / 180
	lat2 := b.Lat * math.Pi / 180
	dLat := (b.Lat - a.Lat) * math.Pi / 180
	dLng := (b.Lng - a.Lng) * math.Pi / 180

	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * earthRadiusM * math.Asin(math.Min(1, math.Sqrt(h)))
}

func (o Options) roadM(a, b Point) float64 { return HaversineM(a, b) * o.DetourFactor }

func (o Options) travelSeconds(a, b Point) int {
	metres := o.roadM(a, b)
	return int(metres / (o.AverageSpeedKMH * 1000 / 3600))
}

// ─────────────────────────────────────────────────────────────────────────────
// Precedence
// ─────────────────────────────────────────────────────────────────────────────

// precedes reports whether a must come before b.
//
// The only ordering constraint dispatch knows, and it is expressed entirely in
// seam terms: within one parent, a lower sequence comes first. A laundry order's
// pickup is sequence 1 and its delivery is sequence 2, so the pickup is visited
// first — and this function has no idea that is what it just guaranteed.
//
// Across parents there is no constraint at all, which is what lets a driver
// interleave two orders' stops by geography.
func precedes(a, b Stop) bool {
	return a.ParentID == b.ParentID && a.ParentID != uuid.Nil && a.Sequence < b.Sequence
}

// feasible reports whether an ordering violates precedence.
func feasible(stops []Stop) bool {
	for i := range stops {
		for j := i + 1; j < len(stops); j++ {
			if precedes(stops[j], stops[i]) {
				return false
			}
		}
	}
	return true
}

// ─────────────────────────────────────────────────────────────────────────────
// Sequencing
// ─────────────────────────────────────────────────────────────────────────────

// Sequence orders a set of stops into a route from an origin.
//
// Nearest-neighbour construction followed by a precedence-preserving 2-opt. Not
// optimal, and optimality is not the goal: at twenty stops the difference
// between this and an exact solver is a few percent of distance, while the
// difference between *any* sequencing and the arrival order is often 40%. The
// gap worth closing is the first one.
//
// Deterministic for a given input. Ties break on fulfillment id, so two planning
// runs over the same day produce the same route — which matters because a
// driver watching their manifest reshuffle every time the planner runs stops
// trusting it.
func Sequence(origin Point, stops []Stop, opts Options) []Stop {
	opts = opts.normalise()
	if len(stops) <= 1 {
		return append([]Stop(nil), stops...)
	}

	remaining := append([]Stop(nil), stops...)
	// Sort first so nearest-neighbour ties resolve deterministically rather than
	// by the order the database happened to return rows in.
	sort.Slice(remaining, func(i, j int) bool {
		return remaining[i].FulfillmentID.String() < remaining[j].FulfillmentID.String()
	})

	route := make([]Stop, 0, len(remaining))
	current := origin

	for len(remaining) > 0 {
		best := -1
		bestCost := math.MaxFloat64

		for i, s := range remaining {
			// Skip anything whose predecessor is still unvisited.
			if blocked(s, remaining, i) {
				continue
			}
			cost := opts.roadM(current, s.At)
			// A stop whose window is already closing outranks a nearer one that
			// is open all afternoon. Weighted rather than hard-constrained
			// because refusing to sequence an infeasible set produces no route
			// at all, and a late delivery beats no delivery.
			if !s.WindowEnd.IsZero() {
				cost -= urgencyBonus(s, opts)
			}
			if cost < bestCost {
				best, bestCost = i, cost
			}
		}

		if best < 0 {
			// Every remaining stop is blocked, which means a precedence cycle —
			// two fulfillments in one parent each claiming to precede the other.
			// Impossible through the seam (sequence is a total order within a
			// parent) but not impossible through a bad backfill. Emit the rest
			// in sequence order rather than looping forever.
			sort.Slice(remaining, func(i, j int) bool {
				if remaining[i].ParentID == remaining[j].ParentID {
					return remaining[i].Sequence < remaining[j].Sequence
				}
				return remaining[i].FulfillmentID.String() < remaining[j].FulfillmentID.String()
			})
			return append(route, remaining...)
		}

		route = append(route, remaining[best])
		current = remaining[best].At
		remaining = append(remaining[:best], remaining[best+1:]...)
	}

	return twoOpt(origin, route, opts)
}

// blocked reports whether some other unvisited stop must come before s.
func blocked(s Stop, remaining []Stop, self int) bool {
	for i, other := range remaining {
		if i == self {
			continue
		}
		if precedes(other, s) {
			return true
		}
	}
	return false
}

// urgencyBonus discounts the apparent distance of a stop whose window is
// closing.
//
// Expressed in metres so it can be subtracted from a distance directly. One
// minute of remaining slack is worth roughly one metre of detour — a stop with
// two hours left is discounted 120 m, which is noise, and a stop with five
// minutes left is discounted almost nothing but ranks above everything with
// slack because the others get discounted more. Capped so a stop with a
// pathological window cannot dominate the whole route.
func urgencyBonus(s Stop, opts Options) float64 {
	if s.WindowEnd.IsZero() || s.WindowStart.IsZero() {
		return 0
	}
	slack := s.WindowEnd.Sub(s.WindowStart).Minutes()
	if slack < 0 {
		slack = 0
	}
	bonus := 3000 - slack*10
	if bonus < 0 {
		return 0
	}
	if bonus > 3000 {
		return 3000
	}
	return bonus
}

// twoOpt reverses segments while the total gets shorter and precedence holds.
//
// Bounded iterations rather than "until no improvement": the loop provably
// terminates because every accepted move strictly shortens the route, but the
// bound is there so a floating-point plateau cannot spin a planner that runs
// every few minutes.
func twoOpt(origin Point, route []Stop, opts Options) []Stop {
	const maxPasses = 40
	n := len(route)
	if n < 4 {
		return route
	}

	best := append([]Stop(nil), route...)
	bestLen := TotalDistanceM(origin, best, opts)

	for pass := 0; pass < maxPasses; pass++ {
		improved := false
		for i := 0; i < n-1; i++ {
			for j := i + 1; j < n; j++ {
				candidate := append([]Stop(nil), best...)
				reverse(candidate[i : j+1])
				if !feasible(candidate) {
					continue
				}
				if l := TotalDistanceM(origin, candidate, opts); l < bestLen-0.5 {
					best, bestLen, improved = candidate, l, true
				}
			}
		}
		if !improved {
			break
		}
	}
	return best
}

func reverse(s []Stop) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

// TotalDistanceM is the road distance of a route from an origin.
//
// It does not return to the origin. A meal round trip does, and a laundry
// pickup run ends at the last customer; modelling the return leg would make the
// second case wrong by the length of the city. Callers that need a round trip
// add the closing leg themselves.
func TotalDistanceM(origin Point, route []Stop, opts Options) float64 {
	opts = opts.normalise()
	total := 0.0
	current := origin
	for _, s := range route {
		total += opts.roadM(current, s.At)
		current = s.At
	}
	return total
}

// ─────────────────────────────────────────────────────────────────────────────
// Timing
// ─────────────────────────────────────────────────────────────────────────────

// Leg is one hop of a planned route.
type Leg struct {
	Stop      Stop
	DistanceM int
	DurationS int
	ETA       time.Time
	// Late is true when the ETA falls after the stop's window closes. Computed
	// at plan time so a route can be rejected or reassigned *before* a driver
	// starts it, rather than discovered by the customer.
	Late bool
}

// Plan turns an ordered route into legs with ETAs.
//
// Waiting is modelled: a driver who arrives before the window opens waits, and
// the wait pushes every subsequent ETA. Omitting that makes early-morning routes
// look an hour faster than they are, which is exactly the case where the
// promised time matters most.
func Plan(origin Point, start time.Time, route []Stop, opts Options) []Leg {
	opts = opts.normalise()

	legs := make([]Leg, 0, len(route))
	current := origin
	clock := start

	for _, s := range route {
		distance := opts.roadM(current, s.At)
		travel := opts.travelSeconds(current, s.At)
		arrival := clock.Add(time.Duration(travel) * time.Second)

		if !s.WindowStart.IsZero() && arrival.Before(s.WindowStart) {
			arrival = s.WindowStart
		}

		service := s.ServiceSeconds
		if service <= 0 {
			service = opts.DefaultServiceSeconds
		}

		legs = append(legs, Leg{
			Stop:      s,
			DistanceM: int(distance),
			DurationS: travel,
			ETA:       arrival,
			Late:      !s.WindowEnd.IsZero() && arrival.After(s.WindowEnd),
		})

		clock = arrival.Add(time.Duration(service) * time.Second)
		current = s.At
	}
	return legs
}

// ─────────────────────────────────────────────────────────────────────────────
// Splitting
// ─────────────────────────────────────────────────────────────────────────────

// Split divides a sequenced route into driver-sized chunks.
//
// Applied *after* sequencing rather than before, so the chunks are geographic
// neighbours: splitting first and sequencing each half independently produces
// two routes that cross the city through each other, which is the classic way to
// make a router look worse than no router.
//
// Precedence survives the split within a chunk, and a parent's stops landing in
// two different chunks is left alone deliberately — two drivers handling one
// order's pickup and delivery is normal when they happen hours apart.
func Split(route []Stop, maxStops int) [][]Stop {
	if maxStops <= 0 || len(route) <= maxStops {
		if len(route) == 0 {
			return nil
		}
		return [][]Stop{route}
	}

	var out [][]Stop
	for i := 0; i < len(route); i += maxStops {
		end := i + maxStops
		if end > len(route) {
			end = len(route)
		}
		out = append(out, route[i:end])
	}
	return out
}
