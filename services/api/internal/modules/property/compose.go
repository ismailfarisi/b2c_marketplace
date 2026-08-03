package property

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

// Composer is the sell flow for a property advert.
//
// The interesting comparison is with vehicles. That composer asks four questions
// and the fourth exists only because there is no OCR yet. This one asks four and
// every one of them is load-bearing — and it still cannot ask everything the
// advert needs, because the permit is a document the advertiser does not have
// when they are standing in the flat with their phone.
//
// That shapes the flow. The video and the facts are captured on site; the permit
// arrives days later from a brokerage. So a property advert is *drafted* from
// the app and *submitted* once the number comes back, which is why Draft and
// Publish being separate calls on this seam matters more here than anywhere
// else.
type Composer struct {
	svc *Service
}

func NewComposer(svc *Service) *Composer { return &Composer{svc: svc} }

const (
	fieldPurpose  = "purpose"
	fieldKind     = "property_type"
	fieldBeds     = "bedrooms"
	fieldSize     = "size_sqft"
	fieldPrice    = "price_minor"
	fieldPermit   = "permit_number"
	fieldRentTerm = "rent_period"
)

// ClockOpen60d is the only listing term. Sixty days, matching ListingTTL.
const ClockOpen60d = "open_60d"

func int64p(v int64) *int64 { return &v }

// Compose declares what an advertiser must answer on site.
//
// # Four fields, and the permit is not one of them
//
// The seam caps a composition at four, and property has more than four facts
// that matter — bathrooms, furnishing, tower, community, the permit, the BRN.
// Cutting to four is not a compromise on data quality; it is a bet about where
// the data comes from. Somebody standing in an empty flat with a phone knows
// what they are selling, how big it is and what it costs. They do not have the
// Trakheesi number in their hand, and asking for it here would stop the flow
// dead for every advert.
//
// So the permit is a *trust upload* in the same slot the mulkiya occupies for
// cars — with one difference that the client cannot see and does not need to:
// skipping it produces a draft that can never go live, where skipping a mulkiya
// produces a listing that merely earns less. Service.Submit is where that
// difference is enforced.
func (c *Composer) Compose() subject.Composition {
	return subject.Composition{
		// Property is the one vertical where the walkthrough *is* the product.
		// A still of a living room tells a viewer nothing about whether the
		// bedroom is next to the lift shaft.
		Tip:   "Walk the whole unit, slowly. Show the view from the window.",
		TipAr: "امشِ في الوحدة كاملة ببطء. أظهر الإطلالة من النافذة.",

		Fields: []subject.Field{{
			Key:   fieldPurpose,
			Label: "Sale or rent", LabelAr: "بيع أم إيجار",
			Kind:     subject.FieldSelect,
			Required: true,
			Options: []subject.Option{
				{Value: "rent", Label: "For rent", LabelAr: "للإيجار"},
				{Value: "sale", Label: "For sale", LabelAr: "للبيع"},
			},
			// First, because it changes what the price means. A number with no
			// purpose attached is the one mistake in this vertical that is off
			// by three orders of magnitude.
			Hint:   "Rent is quoted per year here.",
			HintAr: "الإيجار يُحسب سنوياً هنا.",
		}, {
			Key:   fieldKind,
			Label: "Property", LabelAr: "نوع العقار",
			Kind:     subject.FieldSelect,
			Required: true,
			Options: []subject.Option{
				{Value: "apartment", Label: "Apartment", LabelAr: "شقة"},
				{Value: "villa", Label: "Villa", LabelAr: "فيلا"},
				{Value: "townhouse", Label: "Townhouse", LabelAr: "تاون هاوس"},
				{Value: "studio", Label: "Studio", LabelAr: "استوديو"},
				{Value: "penthouse", Label: "Penthouse", LabelAr: "بنتهاوس"},
				{Value: "office", Label: "Office", LabelAr: "مكتب"},
				{Value: "shop", Label: "Shop", LabelAr: "محل"},
				{Value: "warehouse", Label: "Warehouse", LabelAr: "مستودع"},
				{Value: "land", Label: "Land", LabelAr: "أرض"},
			},
		}, {
			Key:   fieldSize,
			Label: "Size", LabelAr: "المساحة",
			Kind:     subject.FieldInteger,
			Required: true,
			Unit:     "sqft", UnitAr: "قدم²",
			Min: int64p(MinSizeSqft), Max: int64p(MaxSizeSqft),
			// Said out loud, because it is the mistake. Everyone in this market
			// thinks in feet and half the source documents are in metres.
			Hint:        "Square feet, not metres.",
			HintAr:      "بالقدم المربع وليس المتر.",
			Placeholder: "1,250",
		}, {
			Key:   fieldPrice,
			Label: "Price", LabelAr: "السعر",
			Kind:     subject.FieldMoney,
			Required: true,
			Unit:     "AED", UnitAr: "درهم",
			Min:         int64p(100_000), // AED 1,000
			Placeholder: "140,000",
		}},

		Clocks: []subject.ClockOption{{
			Key:   ClockOpen60d,
			Mode:  subject.ModeFreshness,
			Label: "Open advert · 60 days", LabelAr: "إعلان مفتوح · ٦٠ يوماً",
			// The permit clause is here rather than buried in the trust row,
			// because it is the part that surprises people: an advert can come
			// down before its term through nobody's fault.
			Detail:   "Ends sooner if the permit expires first",
			DetailAr: "ينتهي قبل ذلك إذا انتهى التصريح",
			Default:  true,
		}},

		Trust: &subject.TrustUpload{
			Key: "permit",
			// Phrased as the gate it is, unlike the mulkiya's "earn Verified".
			// An advertiser who reads this as optional and skips it has a draft
			// that will never publish, and finding that out at the end is worse
			// than being told now.
			Label:   "Permit number — required before it can go live",
			LabelAr: "رقم التصريح — مطلوب قبل النشر",
			Earns:   "Published", EarnsAr: "منشور",
			Accept: []string{"image/jpeg", "image/png", "application/pdf"},

			// The number itself, which until now this flow had no way to
			// collect at all.
			//
			// Draft read `Values["permit_number"]` and Compose never asked for
			// it, so every property advert created from the app carried an
			// empty permit — and Submit refuses those, correctly. The result
			// was a vertical that was sellable from the phone right up to the
			// last tap and then always refused. The comment above this
			// composer describes drafting on site and submitting when the
			// paperwork arrives, which is the right shape; there was simply no
			// screen for the second half.
			//
			// A photograph of the permit is still the thing a reviewer wants,
			// but the number is what goes in the advert, what a regulator
			// searches for, and what the duplicate check compares. Asking for
			// it here means an agent who already has it can finish in one
			// sitting.
			Reference: &subject.Field{
				Key:   fieldPermit,
				Label: "Permit number", LabelAr: "رقم التصريح",
				Kind: subject.FieldText,
				// Required, which is a change of position from the comment at
				// the top of this file and worth being explicit about.
				//
				// That comment describes the real sequence — an agent films the
				// flat while DET processes the application — and concludes the
				// advert should be draftable without a permit and submitted
				// later. The *model* still supports exactly that: Draft accepts
				// an empty permit and only Submit refuses one.
				//
				// What does not exist is the second visit. The app's sell flow
				// is one sitting that ends in Publish, and there is no screen
				// anywhere that opens an existing draft to finish it. So an
				// optional permit here does not mean "come back later", it
				// means "reach the last step and be refused" — which is the
				// worst version of both options.
				//
				// Required until a resume-a-draft screen exists. At that point
				// this becomes optional again and the flow the comment above
				// describes finally works.
				Required:    true,
				Placeholder: "7128394",
				Hint:        "Trakheesi in Dubai, DARI in Abu Dhabi.",
				HintAr:      "تصريح في دبي، داري في أبوظبي.",
			},
		},
	}
}

// Draft creates the advert in `draft`.
//
// Deliberately permissive about the permit, and strict about everything else.
// The advertiser is standing in the property; the brokerage's paperwork is
// somewhere else entirely.
func (c *Composer) Draft(ctx context.Context, in subject.DraftInput) (subject.Drafted, error) {
	var fields []httpx.FieldError
	bad := func(key, code, msg string) {
		fields = append(fields, httpx.FieldError{Field: key, Code: code, Message: msg})
	}

	purpose := strings.TrimSpace(in.Values[fieldPurpose])
	propertyType := strings.TrimSpace(in.Values[fieldKind])

	size, err := parseInt(in.Values[fieldSize])
	if err != nil {
		bad(fieldSize, "invalid", "expected a whole number of square feet")
	}
	price, err := parseInt64(in.Values[fieldPrice])
	if err != nil {
		bad(fieldPrice, "invalid", "expected minor units")
	}

	if in.Clock != "" && in.Clock != ClockOpen60d {
		bad("clock", "unsupported", "this vertical only offers "+ClockOpen60d)
	}
	if len(fields) > 0 {
		return subject.Drafted{}, &httpx.ValidationError{Fields: fields}
	}

	// Bedrooms are not asked for on the form and are inferred from the type.
	//
	// A deliberate trade against the four-field cap: bedroom count is the single
	// most-filtered attribute in property search, and it is also the one an
	// advertiser will happily correct later from a fuller edit screen. Guessing
	// one for a studio is safe because a studio has none by definition; guessing
	// for an apartment would be inventing data, so it takes the minimum the
	// validator allows and the review desk sees it.
	bedrooms := 1
	switch propertyType {
	case "studio", "land", "office", "shop", "warehouse":
		bedrooms = 0
	case "villa", "townhouse":
		bedrooms = 3
	}

	// Rent is quoted yearly here and the form does not ask, because the hint on
	// the purpose field says so. This is the one default in the vertical and it
	// is the market's convention rather than a guess.
	rentPeriod := ""
	if Purpose(purpose) == PurposeRent {
		rentPeriod = "yearly"
	}

	listing, err := c.svc.Create(ctx, CreateInput{
		AdvertiserID: in.SellerID,
		Draft: Draft{
			Purpose:      purpose,
			PropertyType: propertyType,
			Bedrooms:     bedrooms,
			Bathrooms:    1,
			SizeSqft:     size,
			PriceMinor:   price,
			Currency:     "AED",
			RentPeriod:   rentPeriod,
			PermitNumber: strings.TrimSpace(in.Values[fieldPermit]),
			// Derived from where the property is, not asked for.
			//
			// The authority is a fact about the emirate — Dubai issues
			// Trakheesi, Abu Dhabi issues DARI — so asking an agent standing in
			// the flat to pick one is asking them to restate the address. It is
			// also the field they would get wrong: the permit in their hand says
			// neither word on it.
			//
			// Validate still checks the number against the issuer's format, so
			// a Dubai number typed for an Abu Dhabi property is caught rather
			// than silently accepted under a guessed authority.
			PermitIssuer: issuerFor(in.Region),
			Emirate:      in.Region,
			Community:    in.Place,
			Lat:          in.Lat,
			Lng:          in.Lng,
		},
	})
	if err != nil {
		return subject.Drafted{}, err
	}

	return subject.Drafted{
		SubjectID:     listing.ID,
		CTAKind:       string(action.KindEnquireProperty),
		CTATargetType: string(action.TargetProperty),
	}, nil
}

// Publish sends the advert to the permit desk.
//
// This is where skipping the permit upload becomes visible, and the error names
// the field so the client can say which one. An advertiser who filmed, answered
// four questions and tapped Publish gets told they need the number — not
// silently left with a draft they think is live.
func (c *Composer) Publish(ctx context.Context, advertiserID, listingID uuid.UUID) error {
	return c.svc.Submit(ctx, advertiserID, listingID)
}

// issuerFor maps an emirate to the authority that licenses adverts there.
//
// Only two of the seven have their own register today. IssuerOther is the
// honest answer for the rest rather than defaulting to Trakheesi, which would
// apply Dubai's number format to a Sharjah advert and reject a valid permit for
// looking wrong.
//
// An empty region also lands on IssuerOther, which keeps the format check
// permissive rather than refusing a draft because the client sent no emirate.
func issuerFor(emirate string) string {
	switch strings.ToLower(strings.TrimSpace(emirate)) {
	case "dubai", "دبي":
		return string(IssuerTrakheesi)
	case "abu dhabi", "abudhabi", "أبوظبي", "ابوظبي":
		return string(IssuerDARI)
	default:
		return string(IssuerOther)
	}
}

func parseInt(v string) (int, error) {
	n, err := parseInt64(v)
	return int(n), err
}

// parseInt64 accepts what a keypad and a grouped display produce, matching
// vehicles: the box renders "1,250" as the advertiser types, and refusing what
// it renders is a dead end.
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
