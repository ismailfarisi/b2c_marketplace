package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/promo"
	"github.com/marketplace/api/internal/platform/postgres"
	"github.com/marketplace/api/internal/seam/servicetype"
)

// RotationKitchens is how many kitchens a rotation has, one per week.
const RotationKitchens = 4

// RotationInput is a customer's four kitchens and the schedule all four cook.
type RotationInput struct {
	UserID    uuid.UUID
	AddressID uuid.UUID
	PlanIDs   []uuid.UUID
	StartDate time.Time
	Weekdays  []int
	Slots     []string
	Promo     promo.Ref
}

// RotationWeek is one week of a rotation and the kitchen the platform gave it.
type RotationWeek struct {
	Week           int        `json:"week"`
	StartsOn       string     `json:"starts_on"`
	EndsOn         string     `json:"ends_on"`
	BranchID       uuid.UUID  `json:"branch_id"`
	KitchenName    string     `json:"kitchen_name"`
	KitchenNameAr  *string    `json:"kitchen_name_ar"`
	PlanID         uuid.UUID  `json:"plan_id"`
	PlanName       string     `json:"plan_name"`
	PriceMinor     int64      `json:"price_minor"`
	SubscriptionID *uuid.UUID `json:"subscription_id"`
	SubscriptionNo *string    `json:"subscription_no"`
	Status         *string    `json:"status"`
}

// RotationQuote is what the customer sees before paying: who cooks which week
// and what each week costs.
type RotationQuote struct {
	StartDate   string         `json:"start_date"`
	EndDate     string         `json:"end_date"`
	Weeks       []RotationWeek `json:"weeks"`
	AmountMinor int64          `json:"amount_minor"`
	// DiscountMinor is already taken off AmountMinor; week prices are before it.
	DiscountMinor int64        `json:"discount_minor"`
	Promo         *promo.Offer `json:"promo,omitempty"`
	Currency      string       `json:"currency"`
}

// Rotation is a purchased rotation.
type Rotation struct {
	ID         uuid.UUID `json:"id"`
	RotationNo string    `json:"rotation_no"`
	Status     string    `json:"status"`
	MealSlots  []string  `json:"meal_slots"`
	Weekdays   []int     `json:"delivery_weekdays"`
	CreatedAt  time.Time `json:"created_at"`
	// AutoRenew bills the next rotation, with the same four kitchens, before
	// this one ends.
	AutoRenew     bool       `json:"auto_renew"`
	RenewedFromID *uuid.UUID `json:"renewed_from_id"`
	// The rotation that renews this one, and its status.
	RenewedByID     *uuid.UUID `json:"renewed_by_id"`
	RenewedByStatus *string    `json:"renewed_by_status"`
	RotationQuote
}

type rotationPlan struct {
	PlanTemplate
	KitchenName   string
	KitchenNameAr *string
	Rules         []Rule
	Quote         servicetype.QuoteResult
}

func rotationInvalid(field, code, msg string) error {
	return &httpx.ValidationError{Fields: []httpx.FieldError{{Field: field, Code: code, Message: msg}}}
}

// prepareRotation loads and checks the four plans: each in the rotation, four
// different kitchens, every one able to cook the chosen days and meals, and
// every one delivering to the address.
func (s *Service) prepareRotation(ctx context.Context, tx pgx.Tx, in RotationInput) ([]rotationPlan, time.Time, *time.Location, error) {
	if len(in.PlanIDs) != RotationKitchens {
		return nil, time.Time{}, nil, rotationInvalid("plan_ids", "four_kitchens",
			fmt.Sprintf("a rotation needs exactly %d kitchens", RotationKitchens))
	}
	if len(in.Weekdays) == 0 || len(in.Slots) == 0 {
		return nil, time.Time{}, nil, rotationInvalid("schedule", "required",
			"choose the delivery days and meals")
	}

	plans := make([]rotationPlan, 0, RotationKitchens)
	branches := map[uuid.UUID]bool{}
	for _, id := range in.PlanIDs {
		p, err := s.repo.LoadPlan(ctx, tx, id, uuid.NullUUID{})
		if errors.Is(err, httpx.ErrNotFound) {
			return nil, time.Time{}, nil, rotationInvalid("plan_ids", "plan_unavailable",
				"one of the chosen plans is no longer on sale")
		}
		if err != nil {
			return nil, time.Time{}, nil, err
		}
		if !p.RotationEnabled || p.RotationWeekPriceMinor == nil {
			return nil, time.Time{}, nil, rotationInvalid("plan_ids", "not_in_rotation",
				fmt.Sprintf("%s is not part of the rotation", p.Name))
		}
		if branches[p.BranchID] {
			return nil, time.Time{}, nil, rotationInvalid("plan_ids", "same_kitchen_twice",
				"choose four different kitchens")
		}
		branches[p.BranchID] = true
		if len(plans) > 0 && (p.Currency != plans[0].Currency || p.ServiceType != plans[0].ServiceType) {
			return nil, time.Time{}, nil, rotationInvalid("plan_ids", "incompatible_plans",
				"these plans cannot be combined in one rotation")
		}
		rp := rotationPlan{PlanTemplate: p}
		if err := tx.QueryRow(ctx,
			`SELECT name, name_ar FROM vendor_branches WHERE id = $1`, p.BranchID,
		).Scan(&rp.KitchenName, &rp.KitchenNameAr); err != nil {
			return nil, time.Time{}, nil, fmt.Errorf("subscriptions: rotation kitchen: %w", err)
		}
		plans = append(plans, rp)
	}

	loc, lerr := time.LoadLocation(plans[0].Timezone)
	if lerr != nil {
		loc = time.UTC
	}
	start := dayIn(in.StartDate, loc)
	if start.Before(dayIn(time.Now(), loc)) {
		return nil, time.Time{}, nil, rotationInvalid("start_date", "in_the_past",
			"a rotation cannot start before today")
	}

	for i := range plans {
		p := &plans[i]
		var window uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT id FROM time_windows
			 WHERE branch_id = $1 AND is_active
			 ORDER BY sort_order, starts_at LIMIT 1`, p.BranchID).Scan(&window)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, time.Time{}, nil, rotationInvalid("plan_ids", "no_delivery_window",
				fmt.Sprintf("%s has no delivery time set up", p.KitchenName))
		}
		if err != nil {
			return nil, time.Time{}, nil, fmt.Errorf("subscriptions: rotation window: %w", err)
		}
		for _, d := range in.Weekdays {
			for _, slot := range in.Slots {
				p.Rules = append(p.Rules, Rule{Weekday: Weekday(d), Slot: slot, WindowID: window})
			}
		}
		if err := validateRules(p.Rules, p.PlanTemplate); err != nil {
			var ve *httpx.ValidationError
			if errors.As(err, &ve) {
				for j := range ve.Fields {
					ve.Fields[j].Message = p.KitchenName + ": " + ve.Fields[j].Message
				}
			}
			return nil, time.Time{}, nil, err
		}
		if err := s.checkAddresses(ctx, tx, SubscribeInput{
			UserID: in.UserID, AddressID: in.AddressID, Rules: p.Rules,
		}, p.BranchID); err != nil {
			return nil, time.Time{}, nil, err
		}

		vertical, err := s.verticals.Get(p.ServiceType)
		if err != nil {
			return nil, time.Time{}, nil, fmt.Errorf("%w: service type %q", httpx.ErrNotFound, p.ServiceType)
		}
		p.Quote, err = vertical.Pricing.Quote(ctx, servicetype.PricingContext{
			BranchID: p.BranchID, ServiceType: p.ServiceType, Currency: p.Currency,
			Lines: []servicetype.PricingLine{{
				OfferingID: p.ID, Name: p.Name,
				UnitPriceMinor: *p.RotationWeekPriceMinor,
				TaxRateBP:      p.TaxRateBP, Quantity: 1,
			}},
		})
		if err != nil {
			return nil, time.Time{}, nil, fmt.Errorf("subscriptions: rotation quote: %w", err)
		}
	}
	return plans, start, loc, nil
}

// rotationLoad counts, per chosen kitchen and per rotation week, how many other
// rotation weeks that kitchen is already cooking in the same seven days.
func rotationLoad(ctx context.Context, tx pgx.Tx, plans []rotationPlan, start time.Time) ([RotationKitchens][RotationKitchens]int, error) {
	var load [RotationKitchens][RotationKitchens]int
	for k, p := range plans {
		for w := 0; w < RotationKitchens; w++ {
			from := start.AddDate(0, 0, 7*w)
			to := from.AddDate(0, 0, 6)
			if err := tx.QueryRow(ctx, `
				SELECT count(*) FROM subscriptions
				 WHERE branch_id = $1 AND rotation_id IS NOT NULL
				   AND status IN ('pending_payment', 'active', 'paused')
				   AND start_date <= $3 AND COALESCE(end_date, start_date) >= $2`,
				p.BranchID, from, to).Scan(&load[k][w]); err != nil {
				return load, fmt.Errorf("subscriptions: rotation load: %w", err)
			}
		}
	}
	return load, nil
}

// AssignWeeks picks which kitchen cooks which week: the order that keeps the
// busiest kitchen-week as light as possible, then the lightest overall. Ties
// keep the customer's own order. order[w] is the kitchen index for week w.
func AssignWeeks(load [RotationKitchens][RotationKitchens]int) [RotationKitchens]int {
	return AssignWeeksAvoiding(load, -1)
}

// AssignWeeksAvoiding is AssignWeeks with kitchen index notFirst kept out of
// week 1, so a renewal never has the same kitchen cook two weeks in a row.
// Pass -1 for no restriction.
func AssignWeeksAvoiding(load [RotationKitchens][RotationKitchens]int, notFirst int) [RotationKitchens]int {
	best := [RotationKitchens]int{0, 1, 2, 3}
	if notFirst == 0 {
		best = [RotationKitchens]int{1, 0, 2, 3}
	}
	bestMax, bestSum := -1, -1
	var perm [RotationKitchens]int
	used := [RotationKitchens]bool{}
	var walk func(w int)
	walk = func(w int) {
		if w == RotationKitchens {
			mx, sum := 0, 0
			for week, k := range perm {
				l := load[k][week]
				sum += l
				mx = max(mx, l)
			}
			if bestMax < 0 || mx < bestMax || (mx == bestMax && sum < bestSum) {
				best, bestMax, bestSum = perm, mx, sum
			}
			return
		}
		for k := 0; k < RotationKitchens; k++ {
			if used[k] || (w == 0 && k == notFirst) {
				continue
			}
			used[k], perm[w] = true, k
			walk(w + 1)
			used[k] = false
		}
	}
	walk(0)
	return best
}

func buildQuote(plans []rotationPlan, order [RotationKitchens]int, start time.Time) RotationQuote {
	q := RotationQuote{
		StartDate: start.Format("2006-01-02"),
		EndDate:   start.AddDate(0, 0, 7*RotationKitchens-1).Format("2006-01-02"),
		Currency:  plans[0].Currency,
		Weeks:     make([]RotationWeek, 0, RotationKitchens),
	}
	for w, k := range order {
		p := plans[k]
		from := start.AddDate(0, 0, 7*w)
		q.Weeks = append(q.Weeks, RotationWeek{
			Week: w + 1, StartsOn: from.Format("2006-01-02"),
			EndsOn:   from.AddDate(0, 0, 6).Format("2006-01-02"),
			BranchID: p.BranchID, KitchenName: p.KitchenName, KitchenNameAr: p.KitchenNameAr,
			PlanID: p.ID, PlanName: p.Name, PriceMinor: p.Quote.TotalMinor,
		})
		q.AmountMinor += p.Quote.TotalMinor
	}
	return q
}

// rotationPromo resolves the customer's promo for these four kitchens and
// takes the discount off the quote and off each kitchen's own billing quote,
// split in proportion to the kitchens' prices. Returns per-plan discount and
// platform subsidy, indexed like plans.
func (s *Service) rotationPromo(ctx context.Context, tx pgx.Tx, in RotationInput, plans []rotationPlan,
	q *RotationQuote) (*promo.Promo, []int64, []int64, error) {
	if in.Promo.Empty() {
		return nil, nil, nil, nil
	}
	if s.promos == nil {
		return nil, nil, nil, rotationInvalid("promo_code", "unavailable", "promo codes are not available")
	}
	branches := make([]uuid.UUID, len(plans))
	weights := make([]int64, len(plans))
	var subtotal int64
	for i, p := range plans {
		branches[i] = p.BranchID
		weights[i] = p.Quote.SubtotalMinor
		subtotal += p.Quote.SubtotalMinor
	}
	pr, err := s.promos.Resolve(ctx, tx, in.Promo, in.UserID, branches, promo.ForRotations)
	if err != nil {
		return nil, nil, nil, err
	}
	discount := pr.Discount(subtotal)
	shares := promo.Spread(discount, weights)
	subsidies := make([]int64, len(plans))
	for i := range plans {
		subsidies[i] = pr.PlatformFunded(shares[i])
		promo.ApplyToQuote(&plans[i].Quote, shares[i], plans[i].TaxRateBP)
	}
	q.DiscountMinor = discount
	q.AmountMinor -= discount
	q.Promo = pr.Offer(in.Promo.Code != "")
	return pr, shares, subsidies, nil
}

// PreviewRotation shows the week order and prices without creating anything.
func (s *Service) PreviewRotation(ctx context.Context, in RotationInput) (RotationQuote, error) {
	var q RotationQuote
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		plans, start, _, err := s.prepareRotation(ctx, tx, in)
		if err != nil {
			return err
		}
		load, err := rotationLoad(ctx, tx, plans, start)
		if err != nil {
			return err
		}
		q = buildQuote(plans, AssignWeeks(load), start)
		_, _, _, err = s.rotationPromo(ctx, tx, in, plans, &q)
		return err
	})
	return q, err
}

// CreateRotation creates the rotation and its four weekly subscriptions, each
// with its own billing order at that kitchen's weekly rotation price, so every
// week's money and meals belong to the kitchen that cooks it. All four start
// pending payment and are paid together.
func (s *Service) CreateRotation(ctx context.Context, in RotationInput) (Rotation, error) {
	return s.createRotation(ctx, in, uuid.NullUUID{}, uuid.Nil)
}

// createRotation is CreateRotation for a new rotation or a renewal. A renewal
// links back to the rotation it renews and keeps notFirstBranch out of week 1.
func (s *Service) createRotation(ctx context.Context, in RotationInput, renewedFrom uuid.NullUUID, notFirstBranch uuid.UUID) (Rotation, error) {
	var id uuid.UUID
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		plans, start, loc, err := s.prepareRotation(ctx, tx, in)
		if err != nil {
			return err
		}
		load, err := rotationLoad(ctx, tx, plans, start)
		if err != nil {
			return err
		}
		notFirst := -1
		for k, p := range plans {
			if p.BranchID == notFirstBranch {
				notFirst = k
			}
		}
		order := AssignWeeksAvoiding(load, notFirst)
		quote := buildQuote(plans, order, start)
		pr, shares, subsidies, err := s.rotationPromo(ctx, tx, in, plans, &quote)
		if err != nil {
			return err
		}
		var cycleIDs []uuid.UUID
		var cycleDiscounts, cycleSubsidies []int64

		weekdays := make([]int16, 0, len(in.Weekdays))
		for _, d := range in.Weekdays {
			weekdays = append(weekdays, int16(d))
		}
		slices.Sort(weekdays)
		id = uuid.New()
		var from any
		if renewedFrom.Valid {
			from = renewedFrom.UUID
		}
		for attempt := 0; ; attempt++ {
			_, err = tx.Exec(ctx, `
				INSERT INTO meal_rotations (
					id, rotation_no, user_id, address_id, start_date, end_date,
					meal_slots, delivery_weekdays, amount_minor, currency,
					auto_renew, renewed_from_id
				) VALUES ($1, generate_rotation_no(), $2, $3, $4, $5, $6, $7, $8, $9, true, $10)`,
				id, in.UserID, in.AddressID, start, start.AddDate(0, 0, 7*RotationKitchens-1),
				in.Slots, weekdays, quote.AmountMinor, quote.Currency, from)
			if err == nil {
				break
			}
			if renewedFrom.Valid && isUniqueViolation(err, "meal_rotations_renewed_from_key") {
				return httpx.Coded(httpx.ErrConflict, "already_renewed", "This rotation has already been renewed.")
			}
			if attempt == 2 || !isUniqueViolation(err, "meal_rotations_no_key") {
				return fmt.Errorf("subscriptions: insert rotation: %w", err)
			}
		}

		for w, k := range order {
			p := plans[k]
			from := start.AddDate(0, 0, 7*w)
			to := from.AddDate(0, 0, 6)
			subID := uuid.New()
			if err := s.repo.InsertSubscription(ctx, tx, insertParams{
				ID: subID, UserID: in.UserID, Plan: p.PlanTemplate,
				AddressID: in.AddressID, Start: from, End: to, RenewalMode: "none",
				RotationID: uuid.NullUUID{UUID: id, Valid: true}, RotationWeek: w + 1,
			}); err != nil {
				return err
			}
			if err := s.repo.InsertRules(ctx, tx, subID, p.Rules); err != nil {
				return err
			}
			cycle := FirstCycle(from, 7, p.Rules, loc, p.Quote.TotalMinor, p.Currency)
			orderID, cycleID, err := s.openCycle(ctx, tx, subID, p.PlanTemplate,
				SubscribeInput{UserID: in.UserID, AddressID: in.AddressID}, cycle, p.Quote)
			if err != nil {
				return err
			}
			if pr != nil {
				cycleIDs = append(cycleIDs, cycleID)
				cycleDiscounts = append(cycleDiscounts, shares[k])
				cycleSubsidies = append(cycleSubsidies, subsidies[k])
			}
			if err := s.repo.RecordEvent(ctx, tx, subID, "created", "", "pending_payment",
				"customer", uuid.NullUUID{UUID: in.UserID, Valid: true},
				mustJSON(map[string]any{"plan_id": p.ID, "cycle_id": cycleID,
					"rotation_id": id, "rotation_week": w + 1})); err != nil {
				return err
			}
			if err := s.insertOutbox(ctx, tx, subID, "subscription.created", map[string]any{
				"subscription_id": subID, "user_id": in.UserID,
				"branch_id": p.BranchID, "vendor_id": p.VendorID, "plan_id": p.ID,
				"order_id": orderID, "amount_minor": p.Quote.TotalMinor,
				"currency": p.Currency, "rotation_id": id,
			}); err != nil {
				return err
			}
		}
		if pr != nil {
			return s.holdPromo(ctx, tx, pr, in.UserID, promo.Target{RotationID: &id},
				cycleIDs, cycleDiscounts, cycleSubsidies, quote.Currency)
		}
		return nil
	})
	if err != nil {
		return Rotation{}, err
	}
	return s.MyRotation(ctx, in.UserID, id)
}

const rotationSelect = `
	SELECT r.id, r.rotation_no, r.status, r.meal_slots, r.delivery_weekdays,
	       r.created_at, r.start_date, r.end_date, r.amount_minor, r.currency,
	       r.auto_renew, r.renewed_from_id, n.id, n.status
	  FROM meal_rotations r
	  LEFT JOIN LATERAL (
	       SELECT id, status FROM meal_rotations
	        WHERE renewed_from_id = r.id AND status <> 'cancelled'
	        ORDER BY created_at DESC LIMIT 1) n ON true`

func scanRotation(row pgx.Row) (Rotation, error) {
	var r Rotation
	var weekdays []int16
	var start, end time.Time
	if err := row.Scan(&r.ID, &r.RotationNo, &r.Status, &r.MealSlots, &weekdays,
		&r.CreatedAt, &start, &end, &r.AmountMinor, &r.Currency,
		&r.AutoRenew, &r.RenewedFromID, &r.RenewedByID, &r.RenewedByStatus); err != nil {
		return r, err
	}
	for _, d := range weekdays {
		r.Weekdays = append(r.Weekdays, int(d))
	}
	r.StartDate, r.EndDate = start.Format("2006-01-02"), end.Format("2006-01-02")
	r.Weeks = []RotationWeek{}
	return r, nil
}

// MyRotations lists the customer's rotations, newest first, with their weeks.
func (s *Service) MyRotations(ctx context.Context, userID uuid.UUID) ([]Rotation, error) {
	rows, err := s.pool.Query(ctx, rotationSelect+` WHERE r.user_id = $1 ORDER BY r.created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("subscriptions: list rotations: %w", err)
	}
	out := []Rotation{}
	for rows.Next() {
		r, err := scanRotation(rows)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("subscriptions: scan rotation: %w", err)
		}
		out = append(out, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Weeks, err = s.rotationWeeks(ctx, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// MyRotation is one rotation the customer owns; anyone else's is 404.
func (s *Service) MyRotation(ctx context.Context, userID, id uuid.UUID) (Rotation, error) {
	r, err := scanRotation(s.pool.QueryRow(ctx, rotationSelect+` WHERE r.id = $1 AND r.user_id = $2`, id, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return r, httpx.ErrNotFound
	}
	if err != nil {
		return r, fmt.Errorf("subscriptions: load rotation: %w", err)
	}
	r.Weeks, err = s.rotationWeeks(ctx, id)
	return r, err
}

func (s *Service) rotationWeeks(ctx context.Context, id uuid.UUID) ([]RotationWeek, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT s.rotation_week, s.start_date, s.end_date, s.branch_id, b.name, b.name_ar,
		       s.plan_id, p.name, COALESCE(c.amount_minor, 0),
		       s.id, s.subscription_no, s.status
		  FROM subscriptions s
		  JOIN vendor_branches b ON b.id = s.branch_id
		  JOIN subscription_plans p ON p.id = s.plan_id
		  LEFT JOIN subscription_cycles c ON c.subscription_id = s.id AND c.cycle_no = 1
		 WHERE s.rotation_id = $1
		 ORDER BY s.rotation_week`, id)
	if err != nil {
		return nil, fmt.Errorf("subscriptions: rotation weeks: %w", err)
	}
	defer rows.Close()
	out := []RotationWeek{}
	for rows.Next() {
		var w RotationWeek
		var start, end time.Time
		var subID uuid.UUID
		var no, status string
		if err := rows.Scan(&w.Week, &start, &end, &w.BranchID, &w.KitchenName, &w.KitchenNameAr,
			&w.PlanID, &w.PlanName, &w.PriceMinor, &subID, &no, &status); err != nil {
			return nil, fmt.Errorf("subscriptions: scan rotation week: %w", err)
		}
		w.StartsOn, w.EndsOn = start.Format("2006-01-02"), end.Format("2006-01-02")
		w.SubscriptionID, w.SubscriptionNo, w.Status = &subID, &no, &status
		out = append(out, w)
	}
	return out, rows.Err()
}

// CancelRotation cancels every week not yet finished. Each week unwinds
// exactly as a cancelled subscription does, so unearned money for a paid week
// is credited the same way and delivered meals stay delivered.
func (s *Service) CancelRotation(ctx context.Context, userID, id uuid.UUID, reason string) error {
	r, err := s.MyRotation(ctx, userID, id)
	if err != nil {
		return err
	}
	if r.Status == "cancelled" {
		return nil
	}
	for _, w := range r.Weeks {
		if w.Status != nil && (*w.Status == "cancelled" || *w.Status == "completed") {
			continue
		}
		if err := s.cancel(ctx, *w.SubscriptionID, uuid.NullUUID{UUID: userID, Valid: true},
			"customer", userID, reason); err != nil {
			return err
		}
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE meal_rotations
		   SET status = 'cancelled', cancelled_at = now(),
		       cancellation_reason = NULLIF($2, ''), updated_at = now()
		 WHERE id = $1 AND status <> 'cancelled'`, id, reason); err != nil {
		return fmt.Errorf("subscriptions: cancel rotation: %w", err)
	}
	if r.Status == "pending_payment" {
		return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
			return promo.ReleaseUnpaid(ctx, tx, promo.Target{RotationID: &id})
		})
	}
	return nil
}
