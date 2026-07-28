package vehicles

import (
	"testing"
	"time"
)

func TestSoldIsNotTerminal(t *testing.T) {
	// A deal falling through is ordinary. If sold were terminal the seller would
	// re-create the listing, and the new row would have no price history, no
	// mileage history and no reel — which is exactly the evidence the fraud
	// detectors read.
	if !CanTransition(StatusSold, StatusActive) {
		t.Fatal("a sold listing must be able to go back on sale")
	}
	if !CanTransition(StatusSold, StatusRemoved) {
		t.Fatal("a sold listing must be removable")
	}
}

func TestRemovedIsTerminal(t *testing.T) {
	for _, to := range []Status{StatusActive, StatusDraft, StatusPendingReview, StatusSold} {
		if CanTransition(StatusRemoved, to) {
			t.Fatalf("removed -> %s must not be allowed", to)
		}
	}
}

func TestActiveEditsReenterReview(t *testing.T) {
	// The bait-and-switch shape: publish a clean listing, then edit the price
	// and mileage once it has traction. A material edit has to be reviewable.
	if !CanTransition(StatusActive, StatusPendingReview) {
		t.Fatal("an active listing must be able to re-enter review after a material edit")
	}
}

func TestNothingPublishesWithoutReview(t *testing.T) {
	// The only edge into active comes from pending_review or paused. If a draft
	// could go straight to active, the moderation queue would be optional.
	for from, tos := range transitions {
		for _, to := range tos {
			if to != StatusActive {
				continue
			}
			switch from {
			case StatusPendingReview, StatusPaused, StatusSold:
			default:
				t.Fatalf("%s -> active bypasses review", from)
			}
		}
	}
}

// ── VIN ──────────────────────────────────────────────────────────────────────

func TestNormaliseVINStripsWhatPeopleType(t *testing.T) {
	got := NormaliseVIN("  jtm hy05j-x04 512345 ")
	want := "JTMHY05JX04512345"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestWellFormedVINRejectsTheAmbiguousLetters(t *testing.T) {
	// I, O and Q are absent from the ISO 3779 alphabet precisely so nobody
	// transcribes them as 1 and 0. A VIN containing one was mistyped or made up.
	for _, vin := range []string{
		"1HGCM82633AO04352", // O
		"1HGCM82633AI04352", // I
		"1HGCM82633AQ04352", // Q
	} {
		if WellFormedVIN(vin) {
			t.Fatalf("%s contains an excluded letter and must not be well formed", vin)
		}
	}
	if WellFormedVIN("1HGCM82633A00435") {
		t.Fatal("16 characters is not a VIN")
	}
}

func TestCheckDigitAcceptsAKnownGoodVIN(t *testing.T) {
	// The canonical worked example from the ISO 3779 check-digit definition.
	if !CheckDigitValid("11111111111111111") {
		t.Fatal("the all-ones VIN is the standard's own worked example and must validate")
	}
	if !CheckDigitValid("1M8GDM9AXKP042788") {
		t.Fatal("known-good NHTSA sample VIN failed the check digit")
	}
}

func TestCheckDigitCatchesASingleTransposition(t *testing.T) {
	// This is the whole point of a weighted checksum: swapping two adjacent
	// characters changes the sum, because the weights differ by position.
	good := "1M8GDM9AXKP042788"
	bad := "1M8GDM9AXKP042878" // last two digits transposed
	if !CheckDigitValid(good) {
		t.Fatal("fixture is wrong: good VIN must validate")
	}
	if CheckDigitValid(bad) {
		t.Fatal("a transposition must fail the check digit")
	}
}

func TestCheckDigitIsASignalNotAValidator(t *testing.T) {
	// A well-formed VIN that fails the check digit must still be *well formed*.
	// If Validate rejected these, the listing flow would refuse most genuine
	// Japanese and European imports — which are a large share of this market.
	vin := "JTMHY05JX04512341"
	if !WellFormedVIN(vin) {
		t.Fatal("fixture must be well formed")
	}
	if CheckDigitValid(vin) {
		t.Fatal("fixture must fail the check digit for this test to mean anything")
	}

	d := validDraft()
	d.VIN = vin
	if issues := d.Validate(time.Now()); len(issues) != 0 {
		t.Fatalf("a failing check digit must not block a listing, got %+v", issues)
	}
}

// ── mileage ──────────────────────────────────────────────────────────────────

func TestClassifyMileage(t *testing.T) {
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		year    int
		mileage int
		want    MileageBand
	}{
		{"new car with delivery miles", 2026, 400, BandNormal},
		{"new car already driven hard", 2026, 60_000, BandHigh},
		{"typical five year old", 2021, 110_000, BandNormal},
		{"high but believable", 2021, 190_000, BandHigh},
		{"impossible", 2021, 400_000, BandImplausible},
		// The rollback shape. Also the garage-queen shape, which is why it
		// produces a signal for a human rather than a rejection.
		{"rolled back or garaged", 2018, 30_000, BandSuspiciouslyLow},
		// Two years old is not enough evidence: a second car genuinely does
		// 8,000 km a year and flagging it would bury reviewers in noise.
		{"too young to judge", 2025, 4_000, BandNormal},
		// A next-model-year car sold in August is normal, not a time traveller.
		{"model year ahead of the calendar", 2027, 50, BandNormal},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClassifyMileage(c.year, c.mileage, now); got != c.want {
				t.Fatalf("got %s want %s", got, c.want)
			}
		})
	}
}

func TestAnnualKMIsAGCCFigure(t *testing.T) {
	// Guarding the constant rather than the code. A European benchmark of
	// ~12,000 km would classify a large share of honest Dubai listings as high
	// mileage, and the band would stop meaning anything.
	if AnnualKM < 18_000 {
		t.Fatalf("AnnualKM = %d — too low for this market; see the comment on the constant", AnnualKM)
	}
}

// ── validation ───────────────────────────────────────────────────────────────

func validDraft() Draft {
	return Draft{
		MakeSlug:     "nissan",
		ModelSlug:    "patrol",
		Year:         2021,
		MileageKM:    95_000,
		RegionalSpec: "gcc",
		PriceMinor:   14_500_000,
		Currency:     "AED",
		Lat:          25.2048,
		Lng:          55.2708,
	}
}

func TestValidDraftPasses(t *testing.T) {
	if issues := validDraft().Validate(time.Now()); len(issues) != 0 {
		t.Fatalf("expected no issues, got %+v", issues)
	}
}

func TestRegionalSpecIsRequired(t *testing.T) {
	// Not defaulted to 'gcc'. Spec swings resale 20–30%, and defaulting would
	// put a value in the listing that the seller never chose and the buyer
	// would read as a statement of fact.
	d := validDraft()
	d.RegionalSpec = ""
	issues := d.Validate(time.Now())
	if !hasField(issues, "regional_spec") {
		t.Fatalf("regional_spec must be required, got %+v", issues)
	}
}

func TestLocationIsRequired(t *testing.T) {
	d := validDraft()
	d.Lat, d.Lng = 0, 0
	if !hasField(d.Validate(time.Now()), "location") {
		t.Fatal("a listing at Null Island is invisible to every buyer query")
	}
}

func TestNextModelYearIsAllowedButNotTheOneAfter(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	d := validDraft()

	d.Year = 2027
	if issues := d.Validate(now); len(issues) != 0 {
		t.Fatalf("cars are sold as next year's model from August, got %+v", issues)
	}

	d.Year = 2028
	if !hasField(d.Validate(now), "year") {
		t.Fatal("two model years ahead is a typo")
	}
}

func TestRadiusIsBounded(t *testing.T) {
	// The geo filter is the product's entire wedge (PLAN-VIDEO-FIRST §10). A
	// seller who sets the radius to the whole country turns it off.
	d := validDraft()
	d.RadiusM = 900_000
	if !hasField(d.Validate(time.Now()), "radius_m") {
		t.Fatal("an unbounded radius defeats the geo filter")
	}
}

func hasField(issues []FieldIssue, field string) bool {
	for _, i := range issues {
		if i.Field == field {
			return true
		}
	}
	return false
}
