package dispatch

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// Dubai landmarks, roughly. Real coordinates rather than a unit grid, because
// the thing being tested includes a haversine and a detour factor, and a grid
// would let a degrees-versus-radians bug through unnoticed.
var (
	kitchen   = Point{25.1180, 55.2010} // Al Quoz
	marina    = Point{25.0805, 55.1403}
	jlt       = Point{25.0693, 55.1428}
	downtown  = Point{25.1972, 55.2744}
	deira     = Point{25.2697, 55.3095}
	businessB = Point{25.1857, 55.2650}
)

var day = time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)

func win(fromHour, toHour int) (time.Time, time.Time) {
	return day.Add(time.Duration(fromHour) * time.Hour), day.Add(time.Duration(toHour) * time.Hour)
}

func stop(at Point, fromHour, toHour int) Stop {
	s, e := win(fromHour, toHour)
	return Stop{FulfillmentID: uuid.New(), At: at, WindowStart: s, WindowEnd: e}
}

func TestHaversineIsInMetres(t *testing.T) {
	// Marina to Deira is about 25 km in a straight line. A radians/degrees
	// mix-up shows up here as a number off by a factor of 57.
	d := HaversineM(marina, deira)
	if d < 20_000 || d > 30_000 {
		t.Fatalf("got %.0f m, expected roughly 25 km", d)
	}
	if HaversineM(marina, marina) != 0 {
		t.Fatal("distance to self must be zero")
	}
}

func TestSequencingBeatsArrivalOrder(t *testing.T) {
	// The claim the whole package rests on. If sequencing did not comfortably
	// beat "whatever order the rows came back in", none of this would be worth
	// the code.
	//
	// The input is deliberately adversarial: alternating ends of the city.
	arrival := []Stop{
		stop(deira, 9, 21),
		stop(marina, 9, 21),
		stop(downtown, 9, 21),
		stop(jlt, 9, 21),
		stop(businessB, 9, 21),
	}
	opts := DefaultOptions()

	naive := TotalDistanceM(kitchen, arrival, opts)
	planned := TotalDistanceM(kitchen, Sequence(kitchen, arrival, opts), opts)

	if planned >= naive {
		t.Fatalf("sequenced route %.0f m is no better than arrival order %.0f m", planned, naive)
	}
	t.Logf("arrival order %.0f m, sequenced %.0f m (%.0f%% shorter)",
		naive, planned, 100*(naive-planned)/naive)
}

func TestPickupPrecedesDeliveryWithoutKnowingWhatLaundryIs(t *testing.T) {
	// The seam result. Dispatch is told `(parent, sequence)` and nothing else,
	// and that alone is enough to guarantee a laundry bag is collected before it
	// is returned.
	//
	// The geometry is rigged against it: the delivery is next door to the
	// kitchen and the pickup is across the city, so pure nearest-neighbour would
	// visit the delivery first every time.
	order := uuid.New()
	pickup := Stop{FulfillmentID: uuid.New(), ParentID: order, Sequence: 1, Kind: "pickup", At: deira}
	delivery := Stop{FulfillmentID: uuid.New(), ParentID: order, Sequence: 2, Kind: "delivery", At: marina}

	got := Sequence(kitchen, []Stop{delivery, pickup}, DefaultOptions())

	if got[0].FulfillmentID != pickup.FulfillmentID {
		t.Fatalf("delivery was sequenced before its own pickup: %+v", got)
	}
}

func TestPrecedenceHoldsAcrossManyStops(t *testing.T) {
	// Two orders interleaved with unrelated deliveries. The router is free to
	// interleave *across* parents — that is the point — but never within one.
	a, b := uuid.New(), uuid.New()
	stops := []Stop{
		{FulfillmentID: uuid.New(), ParentID: a, Sequence: 2, At: marina},
		{FulfillmentID: uuid.New(), ParentID: b, Sequence: 2, At: deira},
		stop(downtown, 9, 21),
		{FulfillmentID: uuid.New(), ParentID: a, Sequence: 1, At: downtown},
		stop(jlt, 9, 21),
		{FulfillmentID: uuid.New(), ParentID: b, Sequence: 1, At: businessB},
	}

	got := Sequence(kitchen, stops, DefaultOptions())
	if !feasible(got) {
		t.Fatalf("precedence violated:\n%+v", got)
	}
	if len(got) != len(stops) {
		t.Fatalf("got %d stops, want %d — sequencing dropped work", len(got), len(stops))
	}
}

func TestTwoOptNeverLengthensTheRoute(t *testing.T) {
	// A refinement pass that can make things worse is worse than no refinement
	// pass, because it only ever runs on routes a human is about to drive.
	opts := DefaultOptions()
	for trial := 0; trial < 20; trial++ {
		stops := []Stop{
			stop(marina, 9, 21), stop(deira, 9, 21), stop(downtown, 9, 21),
			stop(jlt, 9, 21), stop(businessB, 9, 21),
			stop(Point{25.15, 55.22}, 9, 21), stop(Point{25.23, 55.29}, 9, 21),
		}
		constructed := Sequence(kitchen, stops, opts)
		if got := TotalDistanceM(kitchen, constructed, opts); got > TotalDistanceM(kitchen, stops, opts)*1.0001 {
			t.Fatalf("trial %d: sequenced route is longer than the input order", trial)
		}
	}
}

func TestSequencingIsDeterministic(t *testing.T) {
	// A driver whose manifest reshuffles every time the planner runs stops
	// trusting it, and then stops following it.
	stops := []Stop{
		stop(marina, 9, 21), stop(deira, 9, 21), stop(downtown, 9, 21), stop(jlt, 9, 21),
	}
	first := Sequence(kitchen, stops, DefaultOptions())
	for i := 0; i < 10; i++ {
		again := Sequence(kitchen, stops, DefaultOptions())
		for j := range first {
			if first[j].FulfillmentID != again[j].FulfillmentID {
				t.Fatalf("run %d diverged at position %d", i, j)
			}
		}
	}
}

func TestSequencingDoesNotMutateItsInput(t *testing.T) {
	stops := []Stop{stop(marina, 9, 21), stop(deira, 9, 21), stop(downtown, 9, 21)}
	before := make([]uuid.UUID, len(stops))
	for i, s := range stops {
		before[i] = s.FulfillmentID
	}

	Sequence(kitchen, stops, DefaultOptions())

	for i, s := range stops {
		if s.FulfillmentID != before[i] {
			t.Fatal("Sequence reordered its caller's slice")
		}
	}
}

func TestTighterWindowsAreVisitedEarlier(t *testing.T) {
	// Two stops the same distance apart in opposite directions, one with a
	// 30-minute window and one open all day. Distance alone would coin-flip.
	tight := Stop{FulfillmentID: uuid.MustParse("00000000-0000-0000-0000-0000000000aa"),
		At: downtown}
	tight.WindowStart, tight.WindowEnd = win(9, 10)
	tight.WindowEnd = tight.WindowStart.Add(30 * time.Minute)

	loose := Stop{FulfillmentID: uuid.MustParse("00000000-0000-0000-0000-0000000000bb"),
		At: businessB}
	loose.WindowStart, loose.WindowEnd = win(9, 21)

	got := Sequence(kitchen, []Stop{loose, tight}, DefaultOptions())
	if got[0].FulfillmentID != tight.FulfillmentID {
		t.Fatalf("the 30-minute window was not visited first: %+v", got)
	}
}

func TestEmptyAndSingleRoutes(t *testing.T) {
	if got := Sequence(kitchen, nil, DefaultOptions()); len(got) != 0 {
		t.Fatal("empty in, empty out")
	}
	one := []Stop{stop(marina, 9, 21)}
	if got := Sequence(kitchen, one, DefaultOptions()); len(got) != 1 {
		t.Fatal("a single stop is already a route")
	}
}

// ── timing ───────────────────────────────────────────────────────────────────

func TestPlanWaitsForAWindowToOpen(t *testing.T) {
	// A driver leaving the kitchen at 08:00 for a 10:00–12:00 window waits. Not
	// modelling the wait makes early routes look an hour faster than they are —
	// exactly where the promised time matters most.
	s := stop(marina, 10, 12)
	legs := Plan(kitchen, day.Add(8*time.Hour), []Stop{s}, DefaultOptions())

	if legs[0].ETA.Before(s.WindowStart) {
		t.Fatalf("ETA %s is before the window opens at %s", legs[0].ETA, s.WindowStart)
	}
	if legs[0].Late {
		t.Fatal("arriving at the start of the window is not late")
	}
}

func TestPlanPropagatesAWaitToLaterStops(t *testing.T) {
	early := stop(marina, 10, 12)
	later := stop(jlt, 9, 21)
	legs := Plan(kitchen, day.Add(8*time.Hour), []Stop{early, later}, DefaultOptions())

	// The second stop is minutes from the first, but the driver did not leave
	// the first until 10:00 plus service time.
	if !legs[1].ETA.After(day.Add(10 * time.Hour)) {
		t.Fatalf("second ETA %s did not absorb the wait at the first stop", legs[1].ETA)
	}
}

func TestPlanFlagsLatenessBeforeTheDriverStarts(t *testing.T) {
	// The whole value of computing this at plan time: a route that cannot be
	// delivered on time is a reassignment decision, not a customer discovery.
	s := stop(deira, 9, 10)
	legs := Plan(kitchen, day.Add(9*time.Hour).Add(50*time.Minute), []Stop{s}, DefaultOptions())
	if !legs[0].Late {
		t.Fatalf("ETA %s is past the %s window close and was not flagged", legs[0].ETA, s.WindowEnd)
	}
}

func TestServiceTimeIsCountedAtEveryStop(t *testing.T) {
	opts := DefaultOptions()
	opts.DefaultServiceSeconds = 600 // ten minutes at the door

	a, b := stop(marina, 9, 21), stop(jlt, 9, 21)
	legs := Plan(kitchen, day.Add(9*time.Hour), []Stop{a, b}, opts)

	gap := legs[1].ETA.Sub(legs[0].ETA)
	if gap < 10*time.Minute {
		t.Fatalf("gap between stops is %s — service time was not counted", gap)
	}
}

func TestDetourFactorIsApplied(t *testing.T) {
	// Straight-line distance is a systematic underestimate of driving distance,
	// and an ETA built on it is systematically optimistic.
	opts := DefaultOptions()
	straight := HaversineM(kitchen, deira)
	road := opts.roadM(kitchen, deira)
	if road <= straight {
		t.Fatalf("road distance %.0f is not longer than straight line %.0f", road, straight)
	}
}

// ── splitting ────────────────────────────────────────────────────────────────

func TestSplitPreservesOrderAndLosesNothing(t *testing.T) {
	route := []Stop{
		stop(marina, 9, 21), stop(jlt, 9, 21), stop(downtown, 9, 21),
		stop(businessB, 9, 21), stop(deira, 9, 21),
	}
	chunks := Split(route, 2)
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}

	var flat []Stop
	for _, c := range chunks {
		if len(c) > 2 {
			t.Fatalf("chunk of %d exceeds the cap", len(c))
		}
		flat = append(flat, c...)
	}
	if len(flat) != len(route) {
		t.Fatalf("split lost work: %d in, %d out", len(route), len(flat))
	}
	for i := range route {
		if flat[i].FulfillmentID != route[i].FulfillmentID {
			t.Fatal("split reordered the route — chunks must stay geographic neighbours")
		}
	}
}

func TestSplitIsANoOpBelowTheCap(t *testing.T) {
	route := []Stop{stop(marina, 9, 21), stop(jlt, 9, 21)}
	if chunks := Split(route, 10); len(chunks) != 1 || len(chunks[0]) != 2 {
		t.Fatalf("got %v", chunks)
	}
	if chunks := Split(nil, 10); chunks != nil {
		t.Fatal("nothing to split is no chunks, not one empty chunk")
	}
}
