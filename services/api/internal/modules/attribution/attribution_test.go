package attribution

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

var (
	reelA     = uuid.MustParse("aaaaaaaa-0000-0000-0000-000000000001")
	reelB     = uuid.MustParse("bbbbbbbb-0000-0000-0000-000000000002")
	actionA   = uuid.MustParse("acacacac-0000-0000-0000-000000000001")
	offering1 = uuid.MustParse("0ffe1111-0000-0000-0000-000000000001")
	offering2 = uuid.MustParse("0ffe2222-0000-0000-0000-000000000002")
	branch1   = uuid.MustParse("b4a11111-0000-0000-0000-000000000001")
	vendor1   = uuid.MustParse("de111111-0000-0000-0000-000000000001")
)

var placedAt = time.Date(2026, 7, 28, 15, 0, 0, 0, time.UTC)

func baseOrder() Order {
	return Order{
		ID:       uuid.New(),
		PlacedAt: placedAt,
		GMVMinor: 12_500,
		Currency: "AED",
		BranchID: branch1,
		VendorID: vendor1,
		Targets:  []Target{{Type: "service_offering", ID: offering1}},
	}
}

func clickAt(d time.Duration, target uuid.UUID) Click {
	return Click{
		ID:         1,
		ActionID:   actionA,
		ReelID:     reelA,
		TargetType: "service_offering",
		TargetID:   target,
		Surface:    "feed",
		ClickedAt:  placedAt.Add(d),
	}
}

func TestLastClickWins(t *testing.T) {
	got := Resolve(baseOrder(), Evidence{
		Clicks: []Click{clickAt(-30*time.Minute, offering1)},
	}, placedAt)

	if got.Model != ModelLastClick {
		t.Fatalf("model = %s, want last_click", got.Model)
	}
	if got.ReelID == nil || *got.ReelID != reelA {
		t.Fatalf("reel = %v, want %s", got.ReelID, reelA)
	}
	if got.Latency != 30*time.Minute {
		t.Errorf("latency = %s, want 30m", got.Latency)
	}
}

func TestClickOutsideWindowIsNotCredited(t *testing.T) {
	// A 7-day window would roughly double attributed conversions without
	// doubling anything real, and that inflated number is what Phase 4 learns
	// from.
	got := Resolve(baseOrder(), Evidence{
		Clicks: []Click{clickAt(-25*time.Hour, offering1)},
	}, placedAt)

	if got.Model != ModelNone {
		t.Fatalf("model = %s, want none for a click 25h before the order", got.Model)
	}
}

func TestClickAfterTheOrderIsNotCredited(t *testing.T) {
	// Not hypothetical: a client that batches events can deliver a click whose
	// timestamp lands after the order it followed.
	got := Resolve(baseOrder(), Evidence{
		Clicks: []Click{clickAt(5*time.Minute, offering1)},
	}, placedAt)

	if got.Model != ModelNone {
		t.Fatalf("a click after the order was credited as %s", got.Model)
	}
}

func TestClickMustPointAtSomethingTheOrderBought(t *testing.T) {
	// Without this, a viewer who clicks a laundry CTA and then orders an
	// unrelated vendor's meal plan credits the laundry reel — and every reel in
	// a busy user's history starts collecting conversions it did nothing for.
	got := Resolve(baseOrder(), Evidence{
		Clicks: []Click{clickAt(-10*time.Minute, offering2)},
	}, placedAt)

	if got.Model != ModelNone {
		t.Fatalf("a click on an unrelated offering was credited as %s", got.Model)
	}
}

func TestMostRecentMatchingClickWins(t *testing.T) {
	older := clickAt(-3*time.Hour, offering1)
	older.ID, older.ReelID = 1, reelB
	newer := clickAt(-10*time.Minute, offering1)
	newer.ID, newer.ReelID = 2, reelA

	// Evidence arrives most-recent-first, as the repository orders it.
	got := Resolve(baseOrder(), Evidence{Clicks: []Click{newer, older}}, placedAt)

	if got.ReelID == nil || *got.ReelID != reelA {
		t.Fatalf("reel = %v, want the more recent click's reel %s", got.ReelID, reelA)
	}
}

func TestBranchTargetMatchesAnOrderFromThatBranch(t *testing.T) {
	// `open_menu` and `follow` point at a branch, not an offering. An order
	// from that branch is the outcome they were driving toward.
	c := clickAt(-20*time.Minute, branch1)
	c.TargetType = "vendor_branch"

	got := Resolve(baseOrder(), Evidence{Clicks: []Click{c}}, placedAt)
	if got.Model != ModelLastClick {
		t.Fatalf("branch-targeted CTA was not credited: %s", got.Model)
	}
}

func TestExternalURLTargetsAreNeverCredited(t *testing.T) {
	// Nothing in this system can see the outcome of an external link, so
	// crediting it would be inventing a conversion.
	c := clickAt(-5*time.Minute, offering1)
	c.TargetType = "url"

	got := Resolve(baseOrder(), Evidence{Clicks: []Click{c}}, placedAt)
	if got.Model != ModelNone {
		t.Fatalf("an external link was credited as %s", got.Model)
	}
}

// --- the precedence ladder ---------------------------------------------------

func TestVerifiableClickBeatsUnverifiableClaim(t *testing.T) {
	o := baseOrder()
	o.DeclaredReelID = &reelB

	got := Resolve(o, Evidence{Clicks: []Click{clickAt(-5*time.Minute, offering1)}}, placedAt)

	if got.Model != ModelLastClick {
		t.Fatalf("model = %s, want last_click — evidence must beat a claim", got.Model)
	}
	if *got.ReelID != reelA {
		t.Fatalf("credited the claimed reel %s over the observed one %s", *got.ReelID, reelA)
	}
}

func TestDeclaredIsRecordedButNeverAsLastClick(t *testing.T) {
	// The client's claim is kept — a share link opened outside the app is a real
	// path — but it is never merged into the verifiable bucket. The whole point
	// of separating them is that one is checkable and one is not.
	o := baseOrder()
	o.DeclaredReelID = &reelB
	o.DeclaredActionID = &actionA

	got := Resolve(o, Evidence{}, placedAt)

	if got.Model != ModelDeclared {
		t.Fatalf("model = %s, want declared", got.Model)
	}
	if got.Model.Verifiable() {
		t.Fatal("declared must not report as verifiable")
	}
	if !got.OffFeed {
		t.Fatal("an uncorroborated declaration must not count toward the feed's own number")
	}
}

func TestClaimBeatsViewThrough(t *testing.T) {
	o := baseOrder()
	o.DeclaredReelID = &reelB

	got := Resolve(o, Evidence{Impressions: []Impression{{
		ReelID: reelA, TargetType: "service_offering", TargetID: offering1,
		Surface: "feed", ServedAt: placedAt.Add(-time.Hour),
	}}}, placedAt)

	if got.Model != ModelDeclared {
		t.Fatalf("model = %s, want declared to outrank view_through", got.Model)
	}
}

func TestViewThroughIsCreditedButKeptSeparate(t *testing.T) {
	got := Resolve(baseOrder(), Evidence{Impressions: []Impression{{
		ReelID: reelA, TargetType: "service_offering", TargetID: offering1,
		Surface: "feed", ServedAt: placedAt.Add(-90 * time.Minute),
	}}}, placedAt)

	if got.Model != ModelViewThrough {
		t.Fatalf("model = %s, want view_through", got.Model)
	}
	if got.Model.Verifiable() {
		t.Fatal("view_through must never report as verifiable — that is the flattering merge")
	}
	if !got.Model.Attributed() {
		t.Fatal("view_through is still an attribution")
	}
}

func TestViewThroughWindowIsTighterThanClick(t *testing.T) {
	// A click is a stated intent that survives a night's sleep. A view is not.
	if ViewThroughWindow >= ClickWindow {
		t.Fatalf("view-through window %s must be tighter than the click window %s",
			ViewThroughWindow, ClickWindow)
	}

	got := Resolve(baseOrder(), Evidence{Impressions: []Impression{{
		ReelID: reelA, TargetType: "service_offering", TargetID: offering1,
		ServedAt: placedAt.Add(-5 * time.Hour),
	}}}, placedAt)

	if got.Model != ModelNone {
		t.Fatalf("an impression 5h before the order was credited as %s", got.Model)
	}
}

func TestOrganicOrdersAttributeToNothing(t *testing.T) {
	got := Resolve(baseOrder(), Evidence{}, placedAt)

	if got.Model != ModelNone {
		t.Fatalf("model = %s, want none", got.Model)
	}
	if got.ReelID != nil {
		t.Fatalf("an organic order was credited to reel %s", *got.ReelID)
	}
	if got.Model.Attributed() {
		t.Fatal("none must not report as attributed")
	}
}

// --- off-feed separation -----------------------------------------------------

func TestSharedLinkTrafficIsFlaggedOffFeed(t *testing.T) {
	// A vendor pushing their own reel to a WhatsApp group converts at rates the
	// feed will never approach. Letting that into the headline number is how a
	// team convinces itself to keep funding a feed nobody uses.
	c := clickAt(-10*time.Minute, offering1)
	c.Surface = "share_link"

	got := Resolve(baseOrder(), Evidence{Clicks: []Click{c}}, placedAt)

	if got.Model != ModelLastClick {
		t.Fatalf("model = %s, want last_click — off-feed traffic still converts", got.Model)
	}
	if !got.OffFeed {
		t.Fatal("share_link traffic was not flagged off-feed")
	}
}

func TestFeedAndSearchCountAsOnFeed(t *testing.T) {
	for _, surface := range []string{"feed", "search"} {
		c := clickAt(-time.Minute, offering1)
		c.Surface = surface
		if got := Resolve(baseOrder(), Evidence{Clicks: []Click{c}}, placedAt); got.OffFeed {
			t.Errorf("surface %q was flagged off-feed", surface)
		}
	}
}

func TestGMVIsCarriedOntoTheAttribution(t *testing.T) {
	// Snapshotted so the funnel does not re-derive revenue from a quote that
	// may since have been adjusted — laundry-by-weight moves after booking.
	got := Resolve(baseOrder(), Evidence{
		Clicks: []Click{clickAt(-time.Minute, offering1)},
	}, placedAt)

	if got.GMVMinor != 12_500 || got.Currency != "AED" {
		t.Fatalf("gmv = %d %s, want 12500 AED", got.GMVMinor, got.Currency)
	}
}

func TestEveryResultCarriesAReason(t *testing.T) {
	// A vendor will dispute these numbers, because they determine what the
	// vendor thinks the platform is worth. "The query said so" is not an answer.
	cases := []struct {
		name string
		o    Order
		e    Evidence
	}{
		{"click", baseOrder(), Evidence{Clicks: []Click{clickAt(-time.Minute, offering1)}}},
		{"none", baseOrder(), Evidence{}},
		{"view_through", baseOrder(), Evidence{Impressions: []Impression{{
			ReelID: reelA, TargetType: "service_offering", TargetID: offering1,
			ServedAt: placedAt.Add(-time.Hour)}}}},
	}
	for _, tc := range cases {
		if got := Resolve(tc.o, tc.e, placedAt); got.Reason == "" {
			t.Errorf("%s: empty reason", tc.name)
		}
	}
}
