package property

import (
	"testing"
	"time"
)

var now = time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

func validDraft() Draft {
	return Draft{
		Purpose:      "rent",
		PropertyType: "apartment",
		Bedrooms:     2,
		Bathrooms:    2,
		SizeSqft:     1250,
		Furnishing:   "unfurnished",
		PriceMinor:   14000000, // AED 140,000 a year
		Currency:     "AED",
		RentPeriod:   "yearly",
		PermitNumber: "7128394",
		PermitIssuer: "trakheesi",
		Emirate:      "Dubai",
		Community:    "Dubai Marina",
		Lat:          25.0805,
		Lng:          55.1403,
	}
}

func issueCodes(issues []FieldIssue) map[string]string {
	out := map[string]string{}
	for _, i := range issues {
		out[i.Field] = i.Code
	}
	return out
}

// ── the permit ───────────────────────────────────────────────────────────────

func TestADraftMayExistWithoutAPermit(t *testing.T) {
	// The normal sequence: an agent gathers photos while DET processes the
	// application, which takes days. Requiring the permit to save a draft would
	// make the product unusable for the people it is for.
	d := validDraft()
	d.PermitNumber = ""
	d.PermitIssuer = ""

	if issues := d.Validate(now); len(issues) > 0 {
		t.Fatalf("a permitless draft was rejected: %+v", issues)
	}
}

func TestNothingGoesLiveWithoutAPermit(t *testing.T) {
	// The gate. Publishing an unpermitted advert is an offence committed by the
	// platform, so this is the one check in the vertical that is not a product
	// decision.
	d := validDraft()
	d.PermitNumber = ""
	d.PermitIssuer = ""

	codes := issueCodes(d.ReadyToPublish(now))
	if codes["permit_number"] != "required" {
		t.Error("a listing with no permit number was cleared to publish")
	}
	if codes["permit_issuer"] != "required" {
		t.Error("a listing with no issuing authority was cleared to publish")
	}
}

func TestALapsedPermitIsRefusedAtEveryStage(t *testing.T) {
	d := validDraft()
	expired := now.Add(-24 * time.Hour)
	d.PermitExpiry = &expired

	if issueCodes(d.Validate(now))["permit_expires"] != "expired" {
		t.Error("Validate accepted a lapsed permit")
	}
	if issueCodes(d.ReadyToPublish(now))["permit_expires"] != "expired" {
		t.Error("ReadyToPublish accepted a lapsed permit")
	}
}

func TestAPermitExpiringTodayHasNotLapsedUntilItDoes(t *testing.T) {
	// Boundary. A permit valid "until 31 July" is valid on 31 July, and cutting
	// it off at midnight would withdraw a compliant advert a day early.
	later := now.Add(time.Hour)
	if PermitExpired(&later, now) {
		t.Error("a permit expiring in an hour was treated as lapsed")
	}
	if !PermitExpired(&now, now) {
		t.Error("a permit expiring exactly now was treated as live")
	}
	if PermitExpired(nil, now) {
		t.Error("an absent expiry was treated as lapsed")
	}
}

func TestTheFormatCheckIsASignalNotAVerification(t *testing.T) {
	// It catches a typo. It cannot catch a fabrication that happens to be seven
	// digits, and treating it as verification is what would put an unpermitted
	// advert live with a green tick on it.
	if !WellFormedPermit(IssuerTrakheesi, "7128394") {
		t.Error("a plausible Trakheesi number was rejected")
	}
	if !WellFormedPermit(IssuerTrakheesi, "1111111") {
		t.Error("the check should pass an obviously invented but well-formed number — it is not a register lookup")
	}
	if WellFormedPermit(IssuerTrakheesi, "ABC1234") {
		t.Error("letters accepted in a Trakheesi number")
	}
	if WellFormedPermit(IssuerTrakheesi, "") {
		t.Error("an empty permit is not well formed")
	}
}

func TestAnUnknownIssuerIsRejectedRatherThanGuessed(t *testing.T) {
	d := validDraft()
	d.PermitIssuer = "rera"

	if issueCodes(d.Validate(now))["permit_issuer"] != "invalid" {
		t.Fatal("an unrecognised authority was accepted")
	}
}

// ── the advert ───────────────────────────────────────────────────────────────

func TestAValidDraftPasses(t *testing.T) {
	if issues := validDraft().Validate(now); len(issues) > 0 {
		t.Fatalf("unexpected: %+v", issues)
	}
}

func TestRentNeedsAPeriodAndIsNotDefaulted(t *testing.T) {
	// Yearly is the overwhelming norm here, which is exactly why guessing is
	// dangerous: a monthly rent silently published as yearly is off by twelve.
	d := validDraft()
	d.RentPeriod = ""

	if issueCodes(d.Validate(now))["rent_period"] != "required" {
		t.Fatal("a rental with no period was accepted")
	}
}

func TestASaleCannotCarryARentPeriod(t *testing.T) {
	d := validDraft()
	d.Purpose = "sale"
	d.RentPeriod = "yearly"

	if issueCodes(d.Validate(now))["rent_period"] != "not_applicable" {
		t.Fatal("a sale with a rent period was accepted")
	}
}

func TestAStudioHasNoBedroomAndThatIsFine(t *testing.T) {
	d := validDraft()
	d.PropertyType = "studio"
	d.Bedrooms = 0

	if issues := d.Validate(now); len(issues) > 0 {
		t.Fatalf("a studio was rejected for having no bedroom: %+v", issues)
	}
}

func TestAnApartmentWithNoBedroomIsAFieldLeftBlank(t *testing.T) {
	d := validDraft()
	d.Bedrooms = 0

	if issueCodes(d.Validate(now))["bedrooms"] != "required" {
		t.Fatal("a zero-bedroom apartment was accepted; it should be listed as a studio")
	}
}

func TestSquareMetresTypedIntoASquareFeetBoxAreCaught(t *testing.T) {
	// 115 m² is a normal two-bed. As square feet it is a parking space, and
	// publishing it would put the flat in the studio filter.
	d := validDraft()
	d.SizeSqft = 115

	if issueCodes(d.Validate(now))["size_sqft"] != "out_of_range" {
		t.Fatal("115 sqft was accepted for a two-bedroom apartment")
	}
}

func TestAnAdvertNeedsAPoint(t *testing.T) {
	d := validDraft()
	d.Lat, d.Lng = 0, 0

	if issueCodes(d.Validate(now))["location"] != "required" {
		t.Fatal("Null Island was accepted")
	}
}

// ── the lifecycle ────────────────────────────────────────────────────────────

func TestNothingReachesActiveExceptThroughReview(t *testing.T) {
	for from := range transitions {
		if from == StatusPendingReview || from == StatusPaused {
			continue
		}
		if CanTransition(from, StatusActive) {
			t.Errorf("%s → active bypasses the permit desk", from)
		}
	}
}

func TestALetPropertyDoesNotComeBack(t *testing.T) {
	// Unlike a car marked sold, which vehicles lets you reactivate. A property
	// returning to market needs a new permit — the old one was issued against a
	// tenancy that now exists — so it is a new advert.
	if CanTransition(StatusLetOrSold, StatusActive) {
		t.Fatal("a let property was reactivated under its spent permit")
	}
	if !CanTransition(StatusLetOrSold, StatusRemoved) {
		t.Error("a let property should still be removable")
	}
}

func TestARejectedAdvertCanBeFixedAndResubmitted(t *testing.T) {
	if !CanTransition(StatusRejected, StatusPendingReview) {
		t.Fatal("a rejected advert is a dead end")
	}
}

func TestRemovedIsTerminal(t *testing.T) {
	if len(transitions[StatusRemoved]) != 0 {
		t.Fatalf("removed leads to %v", transitions[StatusRemoved])
	}
}

func TestPropertyAgesSlowerThanACar(t *testing.T) {
	// The seam's own Horizon comment uses this exact pair as the example of why
	// the vertical picks the number: three days is fresh for a car and brand new
	// for a home.
	if FreshnessHorizon != 60*24*time.Hour {
		t.Fatalf("horizon is %s", FreshnessHorizon)
	}
	if FreshnessHorizon <= 30*24*time.Hour {
		t.Error("property should age slower than the 30-day vehicle horizon")
	}
}

func TestLandHasNoHabitableFloor(t *testing.T) {
	// The stricter floor is about people typing metres for a home. A small plot,
	// a shop or a storage unit is legitimately under it, and applying the
	// habitable minimum to land would reject real inventory.
	d := validDraft()
	d.PropertyType = "land"
	d.Bedrooms, d.Bathrooms = 0, 0
	d.SizeSqft = 150

	if issues := d.Validate(now); len(issues) > 0 {
		t.Fatalf("a 150 sqft plot was rejected: %+v", issues)
	}
}

func TestTheSmallestRealStudioStillPasses(t *testing.T) {
	// Around 300 sqft is the bottom of the actual market. A floor that rejected
	// those would delete a real segment to catch a typo.
	d := validDraft()
	d.PropertyType = "studio"
	d.Bedrooms = 0
	d.SizeSqft = 320

	if issues := d.Validate(now); len(issues) > 0 {
		t.Fatalf("a 320 sqft studio was rejected: %+v", issues)
	}
}
