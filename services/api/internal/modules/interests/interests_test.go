package interests

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
)

var (
	food    = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	laundry = uuid.MustParse("22222222-2222-2222-2222-222222222222")
)

func profileFor(ps []Profile, id uuid.UUID) (Profile, bool) {
	for _, p := range ps {
		if p.CategoryID == id {
			return p, true
		}
	}
	return Profile{}, false
}

func TestSkipsProduceNegativeAffinity(t *testing.T) {
	// A viewer who has skipped nine laundry reels has told you more than one who
	// has watched none. Fed only positives, every profile converges on "likes
	// everything" and personalisation stops meaning anything.
	now := time.Now()
	var sigs []Signal
	for i := 0; i < 9; i++ {
		sigs = append(sigs, Signal{CategoryID: laundry, Kind: "skip", Age: time.Hour})
	}

	p, ok := profileFor(Fold(sigs, now), laundry)
	if !ok {
		t.Fatal("no profile was produced for a category with only negative signal")
	}
	if p.Affinity >= 0 {
		t.Fatalf("nine skips produced affinity %v, want negative", p.Affinity)
	}
	if p.Negative != 9 {
		t.Errorf("negative count = %d, want 9", p.Negative)
	}
}

func TestOrderedIntentBeatsPassiveWatching(t *testing.T) {
	// Five watched reels against one CTA press. The click should win: it is the
	// only one of the two that cost the viewer a page load, and it is one step
	// from the transaction the north star counts. (Six views is the break-even
	// point by construction — the weights are 1 and 6.)
	now := time.Now()

	watched := make([]Signal, 5)
	for i := range watched {
		watched[i] = Signal{CategoryID: laundry, Kind: "view_progress", Age: time.Hour}
	}
	clicked := []Signal{{CategoryID: food, Kind: "action_click", Age: time.Hour}}

	watchedP, _ := profileFor(Fold(watched, now), laundry)
	clickedP, _ := profileFor(Fold(clicked, now), food)

	if clickedP.Affinity <= watchedP.Affinity {
		t.Fatalf("one CTA press (%v) did not beat five views (%v)",
			clickedP.Affinity, watchedP.Affinity)
	}
}

func TestSignalsDecayByHalfLife(t *testing.T) {
	if got := Decay(0, HalfLife); math.Abs(got-1) > 1e-9 {
		t.Errorf("a signal from now decayed to %v", got)
	}
	if got := Decay(HalfLife, HalfLife); math.Abs(got-0.5) > 1e-9 {
		t.Errorf("decay at the half-life = %v, want 0.5", got)
	}
	if got := Decay(4*HalfLife, HalfLife); got > 0.07 {
		t.Errorf("a signal four half-lives old still contributes %v", got)
	}
}

func TestOldSignalsCountForLessThanNewOnes(t *testing.T) {
	// Someone who spent a fortnight shopping for a car should not still be shown
	// gearbox walkarounds in November.
	now := time.Now()
	recent := []Signal{{CategoryID: food, Kind: "save", Age: time.Hour}}
	stale := []Signal{{CategoryID: food, Kind: "save", Age: 8 * 7 * 24 * time.Hour}}

	r, _ := profileFor(Fold(recent, now), food)
	s, _ := profileFor(Fold(stale, now), food)

	if r.Affinity <= s.Affinity {
		t.Fatalf("a two-month-old save (%v) matched a one-hour-old one (%v)",
			s.Affinity, r.Affinity)
	}
}

func TestReversalsCancelTheSignalTheyReverse(t *testing.T) {
	// An unlike that decayed at a different rate from the like it reverses would
	// leave a residue of a preference the viewer explicitly withdrew.
	now := time.Now()
	sigs := []Signal{
		{CategoryID: food, Kind: "like", Age: time.Hour},
		{CategoryID: food, Kind: "unlike", Age: time.Hour},
	}

	p, ok := profileFor(Fold(sigs, now), food)
	if !ok {
		t.Fatal("no profile produced")
	}
	if math.Abs(p.Affinity) > 1e-9 {
		t.Fatalf("a like and its unlike left affinity %v", p.Affinity)
	}
}

func TestAffinityStaysInsideTheColumnConstraint(t *testing.T) {
	// viewer_interests.affinity is CHECK-constrained to [-1, 1]. A Fold that can
	// exceed it turns a heavy user into a failed INSERT, and the profile that
	// fails to save is always the one with the most signal in it.
	now := time.Now()

	var enormous []Signal
	for i := 0; i < 5000; i++ {
		enormous = append(enormous, Signal{CategoryID: food, Kind: "save", Age: time.Minute})
	}
	p, _ := profileFor(Fold(enormous, now), food)
	if p.Affinity > 1 || p.Affinity < -1 {
		t.Fatalf("affinity = %v, outside the column's [-1, 1]", p.Affinity)
	}

	var awful []Signal
	for i := 0; i < 5000; i++ {
		awful = append(awful, Signal{CategoryID: laundry, Kind: "skip", Age: time.Minute})
	}
	n, _ := profileFor(Fold(awful, now), laundry)
	if n.Affinity < -1 || n.Affinity > 1 {
		t.Fatalf("affinity = %v, outside the column's [-1, 1]", n.Affinity)
	}
}

func TestSaturationDistinguishesLikesFromLoves(t *testing.T) {
	// tanh rather than a hard clamp: with a clamp every heavy user of a category
	// has an identical affinity of exactly 1.0, and the ranker loses the ability
	// to tell "likes food" from "orders food four times a week".
	now := time.Now()

	few := make([]Signal, 3)
	many := make([]Signal, 30)
	for i := range few {
		few[i] = Signal{CategoryID: food, Kind: "like", Age: time.Hour}
	}
	for i := range many {
		many[i] = Signal{CategoryID: food, Kind: "like", Age: time.Hour}
	}

	a, _ := profileFor(Fold(few, now), food)
	b, _ := profileFor(Fold(many, now), food)
	if b.Affinity <= a.Affinity {
		t.Fatalf("thirty likes (%v) did not exceed three (%v)", b.Affinity, a.Affinity)
	}
	if b.Affinity >= 1.0 {
		t.Fatalf("affinity saturated to exactly %v; there is no headroom left", b.Affinity)
	}
}

func TestUnknownEventTypesDoNotSteerTheFeed(t *testing.T) {
	// An event type added for instrumentation must not silently start
	// influencing ranking because someone chose a positive-sounding name.
	now := time.Now()
	sigs := []Signal{{CategoryID: food, Kind: "debug_render_timing", Age: time.Minute}}
	if got := Fold(sigs, now); len(got) != 0 {
		t.Fatalf("an unrecognised event type produced a profile: %+v", got)
	}
}

func TestSignalsWithoutACategoryAreIgnored(t *testing.T) {
	// reels.category_id is nullable, so this is the common case rather than a
	// defensive one — and a profile keyed on the nil uuid would be a bucket that
	// silently accumulates every uncategorised interaction.
	now := time.Now()
	sigs := []Signal{{CategoryID: uuid.Nil, Kind: "like", Age: time.Minute}}
	if got := Fold(sigs, now); len(got) != 0 {
		t.Fatalf("a signal with no category produced %+v", got)
	}
}

func TestLastSignalIsTheMostRecentNotTheOldest(t *testing.T) {
	now := time.Now()
	sigs := []Signal{
		{CategoryID: food, Kind: "like", Age: 30 * 24 * time.Hour},
		{CategoryID: food, Kind: "like", Age: 2 * time.Hour},
	}
	p, _ := profileFor(Fold(sigs, now), food)

	if d := now.Sub(p.LastSignal); d > 3*time.Hour {
		t.Fatalf("last_signal_at is %v old, want ~2h", d)
	}
}

func TestUserIDFromKey(t *testing.T) {
	id := uuid.New()
	if got, ok := UserIDFromKey("u:" + id.String()); !ok || got != id {
		t.Errorf("UserIDFromKey lost the id: %v %v", got, ok)
	}
	for _, bad := range []string{"", "u:", "d:some-device", "u:not-a-uuid", id.String()} {
		if _, ok := UserIDFromKey(bad); ok {
			t.Errorf("UserIDFromKey(%q) claimed a user id", bad)
		}
	}
}
