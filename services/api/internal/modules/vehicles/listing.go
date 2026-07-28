// Package vehicles is the classifieds engine.
//
// It is deliberately not a vertical in the `service_types` sense. A vertical
// binds four strategies — item selection, fulfillment pattern, capacity
// resource, pricing — and a used car has none of them: nothing is fulfilled,
// nothing is scheduled, no capacity is drawn, and the price is whatever two
// people agree at an RTA counter the platform never sees.
//
// What it does bind is the *upstream* seam. One action handler, one
// (kind, target_type) pair, and the feed gains a second supply engine without
// learning what a car is. That claim is the phase's gate; see
// docs/PHASE-6-VEHICLES.md §2.
package vehicles

import (
	"fmt"
	"strings"
	"time"
)

type Status string

const (
	StatusDraft         Status = "draft"
	StatusPendingReview Status = "pending_review"
	StatusActive        Status = "active"
	StatusPaused        Status = "paused"
	StatusSold          Status = "sold"
	StatusExpired       Status = "expired"
	StatusRejected      Status = "rejected"
	StatusRemoved       Status = "removed"
)

// transitions is the whole lifecycle, written down once.
//
// Same choice as the reel state machine in Phase 2 and for the same reason: a
// status column with no declared graph acquires an illegal transition the first
// time someone writes an UPDATE in a hurry, and nothing catches it.
var transitions = map[Status][]Status{
	StatusDraft:         {StatusPendingReview, StatusRemoved},
	StatusPendingReview: {StatusActive, StatusRejected, StatusRemoved},
	// A listing can go back to review from active: an edit to price or mileage
	// is exactly where a bait-and-switch happens, so a material edit re-enters
	// the queue rather than publishing silently.
	StatusActive:   {StatusPaused, StatusSold, StatusExpired, StatusPendingReview, StatusRemoved},
	StatusPaused:   {StatusActive, StatusSold, StatusExpired, StatusRemoved},
	StatusExpired:  {StatusPendingReview, StatusRemoved},
	StatusRejected: {StatusPendingReview, StatusRemoved},
	// Sold is not terminal. Sellers mark cars sold that then fall through, and
	// forcing them to re-create the listing loses the photos, the reel and the
	// history — which is precisely the history the fraud detectors need.
	StatusSold:    {StatusActive, StatusRemoved},
	StatusRemoved: {},
}

func CanTransition(from, to Status) bool {
	for _, allowed := range transitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// Live reports whether a listing is visible to buyers.
func (s Status) Live() bool { return s == StatusActive }

// ─────────────────────────────────────────────────────────────────────────────
// VIN
// ─────────────────────────────────────────────────────────────────────────────

// vinTransliteration is ISO 3779. I, O and Q are absent from the alphabet
// entirely so they can never be confused with 1 and 0 on a windscreen.
var vinTransliteration = map[byte]int{
	'A': 1, 'B': 2, 'C': 3, 'D': 4, 'E': 5, 'F': 6, 'G': 7, 'H': 8,
	'J': 1, 'K': 2, 'L': 3, 'M': 4, 'N': 5, 'P': 7, 'R': 9,
	'S': 2, 'T': 3, 'U': 4, 'V': 5, 'W': 6, 'X': 7, 'Y': 8, 'Z': 9,
	'0': 0, '1': 1, '2': 2, '3': 3, '4': 4, '5': 5, '6': 6, '7': 7, '8': 8, '9': 9,
}

var vinWeights = [17]int{8, 7, 6, 5, 4, 3, 2, 10, 0, 9, 8, 7, 6, 5, 4, 3, 2}

// NormaliseVIN uppercases and strips the separators people type.
func NormaliseVIN(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(raw)) {
		if r == ' ' || r == '-' || r == '.' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// WellFormedVIN checks length and alphabet only.
func WellFormedVIN(vin string) bool {
	if len(vin) != 17 {
		return false
	}
	for i := 0; i < len(vin); i++ {
		if _, ok := vinTransliteration[vin[i]]; !ok {
			return false
		}
	}
	return true
}

// CheckDigitValid verifies position 9 against ISO 3779.
//
// It is a *signal*, never a validation error, and the distinction is the whole
// reason this function is separate from WellFormedVIN. The check digit is
// mandatory for vehicles built for North America and optional everywhere else,
// so a genuine GCC-spec Land Cruiser and a genuine Japanese import both fail it
// routinely. Refusing those listings would delete most of the market.
//
// What it is good for is the other direction: a *fabricated* VIN — someone
// typing seventeen plausible characters to make a listing look documented —
// passes the check digit with probability about 1 in 11. Combined with
// regional_spec = 'us', a failure here is strong evidence.
func CheckDigitValid(vin string) bool {
	if !WellFormedVIN(vin) {
		return false
	}
	sum := 0
	for i := 0; i < 17; i++ {
		sum += vinTransliteration[vin[i]] * vinWeights[i]
	}
	remainder := sum % 11
	expected := byte('0' + remainder)
	if remainder == 10 {
		expected = 'X'
	}
	return vin[8] == expected
}

// ─────────────────────────────────────────────────────────────────────────────
// Plausibility
// ─────────────────────────────────────────────────────────────────────────────

// AnnualKM is the reference distance a car covers in a year in this market.
//
// Higher than European figures on purpose: long commutes, cheap fuel, and
// intercity driving that would be a flight elsewhere. Using a European
// benchmark here would flag half the honest listings in Dubai as rolled back.
const AnnualKM = 22_000

// MileageBand classifies a listing's odometer against its age.
type MileageBand string

const (
	BandNormal      MileageBand = "normal"
	BandHigh        MileageBand = "high"
	BandImplausible MileageBand = "implausible"
	// BandSuspiciouslyLow is the odometer-rollback shape. It is also the shape
	// of a genuinely garaged car, which is why it produces a signal for a human
	// rather than a rejection.
	BandSuspiciouslyLow MileageBand = "suspiciously_low"
)

// ClassifyMileage compares an odometer reading against the car's age.
//
// Deliberately generous at both ends. The purpose is to surface the extremes to
// a reviewer, not to argue with sellers about whether 180,000 km is a lot.
func ClassifyMileage(year int, mileageKm int, now time.Time) MileageBand {
	age := now.Year() - year
	if age < 0 {
		// A model year ahead of the calendar year is normal — cars are sold as
		// next year's model from around August.
		age = 0
	}
	// A brand-new car legitimately reads a few hundred km, so the first year is
	// exempt from the low-end test entirely.
	if age == 0 {
		if mileageKm > AnnualKM*2 {
			return BandHigh
		}
		return BandNormal
	}

	expected := age * AnnualKM
	switch {
	case mileageKm > expected*3:
		return BandImplausible
	case mileageKm > expected*3/2:
		return BandHigh
	case age >= 3 && mileageKm*4 < expected:
		// Under a quarter of expected, on a car at least three years old.
		return BandSuspiciouslyLow
	default:
		return BandNormal
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Validation
// ─────────────────────────────────────────────────────────────────────────────

// Draft is what a seller submits. Everything the database CHECKs is re-checked
// here so the error is a field-level 422 rather than a constraint violation the
// client cannot act on.
type Draft struct {
	MakeSlug     string
	ModelSlug    string
	Year         int
	MileageKM    int
	RegionalSpec string
	PriceMinor   int64
	Currency     string
	VIN          string
	Transmission string
	FuelType     string
	BodyType     string
	Title        string
	Description  string
	Lat, Lng     float64
	RadiusM      int
}

var regionalSpecs = map[string]bool{
	"gcc": true, "us": true, "japanese": true, "european": true,
	"canadian": true, "chinese": true, "other": true,
}

var transmissions = map[string]bool{
	"automatic": true, "manual": true, "cvt": true, "dct": true, "other": true,
}

var fuelTypes = map[string]bool{
	"petrol": true, "diesel": true, "hybrid": true, "plug_in_hybrid": true, "electric": true,
}

var bodyTypes = map[string]bool{
	"sedan": true, "suv": true, "coupe": true, "hatchback": true, "pickup": true,
	"van": true, "convertible": true, "wagon": true, "crossover": true, "other": true,
}

// FieldIssue is one validation failure. The API layer turns these into the
// standard problem+json field errors.
type FieldIssue struct {
	Field   string
	Code    string
	Message string
}

// MaxRadiusM caps how far a seller can claim their car reaches.
//
// A car is not delivered, so "radius" here means "how far will a buyer travel
// to see it" — real, but bounded. Without a cap every seller sets it to the
// whole country and the geo filter, which is the product's entire wedge
// (PLAN-VIDEO-FIRST §10), stops filtering anything.
const (
	MinRadiusM     = 5_000
	MaxRadiusM     = 150_000
	DefaultRadiusM = 50_000
)

// Validate checks a draft. It does not touch the database: taxonomy existence
// and quota are the repository's job, and mixing the two makes the pure half
// untestable.
func (d Draft) Validate(now time.Time) []FieldIssue {
	var issues []FieldIssue
	add := func(field, code, msg string) {
		issues = append(issues, FieldIssue{Field: field, Code: code, Message: msg})
	}

	if d.MakeSlug == "" {
		add("make", "required", "")
	}
	if d.ModelSlug == "" {
		add("model", "required", "")
	}

	// One model year ahead of the calendar is legitimate; two is a typo.
	maxYear := now.Year() + 1
	if d.Year < 1950 || d.Year > maxYear {
		add("year", "out_of_range", fmt.Sprintf("expected 1950–%d", maxYear))
	}
	if d.MileageKM < 0 || d.MileageKM > 2_000_000 {
		add("mileage_km", "out_of_range", "expected 0–2,000,000")
	}
	if !regionalSpecs[d.RegionalSpec] {
		// Not defaulted. Regional spec swings resale value 20–30% in this market
		// (SCOPE-EXTENSIONS §3) and guessing it on the seller's behalf would put
		// a number in the listing that nobody chose.
		add("regional_spec", "required", "one of gcc, us, japanese, european, canadian, chinese, other")
	}
	if d.PriceMinor <= 0 {
		add("price_minor", "required", "")
	}
	if d.Currency != "" && len(d.Currency) != 3 {
		add("currency", "invalid", "ISO 4217")
	}

	if d.VIN != "" && !WellFormedVIN(NormaliseVIN(d.VIN)) {
		add("vin", "invalid", "17 characters, excluding I, O and Q")
	}
	if d.Transmission != "" && !transmissions[d.Transmission] {
		add("transmission", "invalid", "")
	}
	if d.FuelType != "" && !fuelTypes[d.FuelType] {
		add("fuel_type", "invalid", "")
	}
	if d.BodyType != "" && !bodyTypes[d.BodyType] {
		add("body_type", "invalid", "")
	}

	if d.Lat == 0 && d.Lng == 0 {
		// Null Island. A listing with no location is invisible to every buyer
		// query, and publishing it would look like a search bug.
		add("location", "required", "a listing needs a point")
	}
	if d.Lat < -90 || d.Lat > 90 {
		add("lat", "out_of_range", "")
	}
	if d.Lng < -180 || d.Lng > 180 {
		add("lng", "out_of_range", "")
	}
	if d.RadiusM != 0 && (d.RadiusM < MinRadiusM || d.RadiusM > MaxRadiusM) {
		add("radius_m", "out_of_range", fmt.Sprintf("expected %d–%d", MinRadiusM, MaxRadiusM))
	}

	return issues
}

// ListingTTL is how long an active listing stays up before the sweeper expires
// it.
//
// Expiry is not housekeeping. A classifieds site whose listings never expire
// fills with sold cars, buyers message sellers about cars that went three
// months ago, and the sellers stop answering — which reads to the next buyer as
// a dead marketplace. The package's duration_days overrides this per purchase.
const ListingTTL = 30 * 24 * time.Hour

// OfferTTL bounds a negotiation. A pending offer with no end is a buyer who
// thinks they have a claim on the car and a seller who thinks they do not.
const OfferTTL = 7 * 24 * time.Hour
