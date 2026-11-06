package subscriptions

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/ledger"
	"github.com/marketplace/api/internal/platform/postgres"
	"github.com/marketplace/api/internal/seam/servicetype"
)

// A guest week is the anti-boredom move for a single-kitchen subscriber: next
// week's deliveries on their own subscription are paused with wallet credit
// (the documented pause-with-credit path, allowance untouched), and that
// credit pays for an ordinary one-week subscription with another kitchen. The
// original kitchen is not paid for the paused week because nothing is
// delivered and so nothing is recognised; the guest kitchen is paid through
// its own cycle exactly like any other subscription.

// GuestWeekBandPercent is how far a guest kitchen's per-meal price may sit
// from the customer's current plan, either way.
const GuestWeekBandPercent = 15

// GuestWeekMinDeliveredWeeks is how many weeks of completed deliveries a
// subscription needs before a guest week is offered.
const GuestWeekMinDeliveredWeeks = 2

type GuestWeekOption struct {
	PlanID        uuid.UUID `json:"plan_id"`
	BranchID      uuid.UUID `json:"branch_id"`
	KitchenName   string    `json:"kitchen_name"`
	KitchenNameAr *string   `json:"kitchen_name_ar"`
	PlanName      string    `json:"plan_name"`
	PlanNameAr    *string   `json:"plan_name_ar"`
	Cuisines      []string  `json:"cuisines"`
	PerMealMinor  int64     `json:"per_meal_minor"`
	WeekMinor     int64     `json:"week_minor"`
	WalletMinor   int64     `json:"wallet_minor"`
	DueMinor      int64     `json:"due_minor"`
	LeftoverMinor int64     `json:"leftover_credit_minor"`
	Currency      string    `json:"currency"`
}

// GuestWeekOffer is what My meals shows: whether a guest week is on the table
// for the coming week and, if so, which kitchens the customer may pick.
type GuestWeekOffer struct {
	Eligible bool `json:"eligible"`
	// Reason the offer is not available: rotation, not_active, too_few_weeks,
	// past_cutoff, already_used, dismissed, no_deliveries, no_kitchens,
	// unavailable.
	Reason              string            `json:"reason,omitempty"`
	WeekStart           string            `json:"week_start,omitempty"`
	WeekEnd             string            `json:"week_end,omitempty"`
	DeliveryDays        int               `json:"delivery_days"`
	Meals               int               `json:"meals"`
	CreditMinor         int64             `json:"credit_minor"`
	CurrentPerMealMinor int64             `json:"current_per_meal_minor"`
	Currency            string            `json:"currency"`
	Options             []GuestWeekOption `json:"options"`
}

type GuestWeekResult struct {
	GuestSubscriptionID uuid.UUID  `json:"guest_subscription_id"`
	Status              string     `json:"status"`
	PendingOrderID      *uuid.UUID `json:"pending_order_id"`
	WeekStart           string     `json:"week_start"`
	WeekEnd             string     `json:"week_end"`
	CreditMinor         int64      `json:"credit_minor"`
	WalletMinor         int64      `json:"wallet_minor"`
	DueMinor            int64      `json:"due_minor"`
	LeftoverMinor       int64      `json:"leftover_credit_minor"`
	Currency            string     `json:"currency"`
}

// GuestWeekLink ties the two halves of a guest week together in My meals. On
// the original subscription it names the guest kitchen; on the guest
// subscription it names the kitchen the customer came from.
type GuestWeekLink struct {
	SubscriptionID      uuid.UUID  `json:"subscription_id"`
	GuestSubscriptionID uuid.UUID  `json:"guest_subscription_id"`
	KitchenName         string     `json:"kitchen_name"`
	KitchenNameAr       *string    `json:"kitchen_name_ar"`
	WeekStart           string     `json:"week_start"`
	WeekEnd             string     `json:"week_end"`
	Status              string     `json:"status"`
	PendingOrderID      *uuid.UUID `json:"pending_order_id"`
	Completed           bool       `json:"completed"`
}

type CheaperPlan struct {
	PlanID        uuid.UUID `json:"plan_id"`
	BranchID      uuid.UUID `json:"branch_id"`
	KitchenName   string    `json:"kitchen_name"`
	KitchenNameAr *string   `json:"kitchen_name_ar"`
	PlanName      string    `json:"plan_name"`
	PlanNameAr    *string   `json:"plan_name_ar"`
	Cuisines      []string  `json:"cuisines"`
	PerMealMinor  int64     `json:"per_meal_minor"`
	Currency      string    `json:"currency"`
}

type CheaperPlans struct {
	CurrentPerMealMinor int64         `json:"current_per_meal_minor"`
	Currency            string        `json:"currency"`
	Items               []CheaperPlan `json:"items"`
}

// NextWeek is the coming Sunday-to-Saturday week in the branch's time zone.
// Today is never part of it: a guest week is always "next week", even on a
// Sunday, because the cutoff for this week's meals has long gone.
func NextWeek(now time.Time, loc *time.Location) (from, to time.Time) {
	today := dayIn(now, loc)
	ahead := 7 - int(today.Weekday())
	from = today.AddDate(0, 0, ahead)
	return from, from.AddDate(0, 0, 6)
}

// PerMeal is the price of one meal on a cycle: the cycle amount spread over
// every delivery slot in it. Integer division floors; a single fil of
// rounding on a per-meal label is not money anyone is owed.
func PerMeal(amountMinor int64, deliverableDays, slots int) int64 {
	n := int64(deliverableDays * slots)
	if n <= 0 || amountMinor <= 0 {
		return 0
	}
	return amountMinor / n
}

// WithinBand reports whether candidate is within pct percent of current,
// computed in integer minor units so the band is exact.
func WithinBand(candidate, current int64, pct int) bool {
	if current <= 0 {
		return false
	}
	diff := candidate - current
	if diff < 0 {
		diff = -diff
	}
	return diff*100 <= current*int64(pct)
}

// GuestWeekSplit decides how a guest week is paid: the paused week's credit
// first, a card charge for anything above it, and whatever credit is not
// needed stays in the wallet.
func GuestWeekSplit(creditMinor, weekMinor int64) (wallet, due, leftover int64) {
	if creditMinor < 0 {
		creditMinor = 0
	}
	if weekMinor < 0 {
		weekMinor = 0
	}
	wallet = min(creditMinor, weekMinor)
	return wallet, weekMinor - wallet, creditMinor - wallet
}

type guestPlan struct {
	GuestWeekOffer
	from, to time.Time
	days     int
	weekdays []int16
	slots    []string
	cycle    cycleRow
	plans    map[uuid.UUID]PlanTemplate
}

func notEligible(reason string) *guestPlan {
	return &guestPlan{GuestWeekOffer: GuestWeekOffer{Reason: reason, Options: []GuestWeekOption{}}}
}

// GuestWeekOffer answers GET /me/subscriptions/{id}/guest-week.
func (s *Service) GuestWeekOffer(ctx context.Context, userID, subID uuid.UUID, now time.Time) (*GuestWeekOffer, error) {
	var out *GuestWeekOffer
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		live, err := s.loadOwned(ctx, tx, userID, subID)
		if err != nil {
			return err
		}
		g, err := s.guestOffer(ctx, tx, live, now)
		if err != nil {
			return err
		}
		out = &g.GuestWeekOffer
		return nil
	})
	return out, err
}

func (s *Service) guestOffer(ctx context.Context, tx pgx.Tx, live *Live, now time.Time) (*guestPlan, error) {
	if s.wallet == nil {
		return notEligible("unavailable"), nil
	}
	if live.RotationID.Valid {
		return notEligible("rotation"), nil
	}
	if live.Status != "active" {
		return notEligible("not_active"), nil
	}

	var delivered int
	if err := tx.QueryRow(ctx, `
		SELECT count(DISTINCT (scheduled_date - EXTRACT(dow FROM scheduled_date)::int))
		  FROM fulfillments
		 WHERE subscription_id = $1 AND status = 'completed'`, live.ID).Scan(&delivered); err != nil {
		return nil, fmt.Errorf("subscriptions: delivered weeks: %w", err)
	}
	if delivered < GuestWeekMinDeliveredWeeks {
		return notEligible("too_few_weeks"), nil
	}

	cycle, err := s.currentCycle(ctx, tx, live.ID)
	if err != nil {
		return nil, err
	}
	if cycle.Status != "paid" {
		return notEligible("not_active"), nil
	}

	var used, dismissed bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM subscription_guest_weeks WHERE subscription_id = $1 AND cycle_id = $2),
		       EXISTS (SELECT 1 FROM subscription_guest_week_dismissals WHERE subscription_id = $1 AND cycle_id = $2)`,
		live.ID, cycle.ID).Scan(&used, &dismissed); err != nil {
		return nil, fmt.Errorf("subscriptions: guest week state: %w", err)
	}
	if used {
		return notEligible("already_used"), nil
	}
	if dismissed {
		return notEligible("dismissed"), nil
	}

	from, to := NextWeek(now, live.Location)
	if from.After(dayIn(cycle.Ends, live.Location)) {
		return notEligible("no_deliveries"), nil
	}
	if cycleEnd := dayIn(cycle.Ends, live.Location); to.After(cycleEnd) {
		to = cycleEnd
	}

	g := &guestPlan{from: from, to: to, cycle: cycle, plans: map[uuid.UUID]PlanTemplate{}}
	g.WeekStart, g.WeekEnd = from.Format("2006-01-02"), to.Format("2006-01-02")
	g.Currency = cycle.Currency
	g.Options = []GuestWeekOption{}

	seenDay, seenSlot := map[Weekday]bool{}, map[string]bool{}
	for _, r := range live.Rules {
		if !seenDay[r.Weekday] {
			seenDay[r.Weekday] = true
			g.weekdays = append(g.weekdays, int16(r.Weekday))
		}
		if !seenSlot[r.Slot] {
			seenSlot[r.Slot] = true
			g.slots = append(g.slots, r.Slot)
		}
	}

	first := time.Time{}
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		if seenDay[Weekday(d.Weekday())] {
			first = d
			break
		}
	}
	if first.IsZero() {
		g.Reason = "no_deliveries"
		return g, nil
	}
	if !CanModify(first, live.CutoffHours, live.Location, now) {
		g.Reason = "past_cutoff"
		return g, nil
	}

	g.days = PausedDays(live.Subscription, live.Rules, Pause{From: from, To: to}, now)
	if g.days == 0 {
		g.Reason = "no_deliveries"
		return g, nil
	}
	g.DeliveryDays = g.days
	g.Meals = g.days * len(g.slots)
	g.CreditMinor = PauseCredit(cycle.AmountMinor, g.days, cycle.DeliverableDays, cycle.RecognisedMinor)
	g.CurrentPerMealMinor = PerMeal(cycle.AmountMinor, cycle.DeliverableDays, len(g.slots))

	avoided, err := avoidedBranches(ctx, tx, live.UserID)
	if err != nil {
		return nil, err
	}

	rows, err := tx.Query(ctx, `
		SELECT p.id, b.id, b.name, b.name_ar, p.name, p.name_ar, COALESCE(b.cuisines, '{}'),
		       round(p.price_minor::numeric / greatest(1,
		             round(p.cycle_days * p.days_per_week / 7.0) * p.meals_per_day))::bigint
		  FROM subscription_plans p
		  JOIN vendor_branches b ON b.id = p.branch_id
		  JOIN addresses a ON a.id = $1
		 WHERE p.is_active AND b.is_active AND b.deleted_at IS NULL
		   AND p.service_type = $2 AND p.currency = $3
		   AND b.id <> $4
		   AND ST_Intersects(b.service_area, a.point)
		   AND p.delivery_weekdays @> $5::smallint[]
		   AND p.meal_slots @> $6::text[]
		   AND EXISTS (SELECT 1 FROM time_windows tw WHERE tw.branch_id = b.id AND tw.is_active)
		 ORDER BY b.name, p.price_minor`,
		live.AddressID, s.serviceTypeOf(ctx, tx, live.PlanID), cycle.Currency, live.BranchID,
		g.weekdays, g.slots)
	if err != nil {
		return nil, fmt.Errorf("subscriptions: guest kitchens: %w", err)
	}
	type cand struct {
		opt GuestWeekOption
	}
	var cands []cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.opt.PlanID, &c.opt.BranchID, &c.opt.KitchenName, &c.opt.KitchenNameAr,
			&c.opt.PlanName, &c.opt.PlanNameAr, &c.opt.Cuisines, &c.opt.PerMealMinor); err != nil {
			rows.Close()
			return nil, fmt.Errorf("subscriptions: scan guest kitchen: %w", err)
		}
		cands = append(cands, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("subscriptions: guest kitchens: %w", err)
	}

	seenBranch := map[uuid.UUID]bool{}
	for _, c := range cands {
		o := c.opt
		if avoided[o.BranchID] || seenBranch[o.BranchID] {
			continue
		}
		if !WithinBand(o.PerMealMinor, g.CurrentPerMealMinor, GuestWeekBandPercent) {
			continue
		}
		plan, err := s.repo.LoadPlan(ctx, tx, o.PlanID, uuid.NullUUID{})
		if err != nil {
			continue
		}
		quote, err := s.quoteGuestWeek(ctx, plan, o.PerMealMinor*int64(g.Meals))
		if err != nil {
			continue
		}
		o.WeekMinor = quote.TotalMinor
		o.Currency = cycle.Currency
		o.WalletMinor, o.DueMinor, o.LeftoverMinor = GuestWeekSplit(g.CreditMinor, o.WeekMinor)
		seenBranch[o.BranchID] = true
		g.plans[o.PlanID] = plan
		g.Options = append(g.Options, o)
		if len(g.Options) == 8 {
			break
		}
	}
	if len(g.Options) == 0 {
		g.Reason = "no_kitchens"
		return g, nil
	}
	g.Eligible = true
	return g, nil
}

func (s *Service) serviceTypeOf(ctx context.Context, tx pgx.Tx, planID uuid.UUID) string {
	var st string
	_ = tx.QueryRow(ctx, `SELECT service_type FROM subscription_plans WHERE id = $1`, planID).Scan(&st)
	return st
}

func (s *Service) quoteGuestWeek(ctx context.Context, plan PlanTemplate, baseMinor int64) (servicetype.QuoteResult, error) {
	vertical, err := s.verticals.Get(plan.ServiceType)
	if err != nil {
		return servicetype.QuoteResult{}, fmt.Errorf("%w: service type %q", httpx.ErrNotFound, plan.ServiceType)
	}
	return vertical.Pricing.Quote(ctx, servicetype.PricingContext{
		BranchID:    plan.BranchID,
		ServiceType: plan.ServiceType,
		Currency:    plan.Currency,
		Lines: []servicetype.PricingLine{{
			OfferingID:     plan.ID,
			Name:           plan.Name,
			UnitPriceMinor: baseMinor,
			TaxRateBP:      plan.TaxRateBP,
			Quantity:       1,
		}},
	})
}

// avoidedBranches is the kitchens the customer marked "not again" in their
// taste memory (meal_tastes); a guest week never proposes one of them.
func avoidedBranches(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (map[uuid.UUID]bool, error) {
	rows, err := tx.Query(ctx, `
		SELECT subject_id FROM meal_tastes
		 WHERE user_id = $1 AND subject_kind = 'kitchen' AND sentiment = 'avoid'`, userID)
	if err != nil {
		return nil, fmt.Errorf("subscriptions: avoided kitchens: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("subscriptions: avoided kitchens: %w", err)
	}
	out := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

// DismissGuestWeek records "not now" for the current cycle. The offer comes
// back with the next cycle, never sooner.
func (s *Service) DismissGuestWeek(ctx context.Context, userID, subID uuid.UUID) error {
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		live, err := s.loadOwned(ctx, tx, userID, subID)
		if err != nil {
			return err
		}
		cycle, err := s.currentCycle(ctx, tx, live.ID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO subscription_guest_week_dismissals (subscription_id, cycle_id)
			VALUES ($1, $2) ON CONFLICT DO NOTHING`, live.ID, cycle.ID)
		if err != nil {
			return fmt.Errorf("subscriptions: dismiss guest week: %w", err)
		}
		return nil
	})
}

// StartGuestWeek pauses next week on the customer's subscription with wallet
// credit and opens a one-week subscription with the chosen kitchen, paid from
// that credit. When the guest week costs more than the credit, the remainder
// is a normal pending order the customer pays through the sandbox gateway;
// when it costs less, the difference stays in the wallet.
func (s *Service) StartGuestWeek(ctx context.Context, userID, subID, planID uuid.UUID, now time.Time) (*GuestWeekResult, error) {
	if s.wallet == nil {
		return nil, httpx.Coded(httpx.ErrConflict, "guest_week_unavailable", "Guest weeks are not available right now.")
	}
	var out *GuestWeekResult
	var orderID uuid.UUID
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		// Serialise concurrent attempts on one subscription; the unique
		// (subscription, cycle) key is the backstop.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM subscriptions WHERE id = $1 AND user_id = $2 FOR UPDATE`, subID, userID); err != nil {
			return fmt.Errorf("subscriptions: lock: %w", err)
		}
		live, err := s.loadOwned(ctx, tx, userID, subID)
		if err != nil {
			return err
		}
		g, err := s.guestOffer(ctx, tx, live, now)
		if err != nil {
			return err
		}
		if !g.Eligible {
			switch g.Reason {
			case "already_used":
				return httpx.Coded(httpx.ErrConflict, "guest_week_exists",
					"You already have a guest week in this cycle.")
			case "past_cutoff":
				return &httpx.ValidationError{Fields: []httpx.FieldError{{
					Field: "week_start", Code: "past_cutoff",
					Message: fmt.Sprintf("changes must be made at least %d hours before the delivery day", live.CutoffHours),
				}}}
			}
			return httpx.Coded(httpx.ErrConflict, "guest_week_not_eligible",
				"A guest week is not available for this subscription right now.")
		}
		var opt *GuestWeekOption
		for i := range g.Options {
			if g.Options[i].PlanID == planID {
				opt = &g.Options[i]
			}
		}
		if opt == nil {
			return &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "plan_id", Code: "not_eligible",
				Message: "that kitchen is not available as a guest week for this subscription",
			}}}
		}
		plan := g.plans[planID]

		// 1. Pause next week on the original with wallet credit. The pause
		//    allowance is not consumed: this is not the customer being away.
		var pauseID uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO subscription_pauses
			    (subscription_id, from_date, to_date, credit_mode,
			     suppressed_days, credit_minor, created_by)
			VALUES ($1,$2,$3,'wallet',$4,$5,$6)
			RETURNING id`,
			live.ID, g.from, g.to, g.days, g.CreditMinor, userID,
		).Scan(&pauseID); err != nil {
			return fmt.Errorf("subscriptions: guest week pause: %w", err)
		}
		if g.CreditMinor > 0 {
			if _, err := ledger.PostSubscriptionCredited(ctx, tx, ledger.SubscriptionCredit{
				SubscriptionID: live.ID, CycleID: g.cycle.ID, UserID: userID,
				Currency: g.cycle.Currency, AmountMinor: g.CreditMinor, Reason: "guest week",
			}); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE fulfillments
			   SET status = 'cancelled'
			 WHERE subscription_id = $1
			   AND scheduled_date BETWEEN $2 AND $3
			   AND scheduled_date >= CURRENT_DATE
			   AND status = 'scheduled'`, live.ID, g.from, g.to); err != nil {
			return fmt.Errorf("subscriptions: cancel guest-week deliveries: %w", err)
		}

		// 2. A one-week subscription with the guest kitchen on the same days
		//    and slots, delivered to the same address.
		var window uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT id FROM time_windows
			 WHERE branch_id = $1 AND is_active
			 ORDER BY sort_order, starts_at LIMIT 1`, plan.BranchID).Scan(&window); err != nil {
			return fmt.Errorf("subscriptions: guest window: %w", err)
		}
		rules := make([]Rule, 0, len(live.Rules))
		for _, r := range live.Rules {
			rules = append(rules, Rule{Weekday: r.Weekday, Slot: r.Slot, WindowID: window, AddressID: r.AddressID})
		}
		in := SubscribeInput{
			UserID: userID, PlanID: plan.ID, AddressID: live.AddressID,
			StartDate: g.from, Rules: rules, RenewalMode: "none",
		}
		if err := validateRules(rules, plan); err != nil {
			return err
		}
		if err := s.checkAddresses(ctx, tx, in, plan.BranchID); err != nil {
			return err
		}
		quote, err := s.quoteGuestWeek(ctx, plan, opt.PerMealMinor*int64(g.Meals))
		if err != nil {
			return fmt.Errorf("subscriptions: guest quote: %w", err)
		}
		wallet, due, leftover := GuestWeekSplit(g.CreditMinor, quote.TotalMinor)
		quote.AuthorizeMinor = due

		guestID := uuid.New()
		if err := s.repo.InsertSubscription(ctx, tx, insertParams{
			ID: guestID, UserID: userID, Plan: plan, AddressID: live.AddressID,
			Start: g.from, End: g.to, RenewalMode: "none",
		}); err != nil {
			return err
		}
		if err := s.repo.InsertRules(ctx, tx, guestID, rules); err != nil {
			return err
		}
		cycle := Cycle{No: 1, Starts: g.from, Ends: g.to, AmountMinor: quote.TotalMinor,
			Currency: plan.Currency, DeliverableDays: g.days}
		var cycleID uuid.UUID
		orderID, cycleID, err = s.openCycle(ctx, tx, guestID, plan, in, cycle, quote)
		if err != nil {
			return err
		}

		// 3. The wallet pays its share now, in the same transaction that
		//    created the credit, so the two never disagree.
		var walletPosted *time.Time
		if wallet > 0 {
			if _, err := ledger.PostSubscriptionPaidFromWallet(ctx, tx, ledger.WalletCharge{
				SubscriptionID: guestID, CycleID: cycleID, UserID: userID,
				Currency: plan.Currency, AmountMinor: wallet, Reason: "guest week",
			}); err != nil {
				return err
			}
			t := now
			walletPosted = &t
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO subscription_guest_weeks
			    (subscription_id, guest_subscription_id, cycle_id, pause_id, week_start, week_end,
			     currency, credit_minor, guest_amount_minor, wallet_minor, due_minor, wallet_posted_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			live.ID, guestID, g.cycle.ID, pauseID, g.from, g.to,
			plan.Currency, g.CreditMinor, quote.TotalMinor, wallet, due, walletPosted); err != nil {
			if isUniqueViolation(err, "subscription_guest_weeks_once_per_cycle") {
				return httpx.Coded(httpx.ErrConflict, "guest_week_exists",
					"You already have a guest week in this cycle.")
			}
			return fmt.Errorf("subscriptions: record guest week: %w", err)
		}

		actor := uuid.NullUUID{UUID: userID, Valid: true}
		if err := s.repo.RecordEvent(ctx, tx, live.ID, "paused", "", "", "customer", actor,
			mustJSON(map[string]any{
				"pause_id": pauseID, "days": g.days, "credit_minor": g.CreditMinor,
				"mode": "wallet", "guest_week": true, "guest_subscription_id": guestID,
			})); err != nil {
			return err
		}
		if err := s.repo.RecordEvent(ctx, tx, guestID, "created", "", "pending_payment", "customer", actor,
			mustJSON(map[string]any{
				"plan_id": plan.ID, "cycle_id": cycleID, "guest_of": live.ID,
				"wallet_minor": wallet, "due_minor": due,
			})); err != nil {
			return err
		}
		if err := s.insertOutbox(ctx, tx, guestID, "subscription.created", map[string]any{
			"subscription_id": guestID, "user_id": userID,
			"branch_id": plan.BranchID, "vendor_id": plan.VendorID, "plan_id": plan.ID,
			"order_id": orderID, "amount_minor": quote.TotalMinor, "currency": plan.Currency,
			"guest_of": live.ID,
		}); err != nil {
			return err
		}

		out = &GuestWeekResult{
			GuestSubscriptionID: guestID, Status: "pending_payment",
			WeekStart: g.WeekStart, WeekEnd: g.WeekEnd,
			CreditMinor: g.CreditMinor, WalletMinor: wallet, DueMinor: due, LeftoverMinor: leftover,
			Currency: plan.Currency,
		}
		if due > 0 {
			o := orderID
			out.PendingOrderID = &o
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if out.DueMinor == 0 {
		// Fully covered by the wallet: confirm the order off-gateway. The
		// money already moved above, so the charge callback has nothing left
		// to do but agree.
		err := s.wallet.AuthorizeFromWallet(ctx, userID, orderID,
			func(ctx context.Context, tx pgx.Tx, amountMinor int64, currency string) error {
				if amountMinor != 0 {
					return fmt.Errorf("subscriptions: guest week expected a zero authorization, got %d", amountMinor)
				}
				return nil
			})
		if err != nil {
			slog.ErrorContext(ctx, "subscriptions: guest week wallet confirm failed",
				"subscription_id", out.GuestSubscriptionID, "order_id", orderID, "error", err)
			o := orderID
			out.PendingOrderID = &o
			return out, nil
		}
		out.Status = "active"
	}
	return out, nil
}

// CheaperPlans lists plans around the customer's address with a lower
// per-meal price than their current subscription, for the "too expensive"
// cancel path.
func (s *Service) CheaperPlans(ctx context.Context, userID, subID uuid.UUID) (*CheaperPlans, error) {
	var out *CheaperPlans
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		live, err := s.loadOwned(ctx, tx, userID, subID)
		if err != nil {
			return err
		}
		cycle, err := s.currentCycle(ctx, tx, live.ID)
		if err != nil {
			return err
		}
		slots := map[string]bool{}
		for _, r := range live.Rules {
			slots[r.Slot] = true
		}
		perMeal := PerMeal(cycle.AmountMinor, cycle.DeliverableDays, max(1, len(slots)))
		out, err = s.cheaperPlans(ctx, tx, live.AddressID, s.serviceTypeOf(ctx, tx, live.PlanID),
			cycle.Currency, perMeal, []uuid.UUID{live.BranchID})
		return err
	})
	return out, err
}

// CheaperPlansForRotation is CheaperPlans for a rotation: cheaper than the
// rotation's average per-meal price, excluding its own kitchens.
func (s *Service) CheaperPlansForRotation(ctx context.Context, userID, rotationID uuid.UUID) (*CheaperPlans, error) {
	var out *CheaperPlans
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT s.id, s.branch_id FROM subscriptions s
			 WHERE s.rotation_id = $1 AND s.user_id = $2 AND s.status <> 'cancelled'
			 ORDER BY s.rotation_week`, rotationID, userID)
		if err != nil {
			return fmt.Errorf("subscriptions: rotation weeks: %w", err)
		}
		type wk struct {
			ID, Branch uuid.UUID
		}
		weeks, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (wk, error) {
			var w wk
			err := r.Scan(&w.ID, &w.Branch)
			return w, err
		})
		if err != nil {
			return fmt.Errorf("subscriptions: rotation weeks: %w", err)
		}
		if len(weeks) == 0 {
			return httpx.ErrNotFound
		}
		var total, meals int64
		var currency, serviceType string
		var addressID uuid.UUID
		exclude := make([]uuid.UUID, 0, len(weeks))
		for _, w := range weeks {
			live, err := s.repo.LoadLive(ctx, tx, w.ID)
			if err != nil {
				return err
			}
			cycle, err := s.currentCycle(ctx, tx, w.ID)
			if err != nil {
				return err
			}
			slots := map[string]bool{}
			for _, r := range live.Rules {
				slots[r.Slot] = true
			}
			total += cycle.AmountMinor
			meals += int64(cycle.DeliverableDays * max(1, len(slots)))
			currency, addressID = cycle.Currency, live.AddressID
			serviceType = s.serviceTypeOf(ctx, tx, live.PlanID)
			exclude = append(exclude, w.Branch)
		}
		perMeal := int64(0)
		if meals > 0 {
			perMeal = total / meals
		}
		out, err = s.cheaperPlans(ctx, tx, addressID, serviceType, currency, perMeal, exclude)
		return err
	})
	return out, err
}

func (s *Service) cheaperPlans(ctx context.Context, tx pgx.Tx, addressID uuid.UUID,
	serviceType, currency string, perMeal int64, exclude []uuid.UUID) (*CheaperPlans, error) {
	out := &CheaperPlans{CurrentPerMealMinor: perMeal, Currency: currency, Items: []CheaperPlan{}}
	if perMeal <= 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT * FROM (
		    SELECT DISTINCT ON (b.id)
		           p.id, b.id, b.name, b.name_ar, p.name, p.name_ar, COALESCE(b.cuisines, '{}'), p.currency,
		           round(p.price_minor::numeric / greatest(1,
		                 round(p.cycle_days * p.days_per_week / 7.0) * p.meals_per_day))::bigint AS per_meal
		      FROM subscription_plans p
		      JOIN vendor_branches b ON b.id = p.branch_id
		      JOIN addresses a ON a.id = $1
		     WHERE p.is_active AND b.is_active AND b.deleted_at IS NULL
		       AND p.service_type = $2 AND p.currency = $3
		       AND NOT (b.id = ANY($4))
		       AND ST_Intersects(b.service_area, a.point)
		     ORDER BY b.id, 9) x
		 WHERE per_meal < $5
		 ORDER BY per_meal LIMIT 5`,
		addressID, serviceType, currency, exclude, perMeal)
	if err != nil {
		return nil, fmt.Errorf("subscriptions: cheaper plans: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (CheaperPlan, error) {
		var c CheaperPlan
		err := r.Scan(&c.PlanID, &c.BranchID, &c.KitchenName, &c.KitchenNameAr, &c.PlanName, &c.PlanNameAr,
			&c.Cuisines, &c.Currency, &c.PerMealMinor)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("subscriptions: cheaper plans: %w", err)
	}
	if items != nil {
		out.Items = items
	}
	return out, nil
}

// attachGuestWeeks decorates My meals rows with their guest-week links so
// both kitchens' identities are visible from either side.
func (s *Service) attachGuestWeeks(ctx context.Context, q querier, items []MySubscription, now time.Time) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(items))
	idx := map[uuid.UUID]int{}
	for i, m := range items {
		ids = append(ids, m.ID)
		idx[m.ID] = i
	}
	rows, err := q.Query(ctx, `
		SELECT g.subscription_id, g.guest_subscription_id, g.week_start, g.week_end, gs.status,
		       ob.name, ob.name_ar, gb.name, gb.name_ar,
		       (SELECT c.order_id FROM subscription_cycles c
		         WHERE c.subscription_id = gs.id AND c.status IN ('pending', 'past_due')
		         ORDER BY c.cycle_no DESC LIMIT 1)
		  FROM subscription_guest_weeks g
		  JOIN subscriptions gs ON gs.id = g.guest_subscription_id
		  JOIN vendor_branches gb ON gb.id = gs.branch_id
		  JOIN subscriptions os ON os.id = g.subscription_id
		  JOIN vendor_branches ob ON ob.id = os.branch_id
		 WHERE g.subscription_id = ANY($1) OR g.guest_subscription_id = ANY($1)
		 ORDER BY g.created_at`, ids)
	if err != nil {
		return fmt.Errorf("subscriptions: guest week links: %w", err)
	}
	defer rows.Close()
	today := now.UTC().Truncate(24 * time.Hour)
	for rows.Next() {
		var origID, guestID uuid.UUID
		var from, to time.Time
		var status, origName, guestName string
		var origNameAr, guestNameAr *string
		var pending *uuid.UUID
		if err := rows.Scan(&origID, &guestID, &from, &to, &status,
			&origName, &origNameAr, &guestName, &guestNameAr, &pending); err != nil {
			return fmt.Errorf("subscriptions: scan guest week link: %w", err)
		}
		link := GuestWeekLink{
			SubscriptionID: origID, GuestSubscriptionID: guestID,
			WeekStart: from.Format("2006-01-02"), WeekEnd: to.Format("2006-01-02"),
			Status: status, PendingOrderID: pending,
			Completed: status != "cancelled" && to.Before(today),
		}
		if i, ok := idx[origID]; ok {
			l := link
			l.KitchenName, l.KitchenNameAr = guestName, guestNameAr
			items[i].GuestWeek = &l
		}
		if i, ok := idx[guestID]; ok {
			l := link
			l.KitchenName, l.KitchenNameAr = origName, origNameAr
			items[i].GuestOf = &l
		}
	}
	return rows.Err()
}
