package vehicles

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/seam/subject"
)

// SubjectProvider owns what the reel overlay says about a vehicle listing.
//
// The counterpart to InquiryHandler in this package, and the same ownership
// rule: `vehicle_listing` has exactly one owner, so both the CTA and the overlay
// live here rather than in the feed. Registering it is one line in
// app.BuildRegistries. Nothing in feed, ranking, moderation or media learns that
// 64,000 is a kilometre reading.
type SubjectProvider struct {
	pool *pgxpool.Pool
}

func NewSubjectProvider(pool *pgxpool.Pool) *SubjectProvider { return &SubjectProvider{pool: pool} }

// Facet is how vehicles appears in the feed's filter row.
//
// "Cars", not "Vehicles" and certainly not "vehicle_listing". The row is read at
// a glance by someone deciding whether to keep scrolling, and the word they use
// for the thing is the word that belongs on the chip.
//
// Order 20 leaves room below it. Food is the launch vertical per
// PLAN-VIDEO-FIRST §5 and belongs first when it registers; this is the second
// thing anyone came for.
func (p *SubjectProvider) Facet() subject.Facet {
	return subject.Facet{Label: "Cars", LabelAr: "سيارات", Icon: "car", Order: 20}
}

// FreshnessHorizon is the span a listing's age is drawn against.
//
// Thirty days, matching the default listing term, so the bar reads as "how much
// of this listing's life is left" rather than as an abstract decay. It is a
// vertical's number and belongs in the vertical: three days old is fresh for a
// car and stale for a restaurant offer, and the feed has no basis for choosing
// between them.
const FreshnessHorizon = 30 * 24 * time.Hour

// Project computes the listing's time signal for storage.
//
// Vehicles get freshness, not a countdown, and that is a product decision rather
// than a limitation. A classified does not close at a moment — it sits until it
// sells — and dressing `expires_at` up as a countdown would manufacture urgency
// the seller never agreed to and the buyer would learn to ignore within a week.
//
// What is true and useful is how long it has been sitting. A car listed three
// days ago and one listed three months ago are different propositions at the
// same price, and the second is the one where the buyer has leverage.
//
// Returns ok=false for anything not currently live. A sold or paused listing has
// no time dimension worth drawing, and a stale row left behind would keep a bar
// on screen for a car nobody can buy.
func (p *SubjectProvider) Project(ctx context.Context, listingID uuid.UUID) (subject.TimeSignal, bool, error) {
	var (
		status      string
		publishedAt *time.Time
		bumpedAt    *time.Time
	)
	err := p.pool.QueryRow(ctx, `
		SELECT status, published_at, bumped_at
		  FROM vehicle_listings
		 WHERE id = $1`, listingID).Scan(&status, &publishedAt, &bumpedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return subject.TimeSignal{}, false, nil
	}
	if err != nil {
		return subject.TimeSignal{}, false, fmt.Errorf("vehicles: project time signal: %w", err)
	}
	if status != "active" || publishedAt == nil {
		return subject.TimeSignal{}, false, nil
	}

	// A bump resets the clock, because a bump is the seller paying to say "this
	// is current". Honouring that in search ranking but not in the badge the
	// buyer actually reads would sell them a promotion with no visible effect.
	since := *publishedAt
	if bumpedAt != nil && bumpedAt.After(since) {
		since = *bumpedAt
	}

	return subject.TimeSignal{
		Mode:     subject.ModeFreshness,
		StartsAt: since,
		Horizon:  FreshnessHorizon,
	}, true, nil
}

// Summarize composes the badge and the lines above the CTA.
//
// One query for the whole page. The N+1 this avoids is not hypothetical — it is
// twenty round-trips on the hottest read path in the product, which is the same
// argument the action seam's ResolveBatch makes and wins for the same reason.
func (p *SubjectProvider) Summarize(
	ctx context.Context, ids []uuid.UUID, v subject.Viewer,
) (map[uuid.UUID]subject.Summary, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT l.id,
		       mk.name, COALESCE(mk.name_ar, mk.name),
		       md.name, COALESCE(md.name_ar, md.name),
		       l.year, l.mileage_km, l.regional_spec,
		       COALESCE(l.city, l.emirate, ''),
		       l.price_minor, l.currency,
		       -- Verified means a human looked at the car, and it is the most
		       -- valuable word on the overlay in a market where an honest
		       -- listing and a scam look identical. An expired report is not
		       -- verified: the badge claims a current fact.
		       EXISTS (
		           SELECT 1 FROM inspection_reports ir
		            WHERE ir.listing_id = l.id
		              AND ir.inspected_at IS NOT NULL
		              AND (ir.expires_at IS NULL OR ir.expires_at > now())
		       ) AS inspected
		  FROM vehicle_listings l
		  JOIN vehicle_makes mk ON mk.id = l.make_id
		  JOIN vehicle_models md ON md.id = l.model_id
		 WHERE l.id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("vehicles: summarize: %w", err)
	}
	defer rows.Close()

	out := make(map[uuid.UUID]subject.Summary, len(ids))
	for rows.Next() {
		var (
			id                 uuid.UUID
			makeName, makeAr   string
			modelName, modelAr string
			year, mileageKM    int
			spec, place        string
			priceMinor         int64
			currency           string
			inspected          bool
		)
		if err := rows.Scan(&id, &makeName, &makeAr, &modelName, &modelAr,
			&year, &mileageKM, &spec, &place, &priceMinor, &currency, &inspected); err != nil {
			return nil, fmt.Errorf("vehicles: scan summary: %w", err)
		}

		badge, badgeAr := "USED CAR", "سيارة مستعملة"
		if inspected {
			badge, badgeAr = "USED CAR · VERIFIED", "سيارة مستعملة · موثقة"
		}

		out[id] = subject.Summary{
			Badge:   badge,
			BadgeAr: badgeAr,
			Title:   fmt.Sprintf("%d %s %s", year, makeName, modelName),
			// Year first in both languages. It reads as a numeral either way,
			// and every listing in this market is titled that way — matching the
			// convention matters more than matching the grammar.
			TitleAr:    fmt.Sprintf("%d %s %s", year, makeAr, modelAr),
			Line:       joinLine(formatKM(mileageKM), strings.ToUpper(spec), place),
			LineAr:     joinLine(formatKMAr(mileageKM), specArabic(spec), place),
			PriceMinor: priceMinor,
			Currency:   currency,
		}
	}
	return out, rows.Err()
}

// joinLine assembles the qualifier line, dropping empties.
//
// Pre-joined here rather than returned as parts, because the separator is a
// presentation decision this vertical is making: a middot works in both scripts,
// and a client re-joining an array would eventually pick a comma and get the
// Arabic wrong.
func joinLine(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " · ")
}

// formatKM groups thousands. Done here rather than on the client for the same
// reason the price is: it is this vertical's fact, and a client formatting it
// would first have to know it is a distance.
func formatKM(km int) string {
	if km <= 0 {
		return ""
	}
	return group(km) + " km"
}

func formatKMAr(km int) string {
	if km <= 0 {
		return ""
	}
	return group(km) + " كم"
}

func group(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	lead := len(s) % 3
	if lead > 0 {
		b.WriteString(s[:lead])
	}
	for i := lead; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// specArabic renders regional spec — the attribute the schema singles out as
// swinging GCC resale value by 20–30%, and therefore the one the line cannot
// omit. Left as the market says it out loud rather than translated literally.
func specArabic(spec string) string {
	switch spec {
	case "gcc":
		return "خليجي"
	case "us":
		return "أمريكي"
	case "japanese":
		return "ياباني"
	case "european":
		return "أوروبي"
	case "canadian":
		return "كندي"
	case "chinese":
		return "صيني"
	default:
		return ""
	}
}
