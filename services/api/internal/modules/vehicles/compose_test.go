package vehicles

import (
	"testing"

	"github.com/marketplace/api/internal/seam/subject"
)

func TestTheCarComposerIsWellFormed(t *testing.T) {
	// The same check RegisterComposer runs at boot. Here so a malformed
	// composition fails in CI rather than on the process that would have
	// panicked in front of a seller.
	if err := (&Composer{}).Compose().Validate(); err != nil {
		t.Fatalf("composition is malformed: %v", err)
	}
}

func TestTheCarFormStaysWithinTheFieldCap(t *testing.T) {
	// The cap is a product constraint and the pressure to exceed it is
	// constant. This test is the thing that argues back.
	fields := (&Composer{}).Compose().Fields
	if len(fields) > subject.MaxFields {
		t.Fatalf("%d fields exceeds the cap of %d", len(fields), subject.MaxFields)
	}
}

func TestEverySpecOptionIsOneTheDraftValidatorAccepts(t *testing.T) {
	// The select's options and Draft.Validate's allow-list are two lists of the
	// same seven strings in two files. Without this they drift, and the symptom
	// is a seller picking a spec from a dropdown and being told it is invalid.
	for _, f := range (&Composer{}).Compose().Fields {
		if f.Key != fieldSpec {
			continue
		}
		if len(f.Options) != len(regionalSpecs) {
			t.Fatalf("spec offers %d options but the validator accepts %d",
				len(f.Options), len(regionalSpecs))
		}
		for _, o := range f.Options {
			if !regionalSpecs[o.Value] {
				t.Errorf("spec option %q is not accepted by Draft.Validate", o.Value)
			}
		}
	}
}

func TestTheClockOfferedIsTheModeTheProviderActuallyProjects(t *testing.T) {
	// A seller picking "Open listing · 30 days" is promised a freshness badge.
	// SubjectProvider.Project is what decides whether they get one, and if the
	// two ever disagree the seller is sold an overlay that never appears.
	clock := (&Composer{}).Compose().DefaultClock()
	if clock.Mode != subject.ModeFreshness {
		t.Fatalf("the default clock is %q but the provider projects freshness", clock.Mode)
	}
}

func TestTheTrustBadgePromisedIsTheBadgeSummarizeProduces(t *testing.T) {
	// "Add mulkiya to earn Verified" is a promise about a word that appears on
	// the overlay. Summarize composes "USED CAR · VERIFIED" from an inspection
	// report; promising a badge nothing produces is worse than not asking.
	trust := (&Composer{}).Compose().Trust
	if trust == nil {
		t.Fatal("expected a trust upload")
	}
	if trust.Earns != "Verified" {
		t.Fatalf("earns %q, but Summarize badges VERIFIED", trust.Earns)
	}
}

func TestTheCarValueRoundTrips(t *testing.T) {
	// The picker writes it and Draft reads it. They are the only two places
	// that know the format, which is what lets it change without an app
	// release — provided they stay in agreement.
	value := carValue("nissan", "patrol", 2021)

	mk, md, year, err := parseCarValue(value)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	if mk != "nissan" || md != "patrol" || year != 2021 {
		t.Fatalf("got %q/%q/%d", mk, md, year)
	}
}

func TestAHalfAnsweredCarIsRefused(t *testing.T) {
	// A client that posts the label instead of the value, or an empty box, must
	// produce a field error rather than a listing for a car with no make.
	for _, v := range []string{"", "nissan", "nissan/patrol", "2021 Nissan Patrol", "nissan/patrol/soon"} {
		if _, _, _, err := parseCarValue(v); err == nil {
			t.Errorf("%q was accepted as a car", v)
		}
	}
}

func TestTheYearIsFoundWhereverItIsTyped(t *testing.T) {
	// All three of these are what people type into a car search, and the
	// picker has to behave the same for each.
	for _, tc := range []struct {
		in       string
		wantText string
		wantYear int
	}{
		{"patrol", "patrol", 0},
		{"2021 patrol", "patrol", 2021},
		{"patrol 2021", "patrol", 2021},
		{"nissan patrol 2021", "nissan patrol", 2021},
		{"", "", 0},
		// A model genuinely named with digits keeps its name: four digits are
		// only read as a year, and these are three.
		{"500", "500", 0},
		{"3008", "3008", 0},
	} {
		text, year := splitYear(tc.in)
		if text != tc.wantText || year != tc.wantYear {
			t.Errorf("%q → (%q, %d), want (%q, %d)", tc.in, text, year, tc.wantText, tc.wantYear)
		}
	}
}

func TestAThreeThousandAndEightIsAPeugeotNotAYear(t *testing.T) {
	// 3008 is four digits and a real model. It is excluded by the year range
	// rather than by the digit count, which is the part that is easy to get
	// wrong when someone later widens the range.
	text, year := splitYear("3008")
	if year != 0 || text != "3008" {
		t.Fatalf("3008 read as year %d with text %q", year, text)
	}
}

func TestTheKeypadsGroupingSeparatorsAreAccepted(t *testing.T) {
	// The mileage box renders "64,000" as the seller types. A client that posts
	// back what it renders must not be told its own display is invalid.
	for _, in := range []string{"64000", "64,000", "64 000", " 64000 "} {
		got, err := parseInt64(in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if got != 64000 {
			t.Errorf("%q → %d", in, got)
		}
	}
}

func TestAnEmptyNumberIsAnErrorRatherThanZero(t *testing.T) {
	// Zero is a legal mileage and an illegal price. Returning it for an empty
	// box would list a car at nothing and pass the range check on the way.
	if _, err := parseInt64("  "); err == nil {
		t.Fatal("an empty box parsed as a number")
	}
}
