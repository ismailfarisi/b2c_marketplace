// Package property is the real-estate vertical.
//
// # Why it is not vehicles with different columns
//
// The two look alike from a distance and differ in the one place that matters.
// A used-car advert is a private transaction the platform hosts. A property
// advert in this market is a *regulated publication*: Dubai requires a
// Trakheesi permit obtained per-listing by a licensed brokerage, Abu Dhabi
// requires DARI, and the permit number has to appear in the advert itself.
// Publishing without one is an offence committed by whoever published it —
// which is the platform, not only the advertiser.
//
// That single fact sets everything else here. The permit is a precondition of
// going live rather than a trust badge that improves a listing (contrast the
// mulkiya upload in vehicles, which is genuinely optional). It is enforced by a
// CHECK constraint as well as by this package, because a code path can be
// bypassed by an admin tool and a constraint cannot. And the review queue is a
// different job for a different reviewer: checking a number against a register,
// not judging whether a photo looks honest.
package property

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
	// StatusLetOrSold is one state, not two.
	//
	// A listing is for sale or for rent, never both, so `purpose` already says
	// which happened and a second status would be a fact the row can derive.
	StatusLetOrSold Status = "let_or_sold"
	StatusExpired   Status = "expired"
	StatusRejected  Status = "rejected"
	StatusRemoved   Status = "removed"
)

// transitions is the whole lifecycle, written once — the same choice vehicles
// and reels made, for the same reason.
//
// One difference from vehicles is worth naming: there is no path from
// `let_or_sold` back to `active`. A car marked sold that falls through is
// routine and vehicles allows the round trip. A property that was let and comes
// back on the market needs a *new permit* — the old one was issued against a
// tenancy that now exists — so it is a new advert, and quietly reactivating the
// old row would republish an advert under a spent permit.
var transitions = map[Status][]Status{
	StatusDraft:         {StatusPendingReview, StatusRemoved},
	StatusPendingReview: {StatusActive, StatusRejected, StatusRemoved},
	StatusActive:        {StatusPaused, StatusLetOrSold, StatusExpired, StatusPendingReview, StatusRemoved},
	StatusPaused:        {StatusActive, StatusLetOrSold, StatusExpired, StatusRemoved},
	StatusExpired:       {StatusPendingReview, StatusRemoved},
	StatusRejected:      {StatusPendingReview, StatusRemoved},
	StatusLetOrSold:     {StatusRemoved},
	StatusRemoved:       {},
}

func CanTransition(from, to Status) bool {
	for _, allowed := range transitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

func (s Status) Live() bool { return s == StatusActive }

// ─────────────────────────────────────────────────────────────────────────────
// The permit
// ─────────────────────────────────────────────────────────────────────────────

type PermitIssuer string

const (
	// Dubai. Issued by DET against a specific listing, and the number must be
	// displayed in the advert.
	IssuerTrakheesi PermitIssuer = "trakheesi"
	// Abu Dhabi.
	IssuerDARI PermitIssuer = "dari"
	// The northern emirates, whose regimes differ and are not modelled
	// individually. A permit is still required and still displayed; what this
	// codebase cannot do is validate the format, so it does not pretend to.
	IssuerOther PermitIssuer = "other"
)

var permitIssuers = map[PermitIssuer]bool{
	IssuerTrakheesi: true, IssuerDARI: true, IssuerOther: true,
}

// WellFormedPermit checks shape only, and only where the shape is known.
//
// It is emphatically **not** a validity check. A Trakheesi number is verified
// against DET's register, which this system cannot reach — so what this catches
// is a typo or an obvious fabrication, and everything it passes still goes to a
// human. Treating a format check as verification is exactly the mistake that
// would put an unpermitted advert live with a green tick on it.
//
// The same distinction vehicles draws for a VIN check digit: a signal for a
// reviewer, never a gate.
func WellFormedPermit(issuer PermitIssuer, number string) bool {
	n := strings.TrimSpace(number)
	if n == "" {
		return false
	}
	switch issuer {
	case IssuerTrakheesi:
		// Seven digits in practice. Length only, because the leading digits
		// encode the issuing office and that mapping is not public.
		return len(n) >= 6 && len(n) <= 12 && allDigits(n)
	case IssuerDARI:
		return len(n) >= 6 && len(n) <= 20
	default:
		return len(n) >= 4
	}
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// PermitExpired reports whether a permit has lapsed.
//
// Checked at publish and again by the expiry sweep, because a permit that
// expires while the advert is live makes the *platform* non-compliant from that
// moment — the obligation is continuous, not a one-time check at submission.
func PermitExpired(expires *time.Time, now time.Time) bool {
	return expires != nil && !expires.After(now)
}

// ─────────────────────────────────────────────────────────────────────────────
// The advert
// ─────────────────────────────────────────────────────────────────────────────

type Purpose string

const (
	PurposeSale Purpose = "sale"
	PurposeRent Purpose = "rent"
)

var propertyTypes = map[string]bool{
	"apartment": true, "villa": true, "townhouse": true, "penthouse": true,
	"studio": true, "office": true, "shop": true, "warehouse": true, "land": true,
}

var rentPeriods = map[string]bool{
	"yearly": true, "monthly": true, "weekly": true, "daily": true,
}

var furnishings = map[string]bool{
	"furnished": true, "semi_furnished": true, "unfurnished": true,
}

// Draft is what an advertiser submits.
//
// Everything the database CHECKs is re-checked here so the error is a
// field-level 422 the client can act on rather than a constraint violation it
// cannot.
type Draft struct {
	Purpose      string
	PropertyType string
	Bedrooms     int
	Bathrooms    int
	SizeSqft     int
	Furnishing   string

	PriceMinor int64
	Currency   string
	RentPeriod string

	PermitNumber string
	PermitIssuer string
	PermitExpiry *time.Time
	AgentBRN     string

	Title       string
	Description string
	Emirate     string
	Community   string
	Tower       string
	Lat, Lng    float64
}

// FieldIssue is one validation failure, matching vehicles.FieldIssue so the API
// layer turns both into the same problem+json shape.
type FieldIssue struct {
	Field   string
	Code    string
	Message string
}

// Size floors, and there are two of them because one was not enough.
//
// MinSizeSqft is the absolute floor and applies to land, warehouses and shops,
// which are legitimately any size at all.
//
// MinHabitableSqft is the one that matters. The mistake this catches is not
// somebody advertising a tiny flat — it is square *metres* typed into a
// square-feet box, and 115 m² is a perfectly ordinary two-bedroom. A single
// hundred-foot floor passes 115 happily, publishes a family apartment into the
// studio filter, and prices it at eleven thousand dirhams a square foot.
//
// 250 sits below the smallest studio that actually trades here — Dubai's are
// around 300 — and above the largest number anyone would produce by typing
// metres for a home. A villa in metres is 400+, which clears it; but a villa
// entered in metres is also off by a factor of ten on price per foot, which the
// review desk sees.
const (
	MinSizeSqft      = 100
	MinHabitableSqft = 250
	MaxSizeSqft      = 1_000_000
)

// habitable reports whether a property type is somewhere people live, and so
// whether the stricter floor applies.
func habitable(propertyType string) bool {
	switch propertyType {
	case "apartment", "villa", "townhouse", "penthouse", "studio":
		return true
	}
	return false
}

// Validate checks a draft without touching the database.
//
// The permit is checked here only for *shape*. Whether it is required at all
// depends on where the listing is going: a draft may exist without one, and the
// service enforces its presence at submit. Requiring it here would stop an
// agent saving progress while they wait for DET, which is the normal sequence
// and takes days.
func (d Draft) Validate(now time.Time) []FieldIssue {
	var issues []FieldIssue
	add := func(field, code, msg string) {
		issues = append(issues, FieldIssue{Field: field, Code: code, Message: msg})
	}

	switch Purpose(d.Purpose) {
	case PurposeSale:
		if d.RentPeriod != "" {
			add("rent_period", "not_applicable", "a sale has no rent period")
		}
	case PurposeRent:
		if !rentPeriods[d.RentPeriod] {
			// Not defaulted to yearly, even though yearly is the overwhelming
			// norm here. A rent quoted for the wrong period is off by twelve,
			// and guessing it would put a number in the advert nobody chose —
			// the same objection vehicles raises about regional spec.
			add("rent_period", "required", "one of yearly, monthly, weekly, daily")
		}
	default:
		add("purpose", "required", "sale or rent")
	}

	if !propertyTypes[d.PropertyType] {
		add("property_type", "required", "")
	}
	if d.Bedrooms < 0 || d.Bedrooms > 20 {
		add("bedrooms", "out_of_range", "expected 0–20")
	}
	if d.Bathrooms < 0 || d.Bathrooms > 20 {
		add("bathrooms", "out_of_range", "expected 0–20")
	}
	// A studio has no bedroom and that is the point of the type. Anything else
	// with zero bedrooms is a field left blank.
	if d.Bedrooms == 0 && d.PropertyType != "studio" && d.PropertyType != "land" &&
		d.PropertyType != "office" && d.PropertyType != "shop" && d.PropertyType != "warehouse" {
		add("bedrooms", "required", "a home needs at least one bedroom, or list it as a studio")
	}
	floor := MinSizeSqft
	if habitable(d.PropertyType) {
		floor = MinHabitableSqft
	}
	if d.SizeSqft < floor || d.SizeSqft > MaxSizeSqft {
		add("size_sqft", "out_of_range",
			fmt.Sprintf("expected %d–%d sqft — check you are not entering square metres", floor, MaxSizeSqft))
	}
	if d.Furnishing != "" && !furnishings[d.Furnishing] {
		add("furnishing", "invalid", "")
	}

	if d.PriceMinor <= 0 {
		add("price_minor", "required", "")
	}
	if d.Currency != "" && len(d.Currency) != 3 {
		add("currency", "invalid", "ISO 4217")
	}

	if d.PermitIssuer != "" && !permitIssuers[PermitIssuer(d.PermitIssuer)] {
		add("permit_issuer", "invalid", "one of trakheesi, dari, other")
	}
	if d.PermitNumber != "" && d.PermitIssuer != "" &&
		!WellFormedPermit(PermitIssuer(d.PermitIssuer), d.PermitNumber) {
		add("permit_number", "malformed", "check the number against your permit")
	}
	if PermitExpired(d.PermitExpiry, now) {
		add("permit_expires", "expired", "this permit has lapsed")
	}

	if d.Lat == 0 && d.Lng == 0 {
		add("location", "required", "an advert needs a point")
	}
	if d.Lat < -90 || d.Lat > 90 {
		add("lat", "out_of_range", "")
	}
	if d.Lng < -180 || d.Lng > 180 {
		add("lng", "out_of_range", "")
	}

	return issues
}

// ReadyToPublish is the gate the service applies at submit.
//
// Separate from Validate because they answer different questions at different
// moments: Validate asks "is this a coherent draft", this asks "may this be
// advertised". The second is the regulated one.
func (d Draft) ReadyToPublish(now time.Time) []FieldIssue {
	issues := d.Validate(now)
	if d.PermitNumber == "" {
		issues = append(issues, FieldIssue{
			Field: "permit_number", Code: "required",
			Message: "a permit number is required before a property advert can go live",
		})
	}
	if d.PermitIssuer == "" {
		issues = append(issues, FieldIssue{
			Field: "permit_issuer", Code: "required",
			Message: "which authority issued the permit",
		})
	}
	return issues
}

// ListingTTL is how long a live advert stands before the sweeper expires it.
//
// Sixty days against the classifieds' thirty. Property moves slower and a
// two-month-old advert is still plausibly current, where a two-month-old car
// listing usually is not. It is also bounded by the permit: whichever comes
// first wins, because a live advert under a lapsed permit is a compliance
// failure and not merely stale content.
const ListingTTL = 60 * 24 * time.Hour

// FreshnessHorizon is the span an advert's age is drawn against.
//
// Sixty days, matching the term, so the bar reads as "how much of this advert's
// life is left". Twice the vehicle horizon, because the same three days that
// make a car listing look current make a property listing look brand new — the
// seam's own comment on Horizon uses exactly this pair as the example of why the
// vertical picks the number.
const FreshnessHorizon = ListingTTL
