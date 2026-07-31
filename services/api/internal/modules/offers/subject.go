package offers

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

// SubjectProvider is what the feed knows about a flash offer.
//
// The fourth provider, and the one that finally uses ModeWindow — the mode the
// seam has carried unexercised since it was written. A window fills as it runs
// where a countdown drains, and the seam's own note explains why merging them
// would have been wrong: the client cannot infer which is which from how far
// away the end is.
type SubjectProvider struct {
	pool *pgxpool.Pool
}

func NewSubjectProvider(pool *pgxpool.Pool) *SubjectProvider { return &SubjectProvider{pool: pool} }

// Facet sits between meals and cars.
//
// Order 15, in the gap the other two left. An offer is a *reason to act now*
// rather than a category of thing, so it belongs high in the row — but not
// above the launch vertical, because a filter row that leads with discounts
// teaches viewers the product is a coupon site.
func (p *SubjectProvider) Facet() subject.Facet {
	return subject.Facet{Label: "Offers", LabelAr: "عروض", Icon: "flash", Order: 15}
}

// Project resolves the recurring rule into tonight's — or the next — window.
//
// Returns the occurrence whether or not it is running. A reel about an offer
// that opens at ten still belongs in the feed at eight: "starts 22:00" is
// useful, and dropping it until the moment it opens would give a four-hour
// offer a four-hour audience.
//
// Not projected at all when the offer is sold out, inactive, or its calendar
// range has passed. A window bar over a thing nobody can claim is worse than no
// bar, because the viewer taps.
func (p *SubjectProvider) Project(ctx context.Context, offerID uuid.UUID) (subject.TimeSignal, bool, error) {
	var (
		w            Window
		days         []int16
		runsFrom     *time.Time
		runsUntil    *time.Time
		startMinutes int
		endMinutes   int
		isActive     bool
		branchLive   bool
		quantity     *int
		claimed      int
		zone         *string
	)

	err := p.pool.QueryRow(ctx, `
		SELECT EXTRACT(hour FROM o.starts_at_local) * 60 + EXTRACT(minute FROM o.starts_at_local),
		       EXTRACT(hour FROM o.ends_at_local)   * 60 + EXTRACT(minute FROM o.ends_at_local),
		       o.days_of_week, o.runs_from, o.runs_until,
		       o.is_active, (b.is_active AND b.deleted_at IS NULL AND v.status = 'active'),
		       o.quantity, o.claimed, b.timezone
		  FROM flash_offers o
		  JOIN vendor_branches b ON b.id = o.branch_id
		  JOIN vendors v ON v.id = b.vendor_id
		 WHERE o.id = $1`, offerID,
	).Scan(&startMinutes, &endMinutes, &days, &runsFrom, &runsUntil,
		&isActive, &branchLive, &quantity, &claimed, &zone)
	if errors.Is(err, pgx.ErrNoRows) {
		return subject.TimeSignal{}, false, nil
	}
	if err != nil {
		return subject.TimeSignal{}, false, fmt.Errorf("offers: project signal: %w", err)
	}
	if !isActive || !branchLive {
		return subject.TimeSignal{}, false, nil
	}
	// Sold out for good. A per-occurrence sell-out is different and is not
	// checked here: tonight being gone does not mean tomorrow night is, and the
	// projection is the *rule*, not one run of it.
	if quantity != nil && claimed >= *quantity {
		return subject.TimeSignal{}, false, nil
	}

	loc := time.UTC
	if zone != nil && *zone != "" {
		if l, err := time.LoadLocation(*zone); err == nil {
			loc = l
		}
	}

	w = Window{StartLocal: startMinutes, EndLocal: endMinutes}
	for _, d := range days {
		w.Days = append(w.Days, int(d))
	}
	if runsFrom != nil {
		w.RunsFrom = runsFrom.In(loc)
	}
	if runsUntil != nil {
		w.RunsUntil = runsUntil.In(loc)
	}

	occ, ok := w.Resolve(time.Now().In(loc))
	if !ok {
		return subject.TimeSignal{}, false, nil
	}

	return subject.TimeSignal{
		Mode:     subject.ModeWindow,
		StartsAt: occ.Start,
		EndsAt:   occ.End,
		// The bar is drawn across the window itself, which is the one case where
		// the interval supplies its own denominator — a four-hour offer wants a
		// bar that visibly moves over four hours, not over some arbitrary
		// urgency horizon.
		Horizon: occ.End.Sub(occ.Start),
		// The rule comes back tomorrow. Without this the client removes the reel
		// the moment the window closes, and a nightly offer would disappear from
		// the feed permanently at 02:00 on its first night.
		Recurring: true,
		Timezone:  loc.String(),
	}, true, nil
}

// Summarize composes the badge and the lines above the CTA.
func (p *SubjectProvider) Summarize(
	ctx context.Context, ids []uuid.UUID, v subject.Viewer,
) (map[uuid.UUID]subject.Summary, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT o.id, o.name, COALESCE(o.name_ar, o.name),
		       o.price_minor, o.original_price_minor, o.currency,
		       o.quantity, o.claimed,
		       COALESCE(b.name, ''), COALESCE(b.name_ar, b.name, '')
		  FROM flash_offers o
		  JOIN vendor_branches b ON b.id = o.branch_id
		 WHERE o.id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("offers: summarize: %w", err)
	}
	defer rows.Close()

	out := make(map[uuid.UUID]subject.Summary, len(ids))
	for rows.Next() {
		var (
			id               uuid.UUID
			name, nameAr     string
			priceMinor       int64
			originalMinor    *int64
			currency         string
			quantity         *int
			claimed          int
			branch, branchAr string
		)
		if err := rows.Scan(&id, &name, &nameAr, &priceMinor, &originalMinor,
			&currency, &quantity, &claimed, &branch, &branchAr); err != nil {
			return nil, fmt.Errorf("offers: scan summary: %w", err)
		}

		// The discount is the badge, when there is one. "40% OFF" is the single
		// most legible thing on the overlay and it is the reason the format
		// exists; falling back to "OFFER" when no reference price was given is
		// honest rather than inventing a saving.
		badge, badgeAr := "OFFER", "عرض"
		if originalMinor != nil && *originalMinor > priceMinor {
			pct := int(float64(*originalMinor-priceMinor) / float64(*originalMinor) * 100)
			if pct > 0 {
				badge = fmt.Sprintf("%d%% OFF", pct)
				badgeAr = fmt.Sprintf("خصم %d%%", pct)
			}
		}

		out[id] = subject.Summary{
			Badge:   badge,
			BadgeAr: badgeAr,
			Title:   name,
			TitleAr: nameAr,
			// The kitchen, then what is left. Scarcity last because it is the
			// part that changes minute to minute, and a line that reorders
			// itself as stock moves is hard to read.
			Line:          joinLine(branch, remaining(quantity, claimed)),
			LineAr:        joinLine(branchAr, remainingAr(quantity, claimed)),
			PriceMinor:    priceMinor,
			Currency:      currency,
			PriceSuffix:   wasPrice(originalMinor, currency),
			PriceSuffixAr: wasPriceAr(originalMinor, currency),
		}
	}
	return out, rows.Err()
}

// remaining renders scarcity only when the vendor set a limit.
//
// An offer with no quantity says nothing, rather than "unlimited" — which reads
// as a warning that nobody wants it.
func remaining(quantity *int, claimed int) string {
	if quantity == nil {
		return ""
	}
	left := *quantity - claimed
	switch {
	case left <= 0:
		return "gone"
	case left == 1:
		return "1 left"
	// Above a couple of dozen the number stops being urgency and starts being
	// inventory, which is the vendor's business and not the viewer's.
	case left > 25:
		return ""
	default:
		return fmt.Sprintf("%d left", left)
	}
}

func remainingAr(quantity *int, claimed int) string {
	if quantity == nil {
		return ""
	}
	left := *quantity - claimed
	switch {
	case left <= 0:
		return "نفد"
	case left > 25:
		return ""
	default:
		return fmt.Sprintf("بقي %d", left)
	}
}

// wasPrice puts the struck-through original in the suffix slot.
//
// The suffix is where a meal plan puts "/wk" — a short qualifier rendered
// smaller beside the number — and "was AED 120" is the same shape of fact. It
// avoids adding a field to Summary that only one vertical would ever set.
func wasPrice(original *int64, currency string) string {
	if original == nil {
		return ""
	}
	return fmt.Sprintf(" was %s %s", currency, group(*original/100))
}

func wasPriceAr(original *int64, currency string) string {
	if original == nil {
		return ""
	}
	return fmt.Sprintf(" بدلاً من %s %s", currency, group(*original/100))
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
