package subscriptions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/ledger"
	"github.com/marketplace/api/internal/modules/promo"
	"github.com/marketplace/api/internal/platform/postgres"
	"github.com/marketplace/api/internal/seam/servicetype"
)

type Service struct {
	pool      *pgxpool.Pool
	verticals *servicetype.Registry
	repo      *Repository

	// Commission, in basis points, applied when revenue is recognised rather
	// than when it is charged. Same numbers the order path uses — a subscription
	// meal and a one-off meal must not earn the platform different margins.
	commissionBP     int
	platformVATBP    int
	reservationsHeld bool
	promos           *promo.Service
}

type Config struct {
	CommissionBP  int
	PlatformVATBP int
	// ReserveCapacity draws kitchen covers as each delivery is materialised.
	//
	// On by default and worth stating why it is a flag at all: a subscription
	// materialising fourteen days ahead holds capacity a fortnight before the
	// meal is cooked, which is correct for a kitchen that plans its purchasing
	// and wrong for one that does not. Turning it off means subscriptions never
	// contend with one-off orders, and the kitchen discovers the overbooking on
	// the day.
	ReserveCapacity bool
	// Promos prices discounts and trials at checkout. Nil disables promo codes.
	Promos *promo.Service
}

func NewService(pool *pgxpool.Pool, verticals *servicetype.Registry, cfg Config) *Service {
	return &Service{
		pool:             pool,
		verticals:        verticals,
		repo:             &Repository{},
		commissionBP:     cfg.CommissionBP,
		platformVATBP:    cfg.PlatformVATBP,
		reservationsHeld: cfg.ReserveCapacity,
		promos:           cfg.Promos,
	}
}

// --- signup -------------------------------------------------------------------

type SubscribeInput struct {
	UserID    uuid.UUID
	PlanID    uuid.UUID
	TierID    uuid.NullUUID
	AddressID uuid.UUID
	StartDate time.Time
	Rules     []Rule

	RenewalMode string

	// renewedFrom links a one-tap renewal to the subscription it renews.
	renewedFrom uuid.NullUUID

	// Promo is an optional code or public offer to apply to the first cycle.
	Promo promo.Ref

	// Attribution, carried exactly as an order carries it. A subscription is
	// the highest-value conversion the feed produces and measuring it through a
	// different path would put the best number in the product outside the
	// funnel Phase 3 built.
	SourceReelID   *uuid.UUID
	SourceActionID *uuid.UUID
	ViewerKey      string
}

// Result is a newly created subscription and its first cycle.
type Result struct {
	ID             uuid.UUID
	SubscriptionNo string
	Status         string
	StartDate      time.Time
	FirstCycle     Cycle
	OrderID        uuid.UUID
	AmountMinor    int64
	DiscountMinor  int64
	TrialDays      *int
	Currency       string
}

// Subscribe creates a subscription, its schedule, and its first billing cycle.
//
// One transaction, for the same reason PlaceOrder is one transaction: a
// subscription that exists without its schedule delivers nothing, and a billing
// order that exists without its subscription charges for nothing. Both failures
// are silent until a customer complains.
//
// What this deliberately does *not* do is materialise fulfillments. The
// subscription starts `pending_payment`; the rolling scheduler picks it up once
// payment succeeds. Creating deliveries for an unpaid subscription would hold
// two weeks of a kitchen's capacity against a card that may decline.
func (s *Service) Subscribe(ctx context.Context, in SubscribeInput) (*Result, error) {
	if len(in.Rules) == 0 {
		return nil, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "schedule", Code: "required",
			Message: "a subscription needs at least one delivery day",
		}}}
	}
	if in.RenewalMode == "" {
		in.RenewalMode = "auto"
	}

	var out *Result
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		plan, err := s.repo.LoadPlan(ctx, tx, in.PlanID, in.TierID)
		if err != nil {
			return err
		}

		loc, lerr := time.LoadLocation(plan.Timezone)
		if lerr != nil {
			loc = time.UTC
		}

		if err := validateRules(in.Rules, plan); err != nil {
			return err
		}
		if err := s.checkAddresses(ctx, tx, in, plan.BranchID); err != nil {
			return err
		}

		start := dayIn(in.StartDate, loc)
		if start.Before(dayIn(time.Now(), loc)) {
			return &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "start_date", Code: "in_the_past",
				Message: "a subscription cannot start before today",
			}}}
		}

		pr, err := s.priceFirstCycle(ctx, tx, plan, in, start, loc)
		if err != nil {
			return err
		}
		if pr.Promo != nil && pr.Promo.TrialDays != nil {
			in.RenewalMode = "none"
		}
		quote, cycle := pr.Quote, pr.Cycle
		subID := uuid.New()

		var endDate time.Time
		if in.RenewalMode == "none" {
			endDate = cycle.Ends
		}

		if err := s.repo.InsertSubscription(ctx, tx, insertParams{
			ID: subID, UserID: in.UserID, Plan: plan, TierID: in.TierID,
			AddressID: in.AddressID, Start: start, End: endDate,
			RenewalMode: in.RenewalMode, RenewedFrom: in.renewedFrom,
		}); err != nil {
			return err
		}
		if err := s.repo.InsertRules(ctx, tx, subID, in.Rules); err != nil {
			return err
		}

		orderID, cycleID, err := s.openCycle(ctx, tx, subID, plan, in, cycle, quote)
		if err != nil {
			return err
		}
		cycle.No = 1
		if pr.Promo != nil {
			if err := s.holdPromo(ctx, tx, pr.Promo, in.UserID, promo.Target{SubscriptionID: &subID},
				[]uuid.UUID{cycleID}, []int64{pr.Discount}, []int64{pr.Subsidy}, plan.Currency); err != nil {
				return err
			}
		}

		if err := s.repo.RecordEvent(ctx, tx, subID, "created", "", "pending_payment",
			"customer", uuid.NullUUID{UUID: in.UserID, Valid: true},
			mustJSON(map[string]any{"plan_id": plan.ID, "cycle_id": cycleID})); err != nil {
			return err
		}

		// Outbox in the same transaction, exactly as the order path does it. An
		// event published outside the transaction that produced it either fires
		// for a subscription that rolled back or never fires for one that
		// committed, and both are silent.
		if err := s.insertOutbox(ctx, tx, subID, "subscription.created", map[string]any{
			"subscription_id": subID,
			"user_id":         in.UserID,
			"branch_id":       plan.BranchID,
			"vendor_id":       plan.VendorID,
			"plan_id":         plan.ID,
			"order_id":        orderID,
			"amount_minor":    quote.TotalMinor,
			"currency":        plan.Currency,
		}); err != nil {
			return err
		}

		out = &Result{
			ID: subID, Status: "pending_payment", StartDate: start,
			FirstCycle: cycle, OrderID: orderID,
			AmountMinor: quote.TotalMinor, DiscountMinor: pr.Discount, Currency: plan.Currency,
		}
		if pr.Promo != nil {
			out.TrialDays = pr.Promo.TrialDays
		}
		return tx.QueryRow(ctx,
			`SELECT subscription_no FROM subscriptions WHERE id = $1`, subID).
			Scan(&out.SubscriptionNo)
	})

	if err != nil {
		return nil, err
	}
	return out, nil
}

// firstCyclePrice is the priced first cycle, with any promo applied.
type firstCyclePrice struct {
	Quote    servicetype.QuoteResult
	Cycle    Cycle
	Promo    *promo.Promo
	Discount int64
	Subsidy  int64
}

// priceFirstCycle prices the cycle through the vertical's own pricing
// strategy rather than reading plan.price_minor directly. The strategy is what
// knows whether a delivery fee applies, how VAT is extracted, and whether the
// amount is final — routing around it would be the first place subscription
// money stopped agreeing with order money. A trial promo shortens the cycle
// and prorates its price; a discount comes off the meals, never delivery.
func (s *Service) priceFirstCycle(ctx context.Context, tx pgx.Tx, plan PlanTemplate,
	in SubscribeInput, start time.Time, loc *time.Location) (firstCyclePrice, error) {
	var out firstCyclePrice
	if !in.Promo.Empty() {
		if s.promos == nil {
			return out, &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "promo_code", Code: "unavailable", Message: "promo codes are not available"}}}
		}
		p, err := s.promos.Resolve(ctx, tx, in.Promo, in.UserID, []uuid.UUID{plan.BranchID}, promo.ForSubscriptions)
		if err != nil {
			return out, err
		}
		out.Promo = p
	}

	cycleDays, price := plan.CycleDays, plan.PriceMinor
	if out.Promo != nil && out.Promo.TrialDays != nil && *out.Promo.TrialDays < cycleDays {
		cycleDays = *out.Promo.TrialDays
		price = promo.TrialPrice(plan.PriceMinor, cycleDays, plan.CycleDays)
	}

	vertical, err := s.verticals.Get(plan.ServiceType)
	if err != nil {
		return out, fmt.Errorf("%w: service type %q", httpx.ErrNotFound, plan.ServiceType)
	}
	quote, err := vertical.Pricing.Quote(ctx, servicetype.PricingContext{
		BranchID:    plan.BranchID,
		ServiceType: plan.ServiceType,
		Currency:    plan.Currency,
		Lines: []servicetype.PricingLine{{
			OfferingID:     plan.ID,
			Name:           plan.Name,
			UnitPriceMinor: price,
			TaxRateBP:      plan.TaxRateBP,
			Quantity:       1,
		}},
	})
	if err != nil {
		if out.Promo != nil && out.Promo.TrialDays != nil {
			return out, &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "promo_code", Code: "trial_too_small", Message: "this trial is too small for this plan: " + err.Error()}}}
		}
		return out, fmt.Errorf("subscriptions: quote: %w", err)
	}
	if out.Promo != nil {
		out.Discount = out.Promo.Discount(quote.SubtotalMinor)
		out.Subsidy = out.Promo.PlatformFunded(out.Discount)
		promo.ApplyToQuote(&quote, out.Discount, plan.TaxRateBP)
	}
	out.Quote = quote
	out.Cycle = FirstCycle(start, cycleDays, in.Rules, loc, quote.TotalMinor, plan.Currency)
	if out.Cycle.DeliverableDays == 0 {
		return out, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "start_date", Code: "no_deliveries",
			Message: "no delivery falls inside this period; pick another start date or days"}}}
	}
	return out, nil
}

// holdPromo records the redemption and the platform-funded subsidy on each
// cycle it paid for.
func (s *Service) holdPromo(ctx context.Context, tx pgx.Tx, p *promo.Promo, userID uuid.UUID,
	t promo.Target, cycleIDs []uuid.UUID, discounts, subsidies []int64, currency string) error {
	var discount, subsidy int64
	for i, id := range cycleIDs {
		discount += discounts[i]
		subsidy += subsidies[i]
		if subsidies[i] > 0 {
			if _, err := tx.Exec(ctx,
				`UPDATE subscription_cycles SET promo_subsidy_minor = $2 WHERE id = $1`, id, subsidies[i]); err != nil {
				return fmt.Errorf("subscriptions: promo subsidy: %w", err)
			}
		}
	}
	return s.promos.Reserve(ctx, tx, p, userID, t, discount, subsidy, currency)
}

// SubscriptionPreview is the price of a checkout before it is placed.
type SubscriptionPreview struct {
	SubtotalMinor   int64        `json:"subtotal_minor"`
	DiscountMinor   int64        `json:"discount_minor"`
	DeliveryMinor   int64        `json:"delivery_minor"`
	TotalMinor      int64        `json:"total_minor"`
	Currency        string       `json:"currency"`
	StartsOn        string       `json:"starts_on"`
	EndsOn          string       `json:"ends_on"`
	DeliverableDays int          `json:"deliverable_days"`
	Renews          bool         `json:"renews"`
	Promo           *promo.Offer `json:"promo"`
}

// PreviewSubscription runs every check Subscribe runs and prices the first
// cycle, without creating anything.
func (s *Service) PreviewSubscription(ctx context.Context, in SubscribeInput) (SubscriptionPreview, error) {
	var out SubscriptionPreview
	if len(in.Rules) == 0 {
		return out, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "schedule", Code: "required", Message: "a subscription needs at least one delivery day"}}}
	}
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		plan, err := s.repo.LoadPlan(ctx, tx, in.PlanID, in.TierID)
		if err != nil {
			return err
		}
		loc, lerr := time.LoadLocation(plan.Timezone)
		if lerr != nil {
			loc = time.UTC
		}
		if err := validateRules(in.Rules, plan); err != nil {
			return err
		}
		if err := s.checkAddresses(ctx, tx, in, plan.BranchID); err != nil {
			return err
		}
		start := dayIn(in.StartDate, loc)
		if start.Before(dayIn(time.Now(), loc)) {
			return &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "start_date", Code: "in_the_past", Message: "a subscription cannot start before today"}}}
		}
		pr, err := s.priceFirstCycle(ctx, tx, plan, in, start, loc)
		if err != nil {
			return err
		}
		out = SubscriptionPreview{
			SubtotalMinor: pr.Quote.SubtotalMinor, DiscountMinor: pr.Discount,
			DeliveryMinor: pr.Quote.DeliveryMinor, TotalMinor: pr.Quote.TotalMinor,
			Currency: plan.Currency, StartsOn: pr.Cycle.Starts.Format("2006-01-02"),
			EndsOn: pr.Cycle.Ends.Format("2006-01-02"), DeliverableDays: pr.Cycle.DeliverableDays,
			Renews: in.RenewalMode != "none" && (pr.Promo == nil || pr.Promo.TrialDays == nil),
		}
		if pr.Promo != nil {
			out.Promo = pr.Promo.Offer(in.Promo.Code != "")
		}
		return nil
	})
	return out, err
}

// openCycle creates a billing period and the order that carries its money.
//
// A real row in `orders`, so a subscription charge reaches payments, quotes,
// the ledger and the vendor's payout statement through the same path as any
// other money in the system. A parallel billing table would mean two ways for
// money to exist and two places to fix every reconciliation bug.
//
// The insert is written here rather than reused from `ordering` because
// PlaceOrder does five things a billing order must not do — validate lines
// against a branch's offerings, plan fulfillments, reserve capacity, and
// attribute. Calling it would mean adding a "but not for subscriptions" branch
// to each, which is exactly the engine surgery PLAN.md Â§5 P2's gate exists to
// prevent. The duplication is fifteen lines of INSERT and it is the cheaper
// side of that trade.
func (s *Service) openCycle(ctx context.Context, tx pgx.Tx, subID uuid.UUID,
	plan PlanTemplate, in SubscribeInput, cycle Cycle, quote servicetype.QuoteResult) (orderID, cycleID uuid.UUID, err error) {

	orderID = uuid.New()
	for attempt := 0; attempt < 3; attempt++ {
		_, err = tx.Exec(ctx, `
			INSERT INTO orders (
				id, order_no, user_id, branch_id, vendor_id, service_type,
				status, kind, source_subscription_id, currency,
				source_reel_id, source_action_id
			) VALUES (
				$1, generate_order_no($2), $3, $4, $5, $6,
				'pending_payment', 'subscription_generated', $7, $8, $9, $10
			)`,
			orderID, plan.Country, in.UserID, plan.BranchID, plan.VendorID, plan.ServiceType,
			subID, plan.Currency, in.SourceReelID, in.SourceActionID)
		if err == nil {
			break
		}
		if !isUniqueViolation(err, "orders_order_no_key") {
			return uuid.Nil, uuid.Nil, fmt.Errorf("subscriptions: billing order: %w", err)
		}
	}
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("subscriptions: could not allocate an order number")
	}

	// The payment path authorizes against the order's live quote, exactly as
	// for a one-off order; without one the billing order cannot be paid.
	if _, err = tx.Exec(ctx, `
		INSERT INTO price_quotes (
			order_id, version, state, currency,
			subtotal_minor, discount_minor, delivery_minor, surcharge_minor,
			tax_minor, total_minor, tax_inclusive, authorized_minor
		) VALUES ($1, 1, 'estimated', $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		orderID, plan.Currency,
		quote.SubtotalMinor, quote.DiscountMinor, quote.DeliveryMinor, quote.SurchargeMinor,
		quote.TaxMinor, quote.TotalMinor, quote.TaxInclusive, quote.AuthorizeMinor,
	); err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("subscriptions: billing quote: %w", err)
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO subscription_cycles (
			subscription_id, cycle_no, starts_on, ends_on, order_id,
			status, amount_minor, currency, deliverable_days
		) VALUES ($1, $2, $3, $4, $5, 'pending', $6, $7, $8)
		RETURNING id`,
		subID, cycle.No, cycle.Starts, cycle.Ends, orderID,
		cycle.AmountMinor, cycle.Currency, cycle.DeliverableDays,
	).Scan(&cycleID)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("subscriptions: open cycle: %w", err)
	}
	return orderID, cycleID, nil
}

// --- pause, skip, cancel -------------------------------------------------------

type PauseInput struct {
	UserID         uuid.UUID
	SubscriptionID uuid.UUID
	From, To       time.Time
	CreditMode     string
}

// Pause suppresses a date range and credits the customer for it.
//
// Two things happen and they are separable on purpose: the deliveries stop
// (which the scheduler does on its next run, by reading the pause), and the
// money is dealt with (either by extending the end date or by crediting the
// wallet). A pause that only did the first would quietly keep the customer's
// money for meals they did not receive.
func (s *Service) Pause(ctx context.Context, in PauseInput) error {
	if in.CreditMode == "" {
		in.CreditMode = "extend"
	}
	if in.CreditMode == "none" {
		// Only an admin resolving a dispute forfeits a customer's money, and
		// that path does not come through here.
		return &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "credit_mode", Code: "not_permitted",
		}}}
	}

	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		live, err := s.loadOwned(ctx, tx, in.UserID, in.SubscriptionID)
		if err != nil {
			return err
		}
		// Extending one rotation week would run into the next kitchen's week.
		if live.RotationID.Valid {
			return httpx.Coded(httpx.ErrConflict, "rotation_no_pause",
				"A week in a kitchen rotation cannot be paused; skip meals instead.")
		}

		from := dayIn(in.From, live.Location)
		to := dayIn(in.To, live.Location)
		if to.Before(from) {
			return &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "to_date", Code: "before_from",
			}}}
		}

		// The cutoff applies to the first paused day. Past it the kitchen has
		// bought the ingredients, and a pause starting tomorrow morning is a
		// skip the vendor pays for.
		if !CanModify(from, live.CutoffHours, live.Location, time.Now()) {
			return &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "from_date", Code: "past_cutoff",
				Message: fmt.Sprintf(
					"changes must be made at least %d hours before the delivery day",
					live.CutoffHours),
			}}}
		}

		days := PausedDays(live.Subscription, live.Rules,
			Pause{From: from, To: to}, time.Now())
		if days == 0 {
			return &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "from_date", Code: "no_deliveries",
				Message: "there are no deliveries in that range",
			}}}
		}

		allowance, used, err := s.pauseAllowance(ctx, tx, in.SubscriptionID)
		if err != nil {
			return err
		}
		if used+days > allowance {
			return &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "to_date", Code: "allowance_exceeded",
				Message: fmt.Sprintf("this plan allows %d pause days per cycle, %d already used",
					allowance, used),
			}}}
		}

		cycle, err := s.currentCycle(ctx, tx, in.SubscriptionID)
		if err != nil {
			return err
		}

		credit := PauseCredit(cycle.AmountMinor, days, cycle.DeliverableDays, cycle.RecognisedMinor)

		var pauseID uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO subscription_pauses
			    (subscription_id, from_date, to_date, credit_mode,
			     suppressed_days, credit_minor, created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			RETURNING id`,
			in.SubscriptionID, from, to, in.CreditMode, days, credit, in.UserID,
		).Scan(&pauseID); err != nil {
			return fmt.Errorf("subscriptions: insert pause: %w", err)
		}

		if _, err := tx.Exec(ctx,
			`UPDATE subscriptions SET pause_days_used = pause_days_used + $2 WHERE id = $1`,
			in.SubscriptionID, days); err != nil {
			return fmt.Errorf("subscriptions: consume allowance: %w", err)
		}

		switch in.CreditMode {
		case "extend":
			// Push the end date out by the paused delivery days, so the
			// customer receives what they paid for rather than being credited
			// for it. This is the default because it keeps the subscription
			// alive, and a paused subscriber is worth far more than a credited
			// one.
			if _, err := tx.Exec(ctx, `
				UPDATE subscriptions
				   SET end_date = CASE WHEN end_date IS NULL THEN NULL
				                       ELSE end_date + $2::int END
				 WHERE id = $1`, in.SubscriptionID, days); err != nil {
				return fmt.Errorf("subscriptions: extend end date: %w", err)
			}
			if _, err := tx.Exec(ctx,
				`UPDATE subscription_cycles SET ends_on = ends_on + $2::int
				  WHERE id = $1`, cycle.ID, days); err != nil {
				return fmt.Errorf("subscriptions: extend cycle: %w", err)
			}

		case "wallet":
			if credit > 0 {
				if _, err := ledger.PostSubscriptionCredited(ctx, tx, ledger.SubscriptionCredit{
					SubscriptionID: in.SubscriptionID,
					CycleID:        cycle.ID,
					UserID:         in.UserID,
					Currency:       cycle.Currency,
					AmountMinor:    credit,
					Reason:         "subscription pause",
				}); err != nil {
					return err
				}
			}
		}

		// Cancel any already-materialised fulfillments inside the range.
		//
		// Only future ones. PLAN.md Â§4 is explicit that pauses never touch past
		// fulfillments, and the CURRENT_DATE floor is what enforces it against a
		// backdated pause rather than trusting the caller.
		if _, err := tx.Exec(ctx, `
			UPDATE fulfillments
			   SET status = 'cancelled'
			 WHERE subscription_id = $1
			   AND scheduled_date BETWEEN $2 AND $3
			   AND scheduled_date >= CURRENT_DATE
			   AND status = 'scheduled'`,
			in.SubscriptionID, from, to); err != nil {
			return fmt.Errorf("subscriptions: cancel paused deliveries: %w", err)
		}

		return s.repo.RecordEvent(ctx, tx, in.SubscriptionID, "paused", "", "",
			"customer", uuid.NullUUID{UUID: in.UserID, Valid: true},
			mustJSON(map[string]any{
				"pause_id": pauseID, "days": days,
				"credit_minor": credit, "mode": in.CreditMode,
			}))
	})
}

// Skip suppresses a single delivery.
func (s *Service) Skip(ctx context.Context, userID, subID uuid.UUID, date time.Time, slot, reason string) error {
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		live, err := s.loadOwned(ctx, tx, userID, subID)
		if err != nil {
			return err
		}

		day := dayIn(date, live.Location)
		if !CanModify(day, live.CutoffHours, live.Location, time.Now()) {
			return &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "date", Code: "past_cutoff",
				Message: fmt.Sprintf(
					"changes must be made at least %d hours before the delivery day",
					live.CutoffHours),
			}}}
		}

		// A skip is not credited. It is the customer choosing not to receive a
		// meal they have paid for, which is different from being away — that is
		// what a pause is, and it is the one with the allowance and the money.
		// Conflating them would let a customer skip an entire cycle one day at a
		// time and expect a refund.
		if _, err := tx.Exec(ctx, `
			INSERT INTO subscription_skips (subscription_id, skip_date, meal_slot, reason, created_by)
			VALUES ($1, $2, NULLIF($3,''), NULLIF($4,''), $5)
			ON CONFLICT DO NOTHING`,
			subID, day, slot, reason, userID); err != nil {
			return fmt.Errorf("subscriptions: insert skip: %w", err)
		}

		if _, err := tx.Exec(ctx, `
			UPDATE fulfillments
			   SET status = 'cancelled'
			 WHERE subscription_id = $1
			   AND scheduled_date = $2
			   AND scheduled_date >= CURRENT_DATE
			   AND status = 'scheduled'
			   AND ($3 = '' OR payload_ref = $3)`,
			subID, day, slot); err != nil {
			return fmt.Errorf("subscriptions: cancel skipped delivery: %w", err)
		}

		return s.repo.RecordEvent(ctx, tx, subID, "skipped", "", "",
			"customer", uuid.NullUUID{UUID: userID, Valid: true},
			mustJSON(map[string]any{"date": day.Format("2006-01-02"), "slot": slot}))
	})
}

// Cancel stops a subscription and returns the unearned remainder of its cycle.
func (s *Service) Cancel(ctx context.Context, userID, subID uuid.UUID, reason string) error {
	var rotation uuid.NullUUID
	err := s.pool.QueryRow(ctx,
		`SELECT rotation_id FROM subscriptions WHERE id = $1 AND user_id = $2`,
		subID, userID).Scan(&rotation)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("subscriptions: load for cancel: %w", err)
	}
	if rotation.Valid {
		return httpx.Coded(httpx.ErrConflict, "cancel_rotation",
			"This week is part of a kitchen rotation; cancel the rotation instead.")
	}
	return s.cancel(ctx, subID, uuid.NullUUID{UUID: userID, Valid: true}, "customer", userID, reason)
}

// CancelByStaff cancels on the customer's behalf. Same unwinding as the
// customer path; only the actor recorded against it differs.
func (s *Service) CancelByStaff(ctx context.Context, actorID, subID uuid.UUID, reason string) error {
	return s.cancel(ctx, subID, uuid.NullUUID{}, "admin", actorID, reason)
}

func (s *Service) cancel(ctx context.Context, subID uuid.UUID, owner uuid.NullUUID,
	actorType string, actorID uuid.UUID, reason string) error {
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		live, err := s.repo.LoadLive(ctx, tx, subID)
		if err != nil {
			return err
		}
		if owner.Valid && live.UserID != owner.UUID {
			return httpx.ErrNotFound
		}
		// Customers retrying a cancel get success; staff get told, so the audit
		// log does not show two people cancelling the same subscription.
		if live.Status == "cancelled" && !owner.Valid {
			return httpx.Coded(httpx.ErrConflict, "already_cancelled", "This subscription is already cancelled.")
		}
		userID := live.UserID
		if live.Status == "cancelled" {
			return nil
		}

		cycle, err := s.currentCycle(ctx, tx, subID)
		if err != nil && cycle.ID == uuid.Nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
			UPDATE subscriptions
			   SET status = 'cancelled', cancelled_at = now(),
			       cancellation_reason = NULLIF($2,'')
			 WHERE id = $1`, subID, reason); err != nil {
			return fmt.Errorf("subscriptions: cancel: %w", err)
		}
		if live.Status == "pending_payment" {
			if err := promo.ReleaseUnpaid(ctx, tx, promo.Target{SubscriptionID: &subID}); err != nil {
				return err
			}
		}
		if err := voidUnpaidRenewals(ctx, tx, subID); err != nil {
			return err
		}

		// Future work only. Meals already delivered stay delivered, and a
		// cancellation that retroactively cancelled yesterday's fulfillment
		// would corrupt the vendor's completed-work record.
		if _, err := tx.Exec(ctx, `
			UPDATE fulfillments
			   SET status = 'cancelled'
			 WHERE subscription_id = $1
			   AND scheduled_date >= CURRENT_DATE
			   AND status = 'scheduled'`, subID); err != nil {
			return fmt.Errorf("subscriptions: cancel deliveries: %w", err)
		}

		// The unearned remainder has to go somewhere explicit. Leaving it to age
		// quietly in deferred revenue is how a liability account stops meaning
		// anything.
		if cycle.ID != uuid.Nil && cycle.Status == "paid" {
			if unearned := cycle.AmountMinor - cycle.RecognisedMinor; unearned > 0 {
				if _, err := ledger.PostSubscriptionCancelled(ctx, tx, ledger.SubscriptionCredit{
					SubscriptionID: subID, CycleID: cycle.ID, UserID: userID,
					Currency: cycle.Currency, AmountMinor: unearned,
				}); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx,
					`UPDATE subscription_cycles SET status = 'cancelled' WHERE id = $1`,
					cycle.ID); err != nil {
					return fmt.Errorf("subscriptions: close cycle: %w", err)
				}
			}
		}

		if err := s.insertOutbox(ctx, tx, subID, "subscription.cancelled", map[string]any{
			"subscription_id": subID, "user_id": userID, "reason": reason,
		}); err != nil {
			return err
		}

		return s.repo.RecordEvent(ctx, tx, subID, "cancelled", live.Status, "cancelled",
			actorType, uuid.NullUUID{UUID: actorID, Valid: true},
			mustJSON(map[string]any{"reason": reason}))
	})
}

// --- helpers -------------------------------------------------------------------

type cycleRow struct {
	ID              uuid.UUID
	No              int
	Starts, Ends    time.Time
	Status          string
	AmountMinor     int64
	RecognisedMinor int64
	DeliverableDays int
	Currency        string
}

func (s *Service) currentCycle(ctx context.Context, tx pgx.Tx, subID uuid.UUID) (cycleRow, error) {
	var c cycleRow
	err := tx.QueryRow(ctx, `
		SELECT id, cycle_no, starts_on, ends_on, status,
		       amount_minor, recognised_minor, deliverable_days, currency
		  FROM subscription_cycles
		 WHERE subscription_id = $1 AND NOT (cycle_no > 1 AND paid_at IS NULL)
		 ORDER BY cycle_no DESC
		 LIMIT 1`, subID,
	).Scan(&c.ID, &c.No, &c.Starts, &c.Ends, &c.Status,
		&c.AmountMinor, &c.RecognisedMinor, &c.DeliverableDays, &c.Currency)
	if err != nil {
		return c, fmt.Errorf("subscriptions: current cycle: %w", err)
	}
	return c, nil
}

func (s *Service) pauseAllowance(ctx context.Context, tx pgx.Tx, subID uuid.UUID) (allowance, used int, err error) {
	err = tx.QueryRow(ctx, `
		SELECT p.allowed_pause_days, s.pause_days_used
		  FROM subscriptions s
		  JOIN subscription_plans p ON p.id = s.plan_id
		 WHERE s.id = $1`, subID).Scan(&allowance, &used)
	if err != nil {
		return 0, 0, fmt.Errorf("subscriptions: pause allowance: %w", err)
	}
	return allowance, used, nil
}

// loadOwned reads a subscription scoped to its owner.
//
// Scoped in the query rather than checked afterwards, so a mistake here is a
// 404 rather than one customer pausing another customer's meals.
func (s *Service) loadOwned(ctx context.Context, tx pgx.Tx, userID, subID uuid.UUID) (*Live, error) {
	live, err := s.repo.LoadLive(ctx, tx, subID)
	if err != nil {
		return nil, err
	}
	if live.UserID != userID {
		return nil, httpx.ErrNotFound
	}
	return live, nil
}

func (s *Service) insertOutbox(ctx context.Context, tx pgx.Tx, subID uuid.UUID, eventType string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("subscriptions: marshal event: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO outbox_events (aggregate_type, aggregate_id, event_type, payload, idempotency_key)
		VALUES ('subscription', $1, $2, $3, $4)`, subID, eventType, body,
		// A subscription can be paused or skipped many times, so each occurrence
		// is its own event rather than one per (subscription, type).
		fmt.Sprintf("subscription:%s:%s:%s", subID, eventType, uuid.New())); err != nil {
		return fmt.Errorf("subscriptions: outbox: %w", err)
	}
	return nil
}

// validateRules checks the schedule against the plan's shape.
//
// A customer who picks six days on a five-day plan is not paying for six, and
// letting it through means the kitchen cooks a meal nobody was billed for —
// every week, invisibly, until someone reconciles portions against revenue.
func validateRules(rules []Rule, plan PlanTemplate) error {
	days := map[Weekday]int{}
	for _, r := range rules {
		if r.WindowID == uuid.Nil {
			return &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "schedule", Code: "window_required",
				Message: "every delivery day needs a time window",
			}}}
		}
		if r.Weekday < 0 || r.Weekday > 6 {
			return &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "schedule", Code: "invalid_weekday",
			}}}
		}
		if len(plan.Weekdays) > 0 && !slices.Contains(plan.Weekdays, int16(r.Weekday)) {
			return &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "schedule", Code: "not_a_delivery_day",
				Message: "the kitchen does not deliver this plan on that day",
			}}}
		}
		if len(plan.MealSlots) > 0 && !slices.Contains(plan.MealSlots, r.Slot) {
			return &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "schedule", Code: "meal_not_in_plan",
				Message: "this plan does not include that meal",
			}}}
		}
		days[r.Weekday]++
	}

	if len(days) != plan.DaysPerWeek {
		return &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "schedule", Code: "wrong_day_count",
			Message: fmt.Sprintf("this plan covers %d days a week, %d were chosen",
				plan.DaysPerWeek, len(days)),
		}}}
	}
	for weekday, count := range days {
		if count != plan.MealsPerDay {
			return &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "schedule", Code: "wrong_meal_count",
				Message: fmt.Sprintf("this plan covers %d meals a day, %d were chosen for weekday %d",
					plan.MealsPerDay, count, weekday),
			}}}
		}
	}
	return nil
}

func mustJSON(v map[string]any) []byte {
	body, err := json.Marshal(v)
	if err != nil {
		slog.Warn("subscriptions: could not marshal event metadata", "error", err)
		return []byte("{}")
	}
	return body
}

// checkAddresses refuses an address the subscriber does not own or the kitchen
// does not deliver to. Each rule may override the subscription's address, so
// every one is checked. Not owning one reads as not found, so an address id
// cannot be probed.
func (s *Service) checkAddresses(ctx context.Context, tx pgx.Tx, in SubscribeInput, branchID uuid.UUID) error {
	ids := []uuid.UUID{in.AddressID}
	for _, r := range in.Rules {
		if r.AddressID.Valid {
			ids = append(ids, r.AddressID.UUID)
		}
	}
	rows, err := tx.Query(ctx, `
		SELECT a.id,
		       COALESCE(ST_Intersects(b.service_area, a.point), false)
		  FROM addresses a
		  JOIN vendor_branches b ON b.id = $3
		 WHERE a.id = ANY($1) AND a.user_id = $2 AND a.deleted_at IS NULL`,
		ids, in.UserID, branchID)
	if err != nil {
		return fmt.Errorf("subscriptions: check addresses: %w", err)
	}
	defer rows.Close()
	covered := map[uuid.UUID]bool{}
	for rows.Next() {
		var id uuid.UUID
		var ok bool
		if err := rows.Scan(&id, &ok); err != nil {
			return fmt.Errorf("subscriptions: scan address: %w", err)
		}
		covered[id] = ok
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		ok, found := covered[id]
		switch {
		case !found:
			return &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "address_id", Code: "not_found", Message: "address not found",
			}}}
		case !ok:
			return &httpx.ValidationError{Fields: []httpx.FieldError{{
				Field: "address_id", Code: "outside_delivery_area",
				Message: "this kitchen does not deliver to that address",
			}}}
		}
	}
	return nil
}
