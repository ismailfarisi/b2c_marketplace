package vehicles

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/seam/action"
	"github.com/marketplace/api/internal/seam/subject"
)

// Composer is the sell flow for a used car, and it is the whole of what the app
// knows about selling one.
//
// The counterpart to SubjectProvider in this package and the third thing this
// module contributes to the seam: a CTA (cta.go), an overlay (overlay.go), and
// now a form. Registering it is one line in app.BuildRegistries. Nothing in the
// Sell tab, the recorder, the uploader or either client learns that 64,000 is a
// kilometre reading — the same claim the read half makes, tested in the harder
// direction, because a write path has validation and a read path does not.
type Composer struct {
	svc *Service
}

func NewComposer(svc *Service) *Composer { return &Composer{svc: svc} }

// Field keys. Wire values, posted back in DraftInput.Values.
const (
	fieldCar     = "car"
	fieldMileage = "mileage_km"
	fieldPrice   = "price_minor"
	fieldSpec    = "regional_spec"
)

// ClockOpen30d is the only listing term this vertical offers today.
const ClockOpen30d = "open_30d"

// MinPriceMinor floors a listing at AED 1,000.
//
// Not a revenue decision. A car priced at 1 AED is either a typo or bait, and
// both of them reach the buyer as a reel that is very hard not to tap — which
// makes the price floor a feed-quality control rather than a commercial one.
const MinPriceMinor int64 = 100_000

func int64p(v int64) *int64 { return &v }

// Compose declares what a seller must answer before a car can be listed.
//
// # Four fields, not three
//
// The design this implements promises "never more than three, we read the plate
// and mulkiya photo for the rest". There is no OCR in this codebase, so the
// fourth field is `regional_spec` — and it is asked rather than defaulted
// because Draft.Validate refuses to guess it, for a reason worth repeating:
// regional spec swings resale value 20–30% in this market, and defaulting it
// would put a number in the listing that nobody chose.
//
// The honest version of the three-field promise is that this list is served, not
// compiled in. The day a mulkiya reader lands, spec is derivable, this slice
// loses an element, and every installed app asks for three fields on its next
// request. That is the same property the filter row has and it is why both are
// built this way.
func (c *Composer) Compose() subject.Composition {
	return subject.Composition{
		// "Walk around it once" is the single highest-leverage instruction in the
		// flow. A static three-quarter shot is what every classifieds photo
		// already is, and a reel that looks like a photo is a reel the viewer
		// scrolls past — the whole reason this product films cars instead of
		// photographing them.
		Tip:   "Walk around it once. Buyers skip stills.",
		TipAr: "لف حولها مرة واحدة. المشترون يتجاوزون الصور الثابتة.",

		Fields: []subject.Field{{
			Key:   fieldCar,
			Label: "Car", LabelAr: "السيارة",
			Kind:     subject.FieldLookup,
			Required: true,
			// One control for year, make and model, because that is one fact to
			// the person holding the phone: they own a 2021 Nissan Patrol, not a
			// year and a make and a model. Three pickers here is where a
			// classifieds funnel loses people.
			Source:      "/v1/vehicles/models",
			Placeholder: "e.g. 2021 Nissan Patrol",
		}, {
			Key:   fieldMileage,
			Label: "KM", LabelAr: "الكيلومترات",
			Kind:     subject.FieldInteger,
			Required: true,
			Unit:     "km", UnitAr: "كم",
			Min: int64p(0), Max: int64p(2_000_000),
			Placeholder: "64,000",
		}, {
			Key:   fieldPrice,
			Label: "Price", LabelAr: "السعر",
			Kind:     subject.FieldMoney,
			Required: true,
			// AED is fixed rather than resolved from the viewer, because it is a
			// fact about the market this car is in and not a preference of the
			// person selling it. A seller in Dubai pricing in EUR is a listing no
			// buyer here can act on.
			Unit: "AED", UnitAr: "درهم",
			Min:         int64p(MinPriceMinor),
			Placeholder: "189,000",
		}, {
			Key:   fieldSpec,
			Label: "Spec", LabelAr: "المواصفات",
			Kind:     subject.FieldSelect,
			Required: true,
			// The hint is not decoration. Plenty of private sellers do not know
			// what regional spec means, and an unexplained required field is
			// where they close the app.
			Hint:   "Where it was built for. Buyers filter on this.",
			HintAr: "السوق الذي صُنعت له. المشترون يبحثون بها.",
			Options: []subject.Option{
				{Value: "gcc", Label: "GCC", LabelAr: "خليجي"},
				{Value: "us", Label: "American", LabelAr: "أمريكي"},
				{Value: "japanese", Label: "Japanese", LabelAr: "ياباني"},
				{Value: "european", Label: "European", LabelAr: "أوروبي"},
				{Value: "canadian", Label: "Canadian", LabelAr: "كندي"},
				{Value: "chinese", Label: "Chinese", LabelAr: "صيني"},
				{Value: "other", Label: "Other", LabelAr: "أخرى"},
			},
		}},

		// One option, and that is a statement about what exists.
		//
		// An auction needs a bid table, a hold-to-bid CTA and a settlement path;
		// a flash window needs a recurrence rule. Neither is built. Offering
		// either as a card the seller can tap would produce a listing whose
		// promised overlay never appears, and the seller would find out from a
		// buyer.
		//
		// The client renders a lone clock as a statement rather than a chooser —
		// a control with one option cannot change anything, and presenting it as
		// a choice implies the others exist.
		Clocks: []subject.ClockOption{{
			Key:  ClockOpen30d,
			Mode: subject.ModeFreshness,
			// "Open listing" rather than "30 days" as the headline, because the
			// term is what the seller is choosing and the duration is its
			// consequence.
			Label: "Open listing · 30 days", LabelAr: "إعلان مفتوح · ٣٠ يوماً",
			Detail:   "Freshness badge ages: today → 3d → 2w",
			DetailAr: "شارة الحداثة تتقادم: اليوم ← ٣ أيام ← أسبوعان",
			Default:  true,
		}},

		Trust: &subject.TrustUpload{
			Key: "mulkiya",
			// Phrased as what it buys. "Upload your registration card" reads as
			// a demand for a document; this reads as an offer, and it is one —
			// Verified is the difference between a listing buyers message and
			// one they scroll past.
			Label: "Add mulkiya to earn Verified", LabelAr: "أضف الملكية لتحصل على التوثيق",
			Earns: "Verified", EarnsAr: "موثقة",
			Accept: []string{"image/jpeg", "image/png", "application/pdf"},
		},
	}
}

// Draft turns the seller's four answers into a listing row.
//
// It does not publish. The reel's bytes are still going up while this runs — the
// whole point of creating the subject early is that the upload overlaps the rest
// of the flow — and a listing that went live here would be live with an
// unplayable video attached.
func (c *Composer) Draft(ctx context.Context, in subject.DraftInput) (subject.Drafted, error) {
	var fields []httpx.FieldError
	bad := func(key, code, msg string) {
		fields = append(fields, httpx.FieldError{Field: key, Code: code, Message: msg})
	}

	makeSlug, modelSlug, year, err := parseCarValue(in.Values[fieldCar])
	if err != nil {
		bad(fieldCar, "invalid", err.Error())
	}

	mileage, err := parseInt(in.Values[fieldMileage])
	if err != nil {
		bad(fieldMileage, "invalid", "expected a whole number of kilometres")
	}

	price, err := parseInt64(in.Values[fieldPrice])
	if err != nil {
		bad(fieldPrice, "invalid", "expected minor units")
	} else if price < MinPriceMinor {
		bad(fieldPrice, "out_of_range", fmt.Sprintf("at least %d AED", MinPriceMinor/100))
	}

	spec := strings.TrimSpace(in.Values[fieldSpec])

	// The clock is validated even though there is one of them. A client sending
	// `auction_48h` is either running against a newer server or guessing, and
	// silently substituting the default would give the seller a listing term
	// they did not pick — the same failure the composition refuses to make by
	// defaulting spec.
	if in.Clock != "" && in.Clock != ClockOpen30d {
		bad("clock", "unsupported", "this vertical only offers "+ClockOpen30d)
	}

	if len(fields) > 0 {
		return subject.Drafted{}, &httpx.ValidationError{Fields: fields}
	}

	listing, err := c.svc.Create(ctx, CreateListingInput{
		SellerID: in.SellerID,
		Draft: Draft{
			MakeSlug: makeSlug, ModelSlug: modelSlug, Year: year,
			MileageKM:    mileage,
			RegionalSpec: spec,
			PriceMinor:   price,
			Currency:     "AED",
			Lat:          in.Lat, Lng: in.Lng,
			Emirate: in.Region, City: in.Place,
			// Set explicitly rather than left to the column default, so that
			// changing the default later does not silently reprice how far every
			// app-created listing reaches.
			RadiusM: DefaultRadiusM,
		},
	})
	if err != nil {
		return subject.Drafted{}, err
	}

	return subject.Drafted{
		SubjectID: listing.ID,
		// Named here rather than by the sell handler. The handler attaching
		// "inquire_listing" itself would be the same leak in a different file,
		// and it would be wrong the first time a vertical's primary action is
		// something else — a plate auction's is a bid, not a message.
		CTAKind:       string(action.KindInquireListing),
		CTATargetType: string(action.TargetListing),
	}, nil
}

// Publish sends the listing to review.
//
// Not to `active`. Approve is the only path there and it belongs to a moderator,
// which is what makes the "You're live · under review for the Verified badge"
// wording on the last screen accurate rather than optimistic: the reel goes into
// the feed on its own pipeline, and the listing's badge follows a human.
func (c *Composer) Publish(ctx context.Context, sellerID, listingID uuid.UUID) error {
	return c.svc.Submit(ctx, sellerID, listingID)
}

// ─────────────────────────────────────────────────────────────────────────────
// The car lookup
// ─────────────────────────────────────────────────────────────────────────────

// carValue is the opaque string the lookup returns and Draft parses back:
// "nissan/patrol/2021". Opaque to the client, which posts back whatever it was
// given without inspecting it — which is what lets the format change here
// without an app release.
func carValue(makeSlug, modelSlug string, year int) string {
	return fmt.Sprintf("%s/%s/%d", makeSlug, modelSlug, year)
}

func parseCarValue(v string) (makeSlug, modelSlug string, year int, err error) {
	parts := strings.Split(strings.TrimSpace(v), "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" {
		return "", "", 0, fmt.Errorf("pick a car from the list")
	}
	year, err = strconv.Atoi(parts[2])
	if err != nil {
		return "", "", 0, fmt.Errorf("pick a car from the list")
	}
	return parts[0], parts[1], year, nil
}

// LookupYears is how far back the picker offers model years.
//
// Twenty-five, which covers the overwhelming majority of what actually trades
// here and stops the list becoming a scroll. A 1994 Land Cruiser exists and is
// genuinely for sale somewhere; it is also a listing a human should look at, and
// falling off the picker routes it to the web form where a reviewer will.
const LookupYears = 25

// ModelSuggestion is one row of the car picker.
type ModelSuggestion struct {
	Value   string
	Label   string
	LabelAr string
}

// SuggestModels answers the lookup field.
//
// It returns *cars*, not models: "2021 Nissan Patrol" rather than "Patrol" with
// a year wheel beside it. That is one control instead of two on the screen where
// abandonment is highest, and it costs a cross join the seller never sees.
//
// The query is matched against the make and the model, and a four-digit token is
// pulled out and treated as a year. Someone typing "patrol 2021" means one
// specific row and gets it first; someone typing "patrol" gets newest-first,
// which is the right prior for a used-car marketplace where most supply is
// recent.
func (s *Service) SuggestModels(ctx context.Context, q string, limit int) ([]ModelSuggestion, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	text, year := splitYear(q)
	thisYear := s.nowFn().Year()
	// One model year ahead of the calendar is legitimate — cars sell as next
	// year's model from around August — and Draft.Validate already allows it, so
	// the picker must too or the two disagree on the same car.
	maxYear := thisYear + 1
	minYear := maxYear - LookupYears

	if year != 0 {
		if year < minYear || year > maxYear {
			return nil, nil
		}
		minYear, maxYear = year, year
	}

	rows, err := s.pool.Query(ctx, `
		SELECT mk.slug, mk.name, COALESCE(mk.name_ar, mk.name),
		       md.slug, md.name, COALESCE(md.name_ar, md.name)
		  FROM vehicle_models md
		  JOIN vehicle_makes mk ON mk.id = md.make_id
		 WHERE md.is_active AND mk.is_active
		   AND ($1 = '' OR mk.name ILIKE '%' || $1 || '%'
		                OR md.name ILIKE '%' || $1 || '%'
		                OR (mk.name || ' ' || md.name) ILIKE '%' || $1 || '%')
		 ORDER BY mk.sort_order, mk.name, md.name
		 LIMIT $2`, text, limit)
	if err != nil {
		return nil, fmt.Errorf("vehicles: suggest models: %w", err)
	}
	defer rows.Close()

	type model struct {
		makeSlug, makeName, makeAr    string
		modelSlug, modelName, modelAr string
	}
	var models []model
	for rows.Next() {
		var m model
		if err := rows.Scan(&m.makeSlug, &m.makeName, &m.makeAr,
			&m.modelSlug, &m.modelName, &m.modelAr); err != nil {
			return nil, fmt.Errorf("vehicles: scan model: %w", err)
		}
		models = append(models, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Years expanded here rather than in SQL. A generate_series cross join would
	// push the LIMIT onto the wrong side — twenty rows of one model instead of
	// twenty models — and the seller who typed nothing would see the same car
	// twenty times.
	out := make([]ModelSuggestion, 0, limit)
	for y := maxYear; y >= minYear && len(out) < limit; y-- {
		for _, m := range models {
			if len(out) >= limit {
				break
			}
			out = append(out, ModelSuggestion{
				Value: carValue(m.makeSlug, m.modelSlug, y),
				Label: fmt.Sprintf("%d %s %s", y, m.makeName, m.modelName),
				// Year leading in both scripts, matching how every listing in
				// this market is titled — see the note on Summarize.
				LabelAr: fmt.Sprintf("%d %s %s", y, m.makeAr, m.modelAr),
			})
		}
	}
	return out, nil
}

// splitYear pulls a four-digit token out of the query and returns the rest.
//
// "2021 patrol", "patrol 2021" and "patrol" all have to work, because all three
// are what people type. Anything that is four digits and plausibly a year is
// treated as one; a model genuinely named with four digits (a 3008, a 500) has
// three or fewer and is unaffected.
func splitYear(q string) (text string, year int) {
	var kept []string
	for _, tok := range strings.Fields(strings.TrimSpace(q)) {
		if len(tok) == 4 {
			if n, err := strconv.Atoi(tok); err == nil && n >= 1900 && n <= 2100 {
				year = n
				continue
			}
		}
		kept = append(kept, tok)
	}
	return strings.Join(kept, " "), year
}

func parseInt(v string) (int, error) {
	n, err := parseInt64(v)
	return int(n), err
}

// parseInt64 accepts what a phone keypad and a grouped display produce.
//
// Commas and spaces are stripped rather than rejected: the field renders
// "64,000" as the seller types, and some clients post back what they render.
// Refusing that would be a validation error the seller cannot act on, because
// the string they are being shown looks correct.
func parseInt64(v string) (int64, error) {
	cleaned := strings.Map(func(r rune) rune {
		if r == ',' || r == ' ' || r == ' ' {
			return -1
		}
		return r
	}, strings.TrimSpace(v))
	if cleaned == "" {
		return 0, fmt.Errorf("empty")
	}
	return strconv.ParseInt(cleaned, 10, 64)
}
