package property

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

// SubjectProvider is what the feed knows about a property advert.
type SubjectProvider struct {
	pool *pgxpool.Pool
}

func NewSubjectProvider(pool *pgxpool.Pool) *SubjectProvider { return &SubjectProvider{pool: pool} }

// Facet puts property last of the four.
//
// Order 30, below cars. Not a judgement about importance — property is the
// highest-value transaction in the product by two orders of magnitude — but
// about frequency. The filter row is read at a glance by someone deciding
// whether to keep scrolling, and the chips people tap most belong nearest the
// thumb. Almost nobody opens this app to buy a flat today.
func (p *SubjectProvider) Facet() subject.Facet {
	return subject.Facet{Label: "Property", LabelAr: "عقارات", Icon: "home", Order: 30}
}

// Project gives an advert freshness, drawn against a sixty-day term.
//
// Freshness rather than a countdown, for the reason vehicles gives: a home does
// not close at a moment, it sits until it goes. What is true and useful is how
// long it has been sitting, and in this market that is the single most
// informative fact a buyer has — a flat listed three days ago and the same flat
// still listed four months later are different negotiations at the same asking
// price.
//
// The horizon is twice the vehicle one. Three days old is fresh for a car and
// brand new for a home, which is exactly the pair the seam's own Horizon comment
// uses to explain why the vertical picks the number.
func (p *SubjectProvider) Project(ctx context.Context, listingID uuid.UUID) (subject.TimeSignal, bool, error) {
	var (
		status        string
		publishedAt   *time.Time
		permitExpires *time.Time
	)
	err := p.pool.QueryRow(ctx, `
		SELECT status, published_at, permit_expires
		  FROM property_listings WHERE id = $1`, listingID,
	).Scan(&status, &publishedAt, &permitExpires)
	if errors.Is(err, pgx.ErrNoRows) {
		return subject.TimeSignal{}, false, nil
	}
	if err != nil {
		return subject.TimeSignal{}, false, fmt.Errorf("property: project signal: %w", err)
	}
	if Status(status) != StatusActive || publishedAt == nil {
		return subject.TimeSignal{}, false, nil
	}
	// A lapsed permit means the advert should not be on screen at all. The sweep
	// will retire it; until then, refusing to project keeps it out of the
	// Ending-soon ordering and drops its bar rather than drawing a clock on an
	// advert that is currently non-compliant.
	if PermitExpired(permitExpires, time.Now()) {
		return subject.TimeSignal{}, false, nil
	}

	return subject.TimeSignal{
		Mode:     subject.ModeFreshness,
		StartsAt: *publishedAt,
		Horizon:  FreshnessHorizon,
	}, true, nil
}

// Summarize composes the badge and the lines above the CTA.
//
// The badge carries the permit number, and that is a legal requirement rather
// than a design choice: the advert must display it. Putting it in the badge slot
// — where cars put "USED CAR · VERIFIED" — means it appears on the reel itself
// rather than on a detail screen the viewer may never open, which is what
// "displayed in the advertisement" means when the advertisement is a video.
func (p *SubjectProvider) Summarize(
	ctx context.Context, ids []uuid.UUID, v subject.Viewer,
) (map[uuid.UUID]subject.Summary, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, purpose, property_type, bedrooms, bathrooms, size_sqft,
		       COALESCE(furnishing, ''), price_minor, currency,
		       COALESCE(rent_period, ''),
		       COALESCE(title, ''),
		       COALESCE(community, emirate, ''), COALESCE(tower, ''),
		       COALESCE(permit_number, '')
		  FROM property_listings
		 WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("property: summarize: %w", err)
	}
	defer rows.Close()

	out := make(map[uuid.UUID]subject.Summary, len(ids))
	for rows.Next() {
		var (
			id                        uuid.UUID
			purpose, propertyType     string
			bedrooms, bathrooms, size int
			furnishing                string
			priceMinor                int64
			currency, rentPeriod      string
			title, community, tower   string
			permit                    string
		)
		if err := rows.Scan(&id, &purpose, &propertyType, &bedrooms, &bathrooms,
			&size, &furnishing, &priceMinor, &currency, &rentPeriod,
			&title, &community, &tower, &permit); err != nil {
			return nil, fmt.Errorf("property: scan summary: %w", err)
		}

		badge, badgeAr := badgeFor(Purpose(purpose), permit)

		out[id] = subject.Summary{
			Badge:   badge,
			BadgeAr: badgeAr,
			// "2 bed apartment" rather than the seller's headline, unless they
			// wrote one. The generated form is what buyers scan for; a headline
			// like "STUNNING UPGRADED UNIT!!" tells them nothing and is most of
			// what this market's adverts are titled.
			Title:      titleOr(title, bedrooms, propertyType),
			TitleAr:    titleOr(title, bedrooms, propertyType),
			Line:       joinLine(location(community, tower), sqft(size), baths(bathrooms), furnish(furnishing)),
			LineAr:     joinLine(location(community, tower), sqftAr(size), bathsAr(bathrooms), furnishAr(furnishing)),
			PriceMinor: priceMinor,
			Currency:   currency,
			// A rent with no period is off by twelve. The suffix slot is exactly
			// where a meal plan puts "/wk", and it is the same fact: what the
			// number is per.
			PriceSuffix:   rentSuffix(rentPeriod),
			PriceSuffixAr: rentSuffixAr(rentPeriod),
		}
	}
	return out, rows.Err()
}

// badgeFor states the purpose and the permit.
//
// The permit is truncated to nothing — it is shown in full. It is the one string
// on this overlay that exists for a regulator rather than for a buyer, and an
// abbreviated permit number satisfies neither.
func badgeFor(purpose Purpose, permit string) (string, string) {
	head, headAr := "FOR SALE", "للبيع"
	if purpose == PurposeRent {
		head, headAr = "FOR RENT", "للإيجار"
	}
	if permit == "" {
		return head, headAr
	}
	return head + " · PERMIT " + permit, headAr + " · تصريح " + permit
}

func titleOr(title string, bedrooms int, propertyType string) string {
	if strings.TrimSpace(title) != "" {
		return title
	}
	kind := strings.ReplaceAll(propertyType, "_", " ")
	if bedrooms == 0 {
		return capitalise(kind)
	}
	return fmt.Sprintf("%d bed %s", bedrooms, kind)
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func location(community, tower string) string {
	if tower != "" && community != "" {
		return tower + ", " + community
	}
	if tower != "" {
		return tower
	}
	return community
}

func sqft(size int) string {
	if size <= 0 {
		return ""
	}
	return group(int64(size)) + " sqft"
}

func sqftAr(size int) string {
	if size <= 0 {
		return ""
	}
	return group(int64(size)) + " قدم²"
}

func baths(n int) string {
	if n <= 0 {
		return ""
	}
	if n == 1 {
		return "1 bath"
	}
	return fmt.Sprintf("%d baths", n)
}

func bathsAr(n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("%d حمام", n)
}

func furnish(f string) string {
	switch f {
	case "furnished":
		return "furnished"
	case "semi_furnished":
		return "semi-furnished"
	case "unfurnished":
		return "unfurnished"
	}
	return ""
}

func furnishAr(f string) string {
	switch f {
	case "furnished":
		return "مفروش"
	case "semi_furnished":
		return "نصف مفروش"
	case "unfurnished":
		return "غير مفروش"
	}
	return ""
}

func rentSuffix(period string) string {
	switch period {
	case "yearly":
		return "/yr"
	case "monthly":
		return "/mo"
	case "weekly":
		return "/wk"
	case "daily":
		return "/night"
	}
	return ""
}

func rentSuffixAr(period string) string {
	switch period {
	case "yearly":
		return "/سنة"
	case "monthly":
		return "/شهر"
	case "weekly":
		return "/أسبوع"
	case "daily":
		return "/ليلة"
	}
	return ""
}

func joinLine(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, s := range parts {
		if strings.TrimSpace(s) != "" {
			kept = append(kept, s)
		}
	}
	return strings.Join(kept, " · ")
}

func group(n int64) string {
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
