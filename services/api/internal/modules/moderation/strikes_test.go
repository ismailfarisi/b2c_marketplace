package moderation

import (
	"testing"
	"time"
)

// The claim in the source, tested rather than asserted: a single decision by a
// single moderator must not be able to end a business.
func TestOneSevereStrikeSuspendsButDoesNotBan(t *testing.T) {
	points := Severe.Points()
	if got := EnforcementFor(points); got != Suspended {
		t.Fatalf("one severe strike gives %s, want suspended", got)
	}
	if points >= BanAt {
		t.Fatal("one severe strike reaches the ban threshold — banning must need " +
			"either a second severe violation or a sustained pattern, so that at " +
			"least two people have looked")
	}
}

func TestTwoSevereStrikesDoBan(t *testing.T) {
	if got := EnforcementFor(2 * Severe.Points()); got != Banned {
		t.Fatalf("two severe strikes give %s, want banned", got)
	}
}

func TestTheLadderIsMonotonic(t *testing.T) {
	order := map[Enforcement]int{None: 0, Warned: 1, ReviewAll: 2, Suspended: 3, Banned: 4}
	last := 0
	for points := 0; points <= 40; points++ {
		got := order[EnforcementFor(points)]
		if got < last {
			t.Fatalf("at %d points enforcement went backwards: %s", points, EnforcementFor(points))
		}
		last = got
	}
}

func TestAnAuthorWithNoStrikesIsUnrestricted(t *testing.T) {
	if got := EnforcementFor(0); got != None {
		t.Fatalf("got %s, want none", got)
	}
}

// Gapped point values exist so that combinations of severities can be
// distinguished. With 1/2/3 the thresholds all land on values several
// combinations hit exactly, and "three minor captions" would be treated the
// same as "one serious violation".
func TestSeveritiesAreDistinguishable(t *testing.T) {
	if 3*Minor.Points() == Major.Points() {
		t.Fatal("three minors must not equal one major")
	}
	if Major.Points()+Minor.Points() >= Severe.Points() {
		t.Fatal("a major plus a minor must stay below one severe")
	}
	// And a pattern of majors must eventually escalate past a single severe.
	if 3*Major.Points() < Severe.Points() {
		t.Fatal("a sustained pattern of majors must escalate")
	}
}

// Strikes expiring is what stops the ladder being a one-way ratchet. On a
// marketplace, "permanent" means a vendor's business.
func TestStrikesExpireAndSeverityBuysDuration(t *testing.T) {
	if Minor.Lifetime() >= Major.Lifetime() || Major.Lifetime() >= Severe.Lifetime() {
		t.Fatal("a worse violation must count for longer")
	}
	if Minor.Lifetime() > 90*24*time.Hour {
		t.Fatalf("a minor caption violation counting for %v is not proportionate",
			Minor.Lifetime())
	}
	if Severe.Lifetime() > 2*365*24*time.Hour {
		t.Fatal("no strike should outlive two years — that is a ban by arithmetic")
	}
}

func TestSuspensionsEscalateAndAreCapped(t *testing.T) {
	short := SuspensionLength(SuspendAt)
	long := SuspensionLength(13)
	if long <= short {
		t.Fatal("more points must mean a longer suspension")
	}
	if long > 60*24*time.Hour {
		t.Fatalf("a %v suspension is a ban nobody decided to impose", long)
	}
	// Beyond the ban threshold the length stops mattering, but it must not run
	// away — an unbounded formula is how a nine-month suspension appears.
	if SuspensionLength(1000) > 60*24*time.Hour {
		t.Fatal("suspension length is unbounded")
	}
}

func TestUnknownSeverityFailsTowardTheLenientEnd(t *testing.T) {
	unknown := Severity("catastrophic")
	if unknown.Points() != Minor.Points() {
		t.Fatal("an unrecognised severity must not be able to ban someone by accident")
	}
	if unknown.Lifetime() != Minor.Lifetime() {
		t.Fatal("an unrecognised severity must take the shortest lifetime")
	}
}

// An appeal window with no SLA is a way of saying no slowly; an SLA with no
// window means no decision is ever final.
func TestAppealsHaveBothAWindowAndADeadline(t *testing.T) {
	if AppealWindow <= 0 || AppealSLA <= 0 {
		t.Fatal("both must be set")
	}
	if AppealSLA >= AppealWindow {
		t.Fatal("the platform must answer faster than the author has to ask, or the " +
			"queue is permanently behind")
	}
	if AppealSLA > 14*24*time.Hour {
		t.Fatalf("a %v SLA is not an appeals process", AppealSLA)
	}
}

func TestClaimTTLIsLongEnoughToReviewAndShortEnoughToRecover(t *testing.T) {
	if ClaimTTL < 5*time.Minute {
		t.Fatal("too short to watch a reel and decide")
	}
	if ClaimTTL > time.Hour {
		t.Fatal("a closed laptop must not hold an item for a whole shift")
	}
}
