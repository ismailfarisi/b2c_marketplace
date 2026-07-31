package catalog

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

// PlanSubjectProvider is what the feed is allowed to know about a meal plan.
//
// The counterpart to PlanHandler, and here for the same reason: `subscription_plans`
// is a catalogue table, so the module that owns the table owns the provider.
//
// It is also the first vertical to arrive *after* the subject seam was finished,
// which makes it the test of the claim the seam was built on. Adding it is this
// file and one line in app.BuildRegistries. Nothing in the feed, the ranker, the
// filter row, the sell grid or either client's reel overlay changes — and the
// clients gain a chip, a badge and a countdown they were never told about.
type PlanSubjectProvider struct {
	pool *pgxpool.Pool
}

func NewPlanSubjectProvider(pool *pgxpool.Pool) *PlanSubjectProvider {
	return &PlanSubjectProvider{pool: pool}
}

// Facet puts food first.
//
// Order 10 against vehicles' 20, which is the slot vehicles explicitly left:
// "Food is the launch vertical per PLAN-VIDEO-FIRST §5 and belongs first when it
// registers; this is the second thing anyone came for." Taking it here rather
// than renumbering vehicles is the point — a vertical declares where it belongs
// and nothing arbitrates.
func (p *PlanSubjectProvider) Facet() subject.Facet {
	return subject.Facet{Label: "Meals", LabelAr: "وجبات", Icon: "meal", Order: 10}
}

// Project computes the plan's time signal: a daily cutoff.
//
// # Why cutoff and not countdown
//
// A meal plan does not end. What ends, every single day, is the window to
// change tomorrow's delivery — and that boundary comes back, which is exactly
// what ModeCutoff means and why Recurring exists. Rendering it as a countdown
// would tell a viewer the plan expires at 16:00, and by the following morning
// the reel would be showing a timer that ran out on a subscription still
// happily running.
//
// The instant is resolved here and the IANA zone travels with it. "Order by
// 16:00" is a rule in the kitchen's wall clock, not a moment the kitchen once
// meant: an offset is correct until the first DST boundary in a market that
// observes one, and a plan served to a viewer in another timezone must still
// read as the kitchen's four o'clock.
func (p *PlanSubjectProvider) Project(ctx context.Context, planID uuid.UUID) (subject.TimeSignal, bool, error) {
	var (
		isActive     bool
		branchLive   bool
		cutoffHours  int
		timezoneName *string
	)
	err := p.pool.QueryRow(ctx, `
		SELECT pl.is_active,
		       (b.is_active AND b.deleted_at IS NULL AND v.status = 'active'),
		       pl.cutoff_hours, b.timezone
		  FROM subscription_plans pl
		  JOIN vendor_branches b ON b.id = pl.branch_id
		  JOIN vendors v ON v.id = b.vendor_id
		 WHERE pl.id = $1`, planID).Scan(&isActive, &branchLive, &cutoffHours, &timezoneName)
	if errors.Is(err, pgx.ErrNoRows) {
		return subject.TimeSignal{}, false, nil
	}
	if err != nil {
		return subject.TimeSignal{}, false, fmt.Errorf("catalog: project plan signal: %w", err)
	}
	// A withdrawn plan or a closed kitchen has no clock worth drawing. Returning
	// ok=false deletes the projection rather than leaving a cutoff ticking for a
	// subscription nobody can start.
	if !isActive || !branchLive {
		return subject.TimeSignal{}, false, nil
	}

	zone := "Asia/Dubai"
	if timezoneName != nil && *timezoneName != "" {
		zone = *timezoneName
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		// An unknown zone is a data problem, and guessing one would put a
		// kitchen's cutoff an hour out for everybody. Better to draw no clock.
		return subject.TimeSignal{}, false, nil
	}

	next := nextCutoff(time.Now().In(loc), cutoffHours)

	return subject.TimeSignal{
		Mode:      subject.ModeCutoff,
		EndsAt:    next,
		Timezone:  zone,
		Recurring: true,
		// The bar is drawn across today's ordering day rather than across the
		// gap to the cutoff, so it moves at the same rate all day instead of
		// leaping when the boundary rolls.
		Horizon: 24 * time.Hour,
	}, true, nil
}

// CutoffReconcileWindow is how far ahead a projected cutoff stays useful.
//
// The projection is a stored instant and the boundary recurs daily, so a row
// written yesterday points at a cutoff that has passed. The reconcile worker
// re-projects on a schedule; this constant is what makes the staleness explicit
// rather than something a reader has to infer from the recurrence.
const CutoffReconcileWindow = 24 * time.Hour

// nextCutoff resolves "cutoff_hours before the next delivery day" into the next
// instant it happens.
//
// The column is hours-before-delivery and deliveries are daily, so a plan with
// cutoff_hours = 20 stops taking changes for tomorrow at 04:00 today — twenty
// hours before midnight. Modelling it as an offset from midnight rather than as
// a wall-clock column is the schema's decision, and this is the one place that
// arithmetic is written down.
func nextCutoff(now time.Time, cutoffHours int) time.Time {
	if cutoffHours < 0 {
		cutoffHours = 0
	}
	// AddDate rather than Add(24h), so a DST transition moves the boundary with
	// the wall clock instead of sliding it an hour. The UAE does not observe
	// one; the kitchens this will serve in Amman and Beirut do.
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).
		AddDate(0, 0, 1)
	cutoff := midnight.Add(-time.Duration(cutoffHours) * time.Hour)

	// A loop, not a single roll-forward, and the difference is not theoretical:
	// the CHECK allows cutoff_hours up to 168, so a week's notice subtracts six
	// whole days off tomorrow's midnight. One `+1 day` leaves that five days in
	// the past, and the client renders a cutoff that expired last Sunday.
	//
	// Bounded rather than `for`: the step is a day and the shortfall is at most
	// the 168-hour cap, so eight iterations is comfortably enough and a bug that
	// made this not terminate would otherwise spin inside a request.
	for i := 0; i < 9 && !cutoff.After(now); i++ {
		cutoff = cutoff.AddDate(0, 0, 1)
	}
	return cutoff
}

// Summarize composes the badge and the lines above the CTA.
//
// One query for the page, same as vehicles and for the same reason: a page of
// ten reels resolved one at a time is ten sequential round-trips on the hottest
// read path in the product.
func (p *PlanSubjectProvider) Summarize(
	ctx context.Context, ids []uuid.UUID, v subject.Viewer,
) (map[uuid.UUID]subject.Summary, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT pl.id, pl.name, COALESCE(pl.name_ar, pl.name),
		       pl.days_per_week, pl.meals_per_day, pl.cycle_days,
		       pl.price_minor, pl.currency,
		       COALESCE(b.name, ''), COALESCE(b.name_ar, b.name, '')
		  FROM subscription_plans pl
		  JOIN vendor_branches b ON b.id = pl.branch_id
		 WHERE pl.id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("catalog: summarize plans: %w", err)
	}
	defer rows.Close()

	out := make(map[uuid.UUID]subject.Summary, len(ids))
	for rows.Next() {
		var (
			id                       uuid.UUID
			name, nameAr             string
			daysPerWeek, mealsPerDay int
			cycleDays                int
			priceMinor               int64
			currency                 string
			branch, branchAr         string
		)
		if err := rows.Scan(&id, &name, &nameAr, &daysPerWeek, &mealsPerDay,
			&cycleDays, &priceMinor, &currency, &branch, &branchAr); err != nil {
			return nil, fmt.Errorf("catalog: scan plan summary: %w", err)
		}

		// The suffix is what makes the number mean anything. AED 449 is
		// unremarkable for a month and startling for a week, and a price with no
		// period attached is the one thing a subscription tile must not show.
		suffix, suffixAr := periodSuffix(cycleDays)

		out[id] = subject.Summary{
			Badge:   "MEAL SUBSCRIPTION",
			BadgeAr: "اشتراك وجبات",
			Title:   name,
			TitleAr: nameAr,
			// The kitchen's name leads. For food the vendor *is* the product —
			// two identical five-day plans from two kitchens are not
			// interchangeable, which is the opposite of how a used car works.
			Line:          joinPlanLine(branch, weekly(daysPerWeek), perDay(mealsPerDay)),
			LineAr:        joinPlanLine(branchAr, weeklyAr(daysPerWeek), perDayAr(mealsPerDay)),
			PriceMinor:    priceMinor,
			Currency:      currency,
			PriceSuffix:   suffix,
			PriceSuffixAr: suffixAr,
		}
	}
	return out, rows.Err()
}

func periodSuffix(cycleDays int) (string, string) {
	switch {
	case cycleDays <= 0:
		return "", ""
	case cycleDays <= 7:
		return "/wk", "/أسبوع"
	case cycleDays <= 31:
		return "/mo", "/شهر"
	default:
		return fmt.Sprintf("/%dd", cycleDays), fmt.Sprintf("/%d يوم", cycleDays)
	}
}

func weekly(days int) string {
	if days <= 0 {
		return ""
	}
	if days >= 7 {
		return "every day"
	}
	return fmt.Sprintf("%d days a week", days)
}

func weeklyAr(days int) string {
	if days <= 0 {
		return ""
	}
	if days >= 7 {
		return "كل يوم"
	}
	return fmt.Sprintf("%d أيام في الأسبوع", days)
}

func perDay(meals int) string {
	if meals <= 1 {
		return ""
	}
	return fmt.Sprintf("%d meals a day", meals)
}

func perDayAr(meals int) string {
	if meals <= 1 {
		return ""
	}
	return fmt.Sprintf("%d وجبات يومياً", meals)
}

// joinPlanLine drops empties and joins with a middot.
//
// Pre-joined here rather than returned as parts, for the reason vehicles gives:
// the separator is this vertical's presentation decision, and a client
// re-joining an array would eventually pick a comma and get the Arabic wrong.
func joinPlanLine(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, s := range parts {
		if strings.TrimSpace(s) != "" {
			kept = append(kept, s)
		}
	}
	return strings.Join(kept, " · ")
}
