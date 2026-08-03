package auctions

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/seam/subject"
)

// SubjectProvider is what the feed knows about a lot.
type SubjectProvider struct{ pool *pgxpool.Pool }

func NewSubjectProvider(pool *pgxpool.Pool) *SubjectProvider {
	return &SubjectProvider{pool: pool}
}

// Facet puts plates between offers and cars.
//
// Order 25. Above property because a lot that closes today is more urgent than
// a flat that has been listed for a month, and below cars because far more
// people are shopping for a Patrol than for "AA 5".
func (p *SubjectProvider) Facet() subject.Facet {
	return subject.Facet{Label: "Plates", LabelAr: "أرقام مميزة", Icon: "tag", Order: 25}
}

// Project is the fourth time mode, finally in production.
//
// A countdown, with a one-hour horizon over a lot that may run for nine days.
// That combination is the whole reason [subject.TimeSignal.Horizon] exists, and
// the field's own comment uses this exact case: a bar spread across nine days
// does not visibly move during the two minutes anyone is watching.
//
// Not recurring. When this clock reaches zero the lot is gone — unlike a meal
// cutoff, which rolls to tomorrow, or a flash offer, which opens again
// tonight. That distinction is the flag the client uses to decide whether an
// expired clock removes the reel or resets it.
func (p *SubjectProvider) Project(ctx context.Context, id uuid.UUID) (subject.TimeSignal, bool, error) {
	var (
		status   string
		opensAt  time.Time
		closesAt time.Time
	)
	err := p.pool.QueryRow(ctx, `
		SELECT status, opens_at, closes_at FROM plate_auctions WHERE id = $1`, id,
	).Scan(&status, &opensAt, &closesAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return subject.TimeSignal{}, false, nil
	}
	if err != nil {
		return subject.TimeSignal{}, false, fmt.Errorf("auctions: project: %w", err)
	}

	// A scheduled lot still gets a signal, unlike every other vertical's
	// pre-live state. A plate auction that opens on Thursday is worth showing on
	// Tuesday — the whole point of a lot is that people arrange to be there —
	// and the client already renders a not-yet-open clock as "opens in 4h 56m",
	// which is exactly the right words for it.
	switch Status(status) {
	case StatusLive, StatusScheduled:
	default:
		return subject.TimeSignal{}, false, nil
	}

	return subject.TimeSignal{
		Mode:     subject.ModeCountdown,
		StartsAt: opensAt,
		EndsAt:   closesAt,
		Horizon:  CountdownHorizon,
	}, true, nil
}

// Summarize composes the badge and the lines above the CTA.
//
// The price shown is the *current bid*, not the opening one, and it changes
// under the viewer as the lot runs. That is the one thing this vertical needs
// from the summary slot that no other does — and it needs no new machinery,
// because a summary is already resolved per page rather than cached.
func (p *SubjectProvider) Summarize(
	ctx context.Context, ids []uuid.UUID, v subject.Viewer,
) (map[uuid.UUID]subject.Summary, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, emirate, plate_code, plate_number, digit_count, currency,
		       opening_minor, reserve_minor, current_minor, bid_count, status
		  FROM plate_auctions WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("auctions: summarize: %w", err)
	}
	defer rows.Close()

	out := make(map[uuid.UUID]subject.Summary, len(ids))
	for rows.Next() {
		var (
			id                    uuid.UUID
			emirate, code, number string
			digits                int
			currency              string
			opening               int64
			reserve, current      *int64
			bidCount              int
			status                string
		)
		if err := rows.Scan(&id, &emirate, &code, &number, &digits, &currency,
			&opening, &reserve, &current, &bidCount, &status); err != nil {
			return nil, fmt.Errorf("auctions: scan summary: %w", err)
		}

		price := opening
		if current != nil {
			price = *current
		}

		out[id] = subject.Summary{
			Badge:   badgeFor(digits, emirate),
			BadgeAr: badgeArFor(digits, emirate),
			// The plate itself, large. It is the entire product — there is no
			// photograph of a number that is more informative than the number.
			Title:      code + " " + number,
			TitleAr:    code + " " + number,
			Line:       line(bidCount, reserve, current, opening),
			LineAr:     lineAr(bidCount, reserve, current, opening),
			PriceMinor: price,
			Currency:   currency,
			// "current bid" rather than a bare price, because the number means
			// something different here than anywhere else in the feed: it is
			// what somebody else has already offered, not what the seller is
			// asking.
			PriceSuffix:   suffix(bidCount),
			PriceSuffixAr: suffixAr(bidCount),
		}
	}
	return out, rows.Err()
}

// badgeFor leads with the digit count, which is what a plate is worth.
func badgeFor(digits int, emirate string) string {
	if digits == 1 {
		return "1 DIGIT · " + emirate
	}
	return fmt.Sprintf("%d DIGITS · %s", digits, emirate)
}

func badgeArFor(digits int, emirate string) string {
	if digits == 1 {
		return "رقم واحد · " + emirateAr(emirate)
	}
	if digits == 2 {
		return "رقمان · " + emirateAr(emirate)
	}
	return fmt.Sprintf("%d أرقام · %s", digits, emirateAr(emirate))
}

func emirateAr(e string) string {
	switch e {
	case "Dubai":
		return "دبي"
	case "Abu Dhabi":
		return "أبوظبي"
	case "Sharjah":
		return "الشارقة"
	}
	return e
}

// line says how contested the lot is, and whether it will actually sell.
//
// "3 bids · reserve not met" is the single most useful sentence on the overlay:
// the first half is social proof, and the second stops someone bidding on a lot
// that is not going to trade. The reserve *amount* is never here — see the
// column comment on why publishing it would end the auction.
func line(bidCount int, reserve, current *int64, opening int64) string {
	var head string
	switch bidCount {
	case 0:
		head = "No bids yet"
	case 1:
		head = "1 bid"
	default:
		head = fmt.Sprintf("%d bids", bidCount)
	}
	if reserve == nil {
		return head + " · no reserve"
	}
	if current != nil && *current >= *reserve {
		return head + " · reserve met"
	}
	return head + " · reserve not met"
}

func lineAr(bidCount int, reserve, current *int64, opening int64) string {
	var head string
	switch bidCount {
	case 0:
		head = "لا مزايدات بعد"
	case 1:
		head = "مزايدة واحدة"
	case 2:
		head = "مزايدتان"
	default:
		head = fmt.Sprintf("%d مزايدات", bidCount)
	}
	if reserve == nil {
		return head + " · بدون سعر أدنى"
	}
	if current != nil && *current >= *reserve {
		return head + " · تم بلوغ السعر الأدنى"
	}
	return head + " · لم يُبلغ السعر الأدنى"
}

func suffix(bidCount int) string {
	if bidCount == 0 {
		return " to start"
	}
	return " current bid"
}

func suffixAr(bidCount int) string {
	if bidCount == 0 {
		return " للبدء"
	}
	return " المزايدة الحالية"
}
