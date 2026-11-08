package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/modules/promo"
)

// rotationServiceType is the service type the rotation is sold for.
const rotationServiceType = "meals_subscription"

const (
	// Kitchens rated at least this many times and below minSuggestRating are
	// left out of a suggestion; newer kitchens are judged on the prior.
	minSuggestRatings = 3
	minSuggestRating  = 3.5
	ratingPrior       = 4.2
	ratingPriorWeight = 3
	// A rotation is in one price band when its dearest week costs at most
	// this multiple of its cheapest.
	priceBandRatio = 1.5
	suggestPool    = 14
	poolPerCuisine = 3
)

// SuggestInput asks for a balanced rotation delivered to an address. Empty
// Weekdays or Slots let the suggestion pick the schedule most kitchens share.
type SuggestInput struct {
	UserID    uuid.UUID
	AddressID uuid.UUID
	StartDate *time.Time
	Weekdays  []int
	Slots     []string
	Promo     promo.Ref
}

// RotationSuggestion is a balanced rotation: the plans to send to preview and
// create, the schedule they share, and the same quote preview returns.
type RotationSuggestion struct {
	PlanIDs   []uuid.UUID   `json:"plan_ids"`
	BranchIDs []uuid.UUID   `json:"branch_ids"`
	StartDate string        `json:"start_date"`
	Weekdays  []int         `json:"delivery_weekdays"`
	Slots     []string      `json:"meal_slots"`
	Quote     RotationQuote `json:"quote"`
}

type suggestCandidate struct {
	PlanID      uuid.UUID
	BranchID    uuid.UUID
	Cuisines    []string
	Rating      *float64
	RatingCount int
	WeekPrice   int64
	Currency    string
	DaysPerWeek int
	MealsPerDay int
	Weekdays    []int
	Slots       []string
	CutoffHours int
	Timezone    string
	MenuCycleID uuid.NullUUID
	dishes      []VarietyDish
}

func (c suggestCandidate) quality() float64 {
	r := 0.0
	if c.Rating != nil {
		r = *c.Rating
	}
	n := float64(c.RatingCount)
	return (r*n + ratingPrior*ratingPriorWeight) / (n + ratingPriorWeight)
}

// wellRated keeps kitchens without enough ratings to judge.
func (c suggestCandidate) wellRated() bool {
	return c.RatingCount < minSuggestRatings || c.Rating == nil || *c.Rating >= minSuggestRating
}

func (c suggestCandidate) serves(weekdays []int, slots []string) bool {
	if len(weekdays) != c.DaysPerWeek || len(slots) != c.MealsPerDay {
		return false
	}
	for _, d := range weekdays {
		if len(c.Weekdays) > 0 && !containsInt(c.Weekdays, d) {
			return false
		}
	}
	for _, s := range slots {
		if len(c.Slots) > 0 && !containsString(c.Slots, s) {
			return false
		}
	}
	return true
}

// SuggestRotation picks four eligible, well-rated kitchens delivering to the
// address with the most cuisine variety in one price band, and quotes them.
func (s *Service) SuggestRotation(ctx context.Context, in SuggestInput) (RotationSuggestion, error) {
	if err := validateSchedule(in.Weekdays, in.Slots); err != nil {
		return RotationSuggestion{}, err
	}
	cands, err := s.suggestCandidates(ctx, in.UserID, in.AddressID)
	if err != nil {
		return RotationSuggestion{}, err
	}
	pool, weekdays, slots := suggestionPool(cands, in.Weekdays, in.Slots)
	if len(pool) < RotationKitchens {
		return RotationSuggestion{}, rotationInvalid("address_id", "not_enough_kitchens",
			fmt.Sprintf("fewer than %d rotation kitchens deliver here on that schedule", RotationKitchens))
	}

	start := earliestRotationStart(pool)
	if in.StartDate != nil {
		start = *in.StartDate
	}
	for i := range pool {
		m, err := loadKitchenMenu(ctx, s.pool, pool[i].BranchID, pool[i].MenuCycleID)
		if err != nil {
			return RotationSuggestion{}, err
		}
		pool[i].Cuisines = m.cuisines
		pool[i].dishes = m.weekDishes(calendarDay(start), weekdays, slots)
	}
	picked := pickBalanced(pool)

	out := RotationSuggestion{StartDate: start.Format("2006-01-02"), Weekdays: weekdays, Slots: slots}
	for _, c := range picked {
		out.PlanIDs = append(out.PlanIDs, c.PlanID)
		out.BranchIDs = append(out.BranchIDs, c.BranchID)
	}
	out.Quote, err = s.PreviewRotation(ctx, RotationInput{
		UserID: in.UserID, AddressID: in.AddressID, PlanIDs: out.PlanIDs,
		StartDate: start, Weekdays: weekdays, Slots: slots, Promo: in.Promo,
	})
	if err != nil {
		return RotationSuggestion{}, err
	}
	out.StartDate = out.Quote.StartDate
	return out, nil
}

// suggestCandidates lists each kitchen's cheapest rotation plan among the
// kitchens that deliver to the customer's address and take deliveries.
func (s *Service) suggestCandidates(ctx context.Context, userID, addressID uuid.UUID) ([]suggestCandidate, error) {
	var found bool
	err := s.pool.QueryRow(ctx,
		`SELECT true FROM addresses WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`,
		addressID, userID).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, rotationInvalid("address_id", "not_found", "choose one of your saved addresses")
	}
	if err != nil {
		return nil, fmt.Errorf("subscriptions: suggestion address: %w", err)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (b.id)
		       p.id, b.id, COALESCE(b.cuisines, '{}'), b.rating::float8, b.rating_count,
		       p.rotation_week_price_minor, p.currency, p.days_per_week, p.meals_per_day,
		       COALESCE(p.delivery_weekdays, '{}'), COALESCE(p.meal_slots, '{}'),
		       p.cutoff_hours, b.timezone, p.menu_cycle_id
		  FROM subscription_plans p
		  JOIN vendor_branches b ON b.id = p.branch_id
		  JOIN vendors v ON v.id = b.vendor_id
		  JOIN addresses a ON a.id = $1
		 WHERE p.service_type = $2 AND p.is_active AND p.rotation_enabled
		   AND p.rotation_week_price_minor IS NOT NULL
		   AND b.deleted_at IS NULL AND b.is_active
		   AND v.deleted_at IS NULL AND v.status = 'active'
		   AND b.service_area IS NOT NULL AND ST_Intersects(b.service_area, a.point)
		   AND EXISTS (SELECT 1 FROM time_windows tw WHERE tw.branch_id = b.id AND tw.is_active)
		   AND NOT EXISTS (SELECT 1 FROM meal_tastes t
		                    WHERE t.user_id = $3 AND t.subject_kind = 'kitchen'
		                      AND t.subject_id = b.id AND t.sentiment = 'avoid')
		 ORDER BY b.id, p.price_minor, p.id`, addressID, rotationServiceType, userID)
	if err != nil {
		return nil, fmt.Errorf("subscriptions: suggestion kitchens: %w", err)
	}
	defer rows.Close()
	out := []suggestCandidate{}
	for rows.Next() {
		var c suggestCandidate
		var weekdays []int16
		if err := rows.Scan(&c.PlanID, &c.BranchID, &c.Cuisines, &c.Rating, &c.RatingCount,
			&c.WeekPrice, &c.Currency, &c.DaysPerWeek, &c.MealsPerDay, &weekdays, &c.Slots,
			&c.CutoffHours, &c.Timezone, &c.MenuCycleID); err != nil {
			return nil, fmt.Errorf("subscriptions: scan suggestion kitchen: %w", err)
		}
		for _, d := range weekdays {
			c.Weekdays = append(c.Weekdays, int(d))
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func earliestRotationStart(pool []suggestCandidate) time.Time {
	cutoff := 0
	for _, c := range pool {
		cutoff = max(cutoff, c.CutoffHours)
	}
	loc, err := time.LoadLocation(pool[0].Timezone)
	if err != nil {
		loc = time.UTC
	}
	return EarliestStart(cutoff, loc, time.Now())
}

// suggestionPool keeps the well-rated kitchens that can deliver one shared
// schedule, choosing the schedule (when not given) that most kitchens serve.
// The pool is ranked by rating and capped, keeping a few kitchens of every
// cuisine so variety has room to work.
func suggestionPool(cands []suggestCandidate, weekdays []int, slots []string) ([]suggestCandidate, []int, []string) {
	type shape struct {
		currency    string
		days, meals int
	}
	groups := map[shape][]suggestCandidate{}
	shapes := []shape{}
	for _, c := range cands {
		if !c.wellRated() {
			continue
		}
		k := shape{c.Currency, c.DaysPerWeek, c.MealsPerDay}
		if _, ok := groups[k]; !ok {
			shapes = append(shapes, k)
		}
		groups[k] = append(groups[k], c)
	}

	var best []suggestCandidate
	var bestDays []int
	var bestSlots []string
	for _, k := range shapes {
		group := groups[k]
		days, meals := weekdays, slots
		if len(days) == 0 {
			days = commonWeekdays(group, k.days)
		}
		if len(meals) == 0 {
			meals = commonSlots(group, k.meals)
		}
		eligible := []suggestCandidate{}
		for _, c := range group {
			if c.serves(days, meals) {
				eligible = append(eligible, c)
			}
		}
		if len(eligible) > len(best) {
			best, bestDays, bestSlots = eligible, days, meals
		}
	}
	if bestDays == nil {
		bestDays, bestSlots = weekdays, slots
	}

	sort.SliceStable(best, func(i, j int) bool {
		if qi, qj := best[i].quality(), best[j].quality(); qi != qj {
			return qi > qj
		}
		return best[i].BranchID.String() < best[j].BranchID.String()
	})
	pool := []suggestCandidate{}
	perCuisine := map[string]int{}
	rest := []suggestCandidate{}
	for _, c := range best {
		key := "-" + c.BranchID.String()
		if cs := normalizeCuisines(c.Cuisines); len(cs) > 0 {
			key = cs[0]
		}
		if perCuisine[key] < poolPerCuisine && len(pool) < suggestPool {
			perCuisine[key]++
			pool = append(pool, c)
		} else {
			rest = append(rest, c)
		}
	}
	for _, c := range rest {
		if len(pool) >= suggestPool {
			break
		}
		pool = append(pool, c)
	}
	return pool, bestDays, bestSlots
}

func commonWeekdays(group []suggestCandidate, n int) []int {
	counts := map[int]int{}
	for _, c := range group {
		for d := 0; d < 7; d++ {
			if len(c.Weekdays) == 0 || containsInt(c.Weekdays, d) {
				counts[d]++
			}
		}
	}
	// Sunday to Thursday is the GCC working week; prefer it on ties.
	order := []int{0, 1, 2, 3, 4, 5, 6}
	sort.SliceStable(order, func(i, j int) bool { return counts[order[i]] > counts[order[j]] })
	out := append([]int{}, order[:min(n, len(order))]...)
	sort.Ints(out)
	return out
}

func commonSlots(group []suggestCandidate, n int) []string {
	counts := map[string]int{}
	for _, c := range group {
		for _, s := range c.Slots {
			counts[s]++
		}
	}
	order := []string{"lunch", "dinner", "breakfast", "snack", "iftar", "suhoor"}
	sort.SliceStable(order, func(i, j int) bool { return counts[order[i]] > counts[order[j]] })
	out := []string{}
	for _, s := range order {
		if len(out) < n && counts[s] > 0 {
			out = append(out, s)
		}
	}
	return out
}

// pickBalanced chooses four kitchens from the pool: most distinct cuisines,
// then one price band, then the best variety score, then the best ratings,
// then the lower total.
func pickBalanced(pool []suggestCandidate) []suggestCandidate {
	type pick struct {
		idx             [RotationKitchens]int
		distinct, score int
		inBand          bool
		quality         float64
		total           int64
		ok              bool
	}
	better := func(a, b pick) bool {
		if !b.ok {
			return true
		}
		if a.distinct != b.distinct {
			return a.distinct > b.distinct
		}
		if a.inBand != b.inBand {
			return a.inBand
		}
		if a.score != b.score {
			return a.score > b.score
		}
		if math.Abs(a.quality-b.quality) > 1e-9 {
			return a.quality > b.quality
		}
		return a.total < b.total
	}
	var best pick
	n := len(pool)
	for a := 0; a < n; a++ {
		for b := a + 1; b < n; b++ {
			for c := b + 1; c < n; c++ {
				for d := c + 1; d < n; d++ {
					p := pick{idx: [RotationKitchens]int{a, b, c, d}, ok: true}
					kitchens := make([]VarietyKitchen, 0, RotationKitchens)
					lo, hi := int64(math.MaxInt64), int64(0)
					for _, i := range p.idx {
						k := pool[i]
						kitchens = append(kitchens, VarietyKitchen{Key: k.BranchID.String(), Cuisines: k.Cuisines, Dishes: k.dishes})
						lo, hi = min(lo, k.WeekPrice), max(hi, k.WeekPrice)
						p.quality += k.quality()
						p.total += k.WeekPrice
					}
					v := ScoreVariety(kitchens)
					p.distinct, p.score = v.DistinctCuisines, v.Score
					p.inBand = float64(hi) <= float64(lo)*priceBandRatio
					if better(p, best) {
						best = p
					}
				}
			}
		}
	}
	out := make([]suggestCandidate, 0, RotationKitchens)
	if !best.ok {
		return out
	}
	for _, i := range best.idx {
		out = append(out, pool[i])
	}
	return out
}

func calendarDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
