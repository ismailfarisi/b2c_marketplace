package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/taste"
	"github.com/marketplace/api/internal/seam/servicetype"
)

// Reasons a kitchen is on the proposal; the apps word them.
const (
	ReasonLoved           = "loved"            // hearted, kept from this rotation
	ReasonTopRated        = "top_rated"        // the customer's own highest rating, kept
	ReasonKept            = "kept"             // kept because nothing better fit (or the price cap)
	ReasonNewCuisine      = "new_cuisine"      // a cuisine not eaten in the last two cycles
	ReasonFamiliarCuisine = "familiar_cuisine" // new kitchen in a cuisine the customer knows
	ReasonNewKitchen      = "new_kitchen"      // new kitchen, no cuisine tagged
)

// ProposalKitchen is one kitchen the next rotation could use, priced for one
// week of this rotation's days and meals.
type ProposalKitchen struct {
	BranchID      uuid.UUID `json:"branch_id"`
	PlanID        uuid.UUID `json:"plan_id"`
	KitchenName   string    `json:"kitchen_name"`
	KitchenNameAr *string   `json:"kitchen_name_ar"`
	Cuisines      []string  `json:"cuisines"`
	Rating        *float64  `json:"rating"`
	RatingCount   int       `json:"rating_count"`
	// WeekPriceMinor is the kitchen's price for one rotation week, tax in.
	WeekPriceMinor int64 `json:"week_price_minor"`
	// MealPriceMinor is WeekPriceMinor over the meals in a week: honest per-meal pricing.
	MealPriceMinor int64  `json:"meal_price_minor"`
	Currency       string `json:"currency"`
	// InCurrent: cooks in the rotation being renewed.
	InCurrent bool `json:"in_current"`
	// Favourite: the customer hearted this kitchen.
	Favourite bool `json:"favourite"`
	// Avoided: marked "Not again"; only a current kitchen can still be listed.
	Avoided bool `json:"avoided,omitempty"`
	// OwnRating is the customer's own review of the kitchen, 0 when none.
	OwnRating int `json:"own_rating"`
	// Reason says why it is proposed; empty on an eligible kitchen not proposed.
	Reason        string `json:"reason"`
	ReasonCuisine string `json:"reason_cuisine,omitempty"`
}

// RenewalProposal is the suggested next rotation and everything it could be
// swapped with.
type RenewalProposal struct {
	RotationID     uuid.UUID `json:"rotation_id"`
	StartDate      string    `json:"start_date"`
	Adventure      string    `json:"adventure"`
	RefreshOnRenew bool      `json:"refresh_on_renew"`
	// Kitchens are the four proposed, favourites first.
	Kitchens []ProposalKitchen `json:"kitchens"`
	// Eligible is every kitchen the customer may swap in, current ones included.
	Eligible            []ProposalKitchen `json:"eligible"`
	AmountMinor         int64             `json:"amount_minor"`
	PreviousAmountMinor int64             `json:"previous_amount_minor"`
	Currency            string            `json:"currency"`
	MealsPerWeek        int               `json:"meals_per_week"`
}

// tasteContext is what the ranking knows about the customer.
type tasteContext struct {
	adventure string
	// familiar: cuisines the customer has eaten or hearted.
	familiar map[string]bool
	// recent: cuisines eaten in the last two cycles.
	recent  map[string]bool
	avoided map[uuid.UUID]bool
}

// MaxKept is how many of the current kitchens a proposal keeps at most.
const MaxKept = 2

// renewalPriceCapBP: a refreshed auto-renewal may cost this much of the last one.
const renewalPriceCapBP = 11000

func ratingOf(k ProposalKitchen) float64 {
	if k.Rating == nil {
		return 0
	}
	return *k.Rating
}

func (k ProposalKitchen) hasAny(set map[string]bool) bool {
	for _, c := range k.Cuisines {
		if set[c] {
			return true
		}
	}
	return false
}

// firstIn is the cuisine the reason text names for a familiar kitchen: one
// the customer actually knows, not whichever tag the kitchen listed first.
func (k ProposalKitchen) firstIn(set map[string]bool) string {
	for _, c := range k.Cuisines {
		if set[c] {
			return c
		}
	}
	if len(k.Cuisines) > 0 {
		return k.Cuisines[0]
	}
	return ""
}

func (k ProposalKitchen) firstNotIn(set map[string]bool) string {
	for _, c := range k.Cuisines {
		if !set[c] {
			return c
		}
	}
	return ""
}

// proposeKitchens picks the next four: up to MaxKept favourites from the
// current rotation (hearted first, then the customer's own highest rated),
// then fresh kitchens ranked by rating, nudged by the adventure setting and
// spread across cuisines. Avoided kitchens are never proposed unless nothing
// else can fill the rotation.
func proposeKitchens(current, others []ProposalKitchen, tc tasteContext) []ProposalKitchen {
	picked := make([]ProposalKitchen, 0, RotationKitchens)
	has := map[uuid.UUID]bool{}
	add := func(k ProposalKitchen, reason, cuisine string) {
		k.Reason, k.ReasonCuisine = reason, cuisine
		picked = append(picked, k)
		has[k.BranchID] = true
	}

	keep := make([]ProposalKitchen, 0, len(current))
	for _, k := range current {
		if !tc.avoided[k.BranchID] && (k.Favourite || k.OwnRating > 0) {
			keep = append(keep, k)
		}
	}
	sort.SliceStable(keep, func(i, j int) bool {
		a, b := keep[i], keep[j]
		if a.Favourite != b.Favourite {
			return a.Favourite
		}
		if a.OwnRating != b.OwnRating {
			return a.OwnRating > b.OwnRating
		}
		return ratingOf(a) > ratingOf(b)
	})
	for _, k := range keep {
		if len(picked) == MaxKept {
			break
		}
		if k.Favourite {
			add(k, ReasonLoved, "")
		} else {
			add(k, ReasonTopRated, "")
		}
	}

	fresh := make([]ProposalKitchen, 0, len(others))
	for _, k := range others {
		if !k.InCurrent && !tc.avoided[k.BranchID] && !has[k.BranchID] {
			fresh = append(fresh, k)
		}
	}
	for len(picked) < RotationKitchens && len(fresh) > 0 {
		cuisines := map[string]int{}
		for _, p := range picked {
			for _, c := range p.Cuisines {
				cuisines[c]++
			}
		}
		// balanced alternates: a known cuisine, then a new one.
		wantFamiliar := tc.adventure == taste.Familiar ||
			(tc.adventure == taste.Balanced && (len(picked)-len(keep))%2 == 0)
		wantNew := tc.adventure == taste.Surprise ||
			(tc.adventure == taste.Balanced && !wantFamiliar)
		best, bestScore := -1, 0.0
		for i, k := range fresh {
			score := ratingOf(k) + float64(min(k.RatingCount, 20))/100
			if wantFamiliar && k.hasAny(tc.familiar) {
				score += 3
			}
			if wantNew && !k.hasAny(tc.recent) {
				score += 3
			}
			for _, c := range k.Cuisines {
				score -= 2 * float64(cuisines[c])
			}
			if best < 0 || score > bestScore ||
				(score == bestScore && k.KitchenName < fresh[best].KitchenName) {
				best, bestScore = i, score
			}
		}
		k := fresh[best]
		fresh = slices.Delete(fresh, best, best+1)
		switch {
		case len(k.Cuisines) == 0:
			add(k, ReasonNewKitchen, "")
		case !k.hasAny(tc.recent):
			add(k, ReasonNewCuisine, k.firstNotIn(tc.recent))
		default:
			add(k, ReasonFamiliarCuisine, k.firstIn(tc.familiar))
		}
	}

	// Not enough fresh kitchens: the current ones stay, best rated first,
	// avoided ones only as a last resort.
	rest := make([]ProposalKitchen, 0, len(current))
	for _, k := range current {
		if !has[k.BranchID] {
			rest = append(rest, k)
		}
	}
	sort.SliceStable(rest, func(i, j int) bool {
		a, b := rest[i], rest[j]
		if tc.avoided[a.BranchID] != tc.avoided[b.BranchID] {
			return !tc.avoided[a.BranchID]
		}
		return ratingOf(a) > ratingOf(b)
	})
	for _, k := range rest {
		if len(picked) == RotationKitchens {
			break
		}
		add(k, ReasonKept, "")
	}
	return picked
}

func proposalTotal(ks []ProposalKitchen) int64 {
	var t int64
	for _, k := range ks {
		t += k.WeekPriceMinor
	}
	return t
}

// capProposal keeps a refreshed renewal within renewalPriceCapBP of what the
// customer paid last time. Over the cap, the dearest fresh kitchen gives way to
// a cheaper eligible one that fits, or failing that to the current kitchen it
// replaced; current kitchens are never swapped by the cap.
func capProposal(proposed, current, pool []ProposalKitchen, previousTotal int64, tc tasteContext) []ProposalKitchen {
	cap := previousTotal * renewalPriceCapBP / 10000
	out := slices.Clone(proposed)
	for proposalTotal(out) > cap {
		in := map[uuid.UUID]bool{}
		for _, k := range out {
			in[k.BranchID] = true
		}
		dear := -1
		for i, k := range out {
			if !k.InCurrent && (dear < 0 || k.WeekPriceMinor > out[dear].WeekPriceMinor) {
				dear = i
			}
		}
		if dear < 0 {
			break
		}
		over := proposalTotal(out) - cap
		best := -1
		for i, k := range pool {
			if k.InCurrent || in[k.BranchID] || tc.avoided[k.BranchID] ||
				k.WeekPriceMinor >= out[dear].WeekPriceMinor ||
				out[dear].WeekPriceMinor-k.WeekPriceMinor < over {
				continue
			}
			if best < 0 || ratingOf(k) > ratingOf(pool[best]) {
				best = i
			}
		}
		if best >= 0 {
			k := pool[best]
			k.Reason, k.ReasonCuisine = out[dear].Reason, ""
			if !k.hasAny(tc.recent) {
				k.ReasonCuisine = k.firstNotIn(tc.recent)
			} else if len(k.Cuisines) > 0 {
				k.Reason, k.ReasonCuisine = ReasonFamiliarCuisine, k.firstIn(tc.familiar)
			}
			out[dear] = k
			continue
		}
		back := -1
		for i, k := range current {
			if !in[k.BranchID] && !tc.avoided[k.BranchID] {
				back = i
				break
			}
		}
		if back < 0 {
			for i, k := range current {
				if !in[k.BranchID] {
					back = i
					break
				}
			}
		}
		if back < 0 {
			break
		}
		k := current[back]
		k.Reason, k.ReasonCuisine = ReasonKept, ""
		out[dear] = k
	}
	return out
}

// RenewOptions shape a renewal.
type RenewOptions struct {
	// PlanIDs is the customer's edited proposal: exactly four plans, checked
	// like a new rotation. Empty keeps the default.
	PlanIDs []uuid.UUID
	// Refresh is set by the worker; kept for callers, the rotation's own
	// refresh_on_renew is what decides (see renewalPlans).
	Refresh bool
}

// renewalPending says the rotation's renewal exists but has not been paid for:
// the customer may still edit it (the edited renewal replaces it).
func renewalPending(r Rotation) bool {
	return r.RenewedByID != nil && r.RenewedByStatus != nil && *r.RenewedByStatus == "pending_payment"
}

// renewalPlans decides which four plans a renewal uses. propose is only called
// when the rotation refreshes; with refresh off, the same kitchens come back.
// The rotation's own refresh_on_renew decides, whoever renews it: a customer
// tapping "Renew" gets the fresh kitchens the setting promised.
func renewalPlans(r Rotation, opts RenewOptions, propose func() ([]uuid.UUID, error)) ([]uuid.UUID, error) {
	if len(opts.PlanIDs) > 0 {
		return opts.PlanIDs, nil
	}
	if r.RefreshOnRenew {
		if ids, err := propose(); err == nil && len(ids) == RotationKitchens {
			return ids, nil
		} else if err != nil {
			return nil, err
		}
	}
	plans := make([]uuid.UUID, 0, RotationKitchens)
	for _, w := range r.Weeks {
		plans = append(plans, w.PlanID)
	}
	return plans, nil
}

// renewalWindow is when the next rotation starts and which kitchen may not
// cook its first week: the day after this one ends with last week's kitchen
// barred, or tomorrow with no bar when the rotation has already lapsed.
func renewalWindow(r Rotation, now time.Time, loc *time.Location) (time.Time, uuid.UUID, error) {
	end, err := time.ParseInLocation("2006-01-02", r.EndDate, loc)
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("subscriptions: rotation end: %w", err)
	}
	start := end.AddDate(0, 0, 1)
	notFirst := r.Weeks[RotationKitchens-1].BranchID
	if tomorrow := dayIn(now, loc).AddDate(0, 0, 1); start.Before(tomorrow) {
		start, notFirst = tomorrow, uuid.Nil
	}
	return start, notFirst, nil
}

// rotationContext loads the address and time zone a rotation delivers to.
func (s *Service) rotationContext(ctx context.Context, id uuid.UUID) (uuid.UUID, *time.Location, error) {
	var addressID uuid.UUID
	var tz string
	if err := s.pool.QueryRow(ctx, `
		SELECT r.address_id, s.timezone FROM meal_rotations r
		  JOIN subscriptions s ON s.rotation_id = r.id
		 WHERE r.id = $1 ORDER BY s.rotation_week LIMIT 1`, id).Scan(&addressID, &tz); err != nil {
		return uuid.Nil, nil, fmt.Errorf("subscriptions: rotation address: %w", err)
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	return addressID, loc, nil
}

// weekQuote prices one rotation week of a plan the way the rotation does.
func (s *Service) weekQuote(ctx context.Context, p PlanTemplate) (servicetype.QuoteResult, error) {
	vertical, err := s.verticals.Get(p.ServiceType)
	if err != nil {
		return servicetype.QuoteResult{}, fmt.Errorf("%w: service type %q", httpx.ErrNotFound, p.ServiceType)
	}
	return vertical.Pricing.Quote(ctx, servicetype.PricingContext{
		BranchID: p.BranchID, ServiceType: p.ServiceType, Currency: p.Currency,
		Lines: []servicetype.PricingLine{{
			OfferingID: p.ID, Name: p.Name,
			UnitPriceMinor: *p.RotationWeekPriceMinor,
			TaxRateBP:      p.TaxRateBP, Quantity: 1,
		}},
	})
}

// proposalKitchen loads and prices one plan for the proposal.
func (s *Service) proposalKitchen(ctx context.Context, tx pgx.Tx, planID uuid.UUID, mealsPerWeek int) (ProposalKitchen, error) {
	p, err := s.repo.LoadPlan(ctx, tx, planID, uuid.NullUUID{})
	if err != nil {
		return ProposalKitchen{}, err
	}
	if !p.RotationEnabled || p.RotationWeekPriceMinor == nil {
		return ProposalKitchen{}, httpx.ErrNotFound
	}
	k := ProposalKitchen{BranchID: p.BranchID, PlanID: p.ID, Currency: p.Currency}
	if err := tx.QueryRow(ctx, `
		SELECT name, name_ar, cuisines, rating::float8, rating_count
		  FROM vendor_branches WHERE id = $1`, p.BranchID,
	).Scan(&k.KitchenName, &k.KitchenNameAr, &k.Cuisines, &k.Rating, &k.RatingCount); err != nil {
		return ProposalKitchen{}, fmt.Errorf("subscriptions: proposal kitchen: %w", err)
	}
	if k.Cuisines == nil {
		k.Cuisines = []string{}
	}
	q, err := s.weekQuote(ctx, p)
	if err != nil {
		return ProposalKitchen{}, fmt.Errorf("subscriptions: proposal quote: %w", err)
	}
	k.WeekPriceMinor = q.TotalMinor
	if mealsPerWeek > 0 {
		k.MealPriceMinor = q.TotalMinor / int64(mealsPerWeek)
	}
	return k, nil
}

// RenewalProposal suggests the next four kitchens for a rotation and lists
// every kitchen it could be swapped with.
func (s *Service) RenewalProposal(ctx context.Context, userID, id uuid.UUID, now time.Time) (RenewalProposal, error) {
	r, err := s.MyRotation(ctx, userID, id)
	if err != nil {
		return RenewalProposal{}, err
	}
	if r.RenewedByID != nil && !renewalPending(r) {
		return RenewalProposal{}, httpx.Coded(httpx.ErrConflict, "already_renewed", "This rotation has already been renewed.")
	}
	if r.Status == "pending_payment" {
		return RenewalProposal{}, httpx.Coded(httpx.ErrConflict, "not_paid", "Pay for this rotation before renewing it.")
	}
	if !rotationRenewable(r.Status) || len(r.Weeks) != RotationKitchens {
		return RenewalProposal{}, httpx.Coded(httpx.ErrConflict, "not_renewable", "This rotation cannot be renewed.")
	}
	addressID, loc, err := s.rotationContext(ctx, id)
	if err != nil {
		return RenewalProposal{}, err
	}
	start, _, err := renewalWindow(r, now, loc)
	if err != nil {
		return RenewalProposal{}, err
	}
	return s.propose(ctx, r, userID, addressID, start)
}

func (s *Service) propose(ctx context.Context, r Rotation, userID, addressID uuid.UUID, start time.Time) (RenewalProposal, error) {
	out := RenewalProposal{
		RotationID: r.ID, StartDate: start.Format("2006-01-02"), Currency: r.Currency,
		RefreshOnRenew: r.RefreshOnRenew, PreviousAmountMinor: r.AmountMinor + r.DiscountMinor,
		MealsPerWeek: len(r.Weekdays) * len(r.MealSlots), Adventure: taste.Balanced,
	}
	tc := tasteContext{adventure: taste.Balanced, familiar: map[string]bool{}, recent: map[string]bool{}, avoided: map[uuid.UUID]bool{}}
	favourites := map[uuid.UUID]bool{}
	ownRating := map[uuid.UUID]int{}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, fmt.Errorf("subscriptions: proposal tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := tx.QueryRow(ctx, `SELECT adventure FROM meal_preferences WHERE user_id = $1`, userID).Scan(&tc.adventure); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, fmt.Errorf("subscriptions: adventure: %w", err)
	}
	out.Adventure = tc.adventure

	rows, err := tx.Query(ctx, `
		SELECT t.subject_id, t.sentiment, b.cuisines
		  FROM meal_tastes t JOIN vendor_branches b ON b.id = t.subject_id
		 WHERE t.user_id = $1 AND t.subject_kind = 'kitchen'`, userID)
	if err != nil {
		return out, fmt.Errorf("subscriptions: kitchen tastes: %w", err)
	}
	for rows.Next() {
		var bid uuid.UUID
		var sentiment string
		var cuisines []string
		if err := rows.Scan(&bid, &sentiment, &cuisines); err != nil {
			rows.Close()
			return out, fmt.Errorf("subscriptions: scan taste: %w", err)
		}
		if sentiment == taste.Avoid {
			tc.avoided[bid] = true
			continue
		}
		favourites[bid] = true
		for _, c := range cuisines {
			tc.familiar[c] = true
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}

	// Cuisines eaten: every paid-for kitchen ever is familiar; this rotation
	// and the one before it are the last two cycles.
	rows, err = tx.Query(ctx, `
		SELECT b.cuisines, s.rotation_id IN ($2, (SELECT renewed_from_id FROM meal_rotations WHERE id = $2))
		  FROM subscriptions s JOIN vendor_branches b ON b.id = s.branch_id
		 WHERE s.user_id = $1 AND s.status IN ('active', 'completed', 'suspended', 'cancelled')
		   AND EXISTS (SELECT 1 FROM subscription_cycles c WHERE c.subscription_id = s.id AND c.status = 'paid')
		   -- A week swapped away before it started was never eaten.
		   AND NOT EXISTS (SELECT 1 FROM meal_rotation_swaps sw WHERE sw.old_subscription_id = s.id)`,
		userID, r.ID)
	if err != nil {
		return out, fmt.Errorf("subscriptions: eaten cuisines: %w", err)
	}
	for rows.Next() {
		var cuisines []string
		var recent *bool
		if err := rows.Scan(&cuisines, &recent); err != nil {
			rows.Close()
			return out, fmt.Errorf("subscriptions: scan eaten: %w", err)
		}
		for _, c := range cuisines {
			tc.familiar[c] = true
			if recent != nil && *recent {
				tc.recent[c] = true
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}

	currentBranches := make([]uuid.UUID, 0, RotationKitchens)
	for _, w := range r.Weeks {
		currentBranches = append(currentBranches, w.BranchID)
	}
	rows, err = tx.Query(ctx, `
		SELECT branch_id, max(rating)::int FROM kitchen_reviews
		 WHERE user_id = $1 AND branch_id = ANY($2) AND status = 'published'
		 GROUP BY branch_id`, userID, currentBranches)
	if err != nil {
		return out, fmt.Errorf("subscriptions: own ratings: %w", err)
	}
	for rows.Next() {
		var bid uuid.UUID
		var rating int
		if err := rows.Scan(&bid, &rating); err != nil {
			rows.Close()
			return out, fmt.Errorf("subscriptions: scan own rating: %w", err)
		}
		ownRating[bid] = rating
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}

	current := make([]ProposalKitchen, 0, RotationKitchens)
	var serviceType string
	for _, w := range r.Weeks {
		k, err := s.proposalKitchen(ctx, tx, w.PlanID, out.MealsPerWeek)
		if errors.Is(err, httpx.ErrNotFound) {
			// A plan taken off the rotation cannot come back; it is simply not eligible.
			continue
		}
		if err != nil {
			return out, err
		}
		if serviceType == "" {
			p, err := s.repo.LoadPlan(ctx, tx, w.PlanID, uuid.NullUUID{})
			if err != nil {
				return out, err
			}
			serviceType = p.ServiceType
		}
		k.InCurrent, k.Favourite, k.OwnRating = true, favourites[k.BranchID], ownRating[k.BranchID]
		current = append(current, k)
	}

	weekdays := make([]int, 0, len(r.Weekdays))
	for _, d := range r.Weekdays {
		weekdays = append(weekdays, d)
	}
	rows, err = tx.Query(ctx, `
		SELECT DISTINCT ON (b.id) p.id
		  FROM subscription_plans p
		  JOIN vendor_branches b ON b.id = p.branch_id AND b.deleted_at IS NULL AND b.is_active
		  JOIN vendors v ON v.id = b.vendor_id AND v.deleted_at IS NULL AND v.status = 'active'
		  JOIN addresses a ON a.id = $1
		 WHERE p.is_active AND p.service_type = $2 AND p.rotation_enabled
		   AND p.rotation_week_price_minor IS NOT NULL
		   AND p.delivery_weekdays::int[] @> $3::int[]
		   AND p.meal_slots @> $4::text[]
		   AND b.service_area IS NOT NULL AND ST_Intersects(b.service_area, a.point)
		   AND EXISTS (SELECT 1 FROM time_windows w WHERE w.branch_id = b.id AND w.is_active)
		   AND NOT (b.id = ANY($5))
		   AND NOT EXISTS (SELECT 1 FROM meal_tastes t
		                    WHERE t.user_id = $6 AND t.subject_kind = 'kitchen'
		                      AND t.subject_id = b.id AND t.sentiment = 'avoid')
		 ORDER BY b.id, p.rotation_week_price_minor, p.created_at`,
		addressID, serviceType, weekdays, r.MealSlots, currentBranches, userID)
	if err != nil {
		return out, fmt.Errorf("subscriptions: eligible kitchens: %w", err)
	}
	var planIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return out, fmt.Errorf("subscriptions: scan eligible: %w", err)
		}
		planIDs = append(planIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}
	others := make([]ProposalKitchen, 0, len(planIDs))
	for _, id := range planIDs {
		k, err := s.proposalKitchen(ctx, tx, id, out.MealsPerWeek)
		if errors.Is(err, httpx.ErrNotFound) {
			continue
		}
		if err != nil {
			return out, err
		}
		k.Favourite = favourites[k.BranchID]
		others = append(others, k)
	}
	sort.SliceStable(others, func(i, j int) bool {
		a, b := others[i], others[j]
		if ratingOf(a) != ratingOf(b) {
			return ratingOf(a) > ratingOf(b)
		}
		if a.RatingCount != b.RatingCount {
			return a.RatingCount > b.RatingCount
		}
		return strings.ToLower(a.KitchenName) < strings.ToLower(b.KitchenName)
	})

	out.Kitchens = proposeKitchens(current, others, tc)
	out.Eligible = append(append([]ProposalKitchen{}, current...), others...)
	for i := range out.Eligible {
		out.Eligible[i].Reason, out.Eligible[i].ReasonCuisine = "", ""
		out.Eligible[i].Avoided = tc.avoided[out.Eligible[i].BranchID]
	}
	for i := range out.Kitchens {
		out.Kitchens[i].Avoided = tc.avoided[out.Kitchens[i].BranchID]
	}
	out.AmountMinor = proposalTotal(out.Kitchens)
	if len(out.Kitchens) > 0 {
		out.Currency = out.Kitchens[0].Currency
	}
	return out, nil
}

// cappedProposalPlans is the proposal the worker renews with: within the
// price cap, or the previous kitchens where it is not.
func (s *Service) cappedProposalPlans(ctx context.Context, r Rotation, userID, addressID uuid.UUID, start time.Time) ([]uuid.UUID, error) {
	p, err := s.propose(ctx, r, userID, addressID, start)
	if err != nil {
		return nil, err
	}
	if len(p.Kitchens) != RotationKitchens {
		return nil, nil
	}
	var current []ProposalKitchen
	for _, k := range p.Eligible {
		if k.InCurrent {
			current = append(current, k)
		}
	}
	tc := tasteContext{adventure: p.Adventure, familiar: map[string]bool{}, recent: map[string]bool{}, avoided: map[uuid.UUID]bool{}}
	capped := capProposal(p.Kitchens, current, p.Eligible, p.PreviousAmountMinor, tc)
	ids := make([]uuid.UUID, 0, RotationKitchens)
	for _, k := range capped {
		ids = append(ids, k.PlanID)
	}
	return ids, nil
}
