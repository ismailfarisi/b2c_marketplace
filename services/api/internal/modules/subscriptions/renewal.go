package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/platform/postgres"
	"github.com/marketplace/api/internal/seam/servicetype"
)

// Renewal.
//
// There is no stored card yet, so "renews automatically" means the next cycle
// is billed RenewalLeadDays before the current one ends and the customer pays
// it with one tap. Deliveries are only scheduled inside paid cycles: an unpaid
// renewal goes past due on its first day and the dunning schedule takes over.

// RenewalLeadDays is how many days before a cycle ends the next one is billed.
const RenewalLeadDays = 3

func renewalInvalid(field, code, msg string) error {
	return &httpx.ValidationError{Fields: []httpx.FieldError{{Field: field, Code: code, Message: msg}}}
}

// quotePlan prices one cycle of a plan at its current list price.
func (s *Service) quotePlan(ctx context.Context, plan PlanTemplate) (servicetype.QuoteResult, error) {
	vertical, err := s.verticals.Get(plan.ServiceType)
	if err != nil {
		return servicetype.QuoteResult{}, fmt.Errorf("%w: service type %q", httpx.ErrNotFound, plan.ServiceType)
	}
	return vertical.Pricing.Quote(ctx, servicetype.PricingContext{
		BranchID: plan.BranchID, ServiceType: plan.ServiceType, Currency: plan.Currency,
		Lines: []servicetype.PricingLine{{
			OfferingID: plan.ID, Name: plan.Name, UnitPriceMinor: plan.PriceMinor,
			TaxRateBP: plan.TaxRateBP, Quantity: 1,
		}},
	})
}

// openRenewal bills the next cycle of an auto-renewing subscription whose
// latest paid cycle ends within RenewalLeadDays. It reprices at the plan's
// current price, so a first-cycle promo or trial does not carry over.
func (s *Service) openRenewal(ctx context.Context, tx pgx.Tx, subID uuid.UUID, now time.Time) (bool, error) {
	var (
		userID, planID, addressID uuid.UUID
		tierID, rotationID        uuid.NullUUID
		status, mode, lastStatus  string
		lastNo, maxNo             int
		lastEnd                   time.Time
	)
	err := tx.QueryRow(ctx, `
		SELECT s.user_id, s.plan_id, s.tier_id, s.address_id, s.status, s.renewal_mode, s.rotation_id,
		       c.cycle_no, c.ends_on, c.status,
		       (SELECT max(cycle_no) FROM subscription_cycles WHERE subscription_id = s.id)
		  FROM subscriptions s
		  JOIN LATERAL (
		       SELECT cycle_no, ends_on, status FROM subscription_cycles
		        WHERE subscription_id = s.id AND status <> 'cancelled'
		        ORDER BY cycle_no DESC LIMIT 1) c ON true
		 WHERE s.id = $1
		 FOR UPDATE OF s`, subID).Scan(&userID, &planID, &tierID, &addressID, &status, &mode,
		&rotationID, &lastNo, &lastEnd, &lastStatus, &maxNo)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("subscriptions: renewal load: %w", err)
	}
	if status != "active" || mode != "auto" || rotationID.Valid || lastStatus != "paid" {
		return false, nil
	}
	plan, err := s.repo.LoadPlan(ctx, tx, planID, tierID)
	if errors.Is(err, httpx.ErrNotFound) {
		slog.WarnContext(ctx, "renewal skipped: plan no longer on sale", "subscription_id", subID)
		return false, nil
	}
	if err != nil {
		return false, err
	}
	loc, lerr := time.LoadLocation(plan.Timezone)
	if lerr != nil {
		loc = time.UTC
	}
	today := dayIn(now, loc)
	end := dayIn(lastEnd, loc)
	if end.After(today.AddDate(0, 0, RenewalLeadDays)) {
		return false, nil
	}
	start := end.AddDate(0, 0, 1)
	if start.Before(today) {
		start = today
	}
	rules, err := s.repo.LoadRules(ctx, tx, subID)
	if err != nil {
		return false, err
	}
	quote, err := s.quotePlan(ctx, plan)
	if err != nil {
		return false, fmt.Errorf("subscriptions: renewal quote: %w", err)
	}
	cycle := FirstCycle(start, plan.CycleDays, rules, loc, quote.TotalMinor, plan.Currency)
	cycle.No = max(lastNo, maxNo) + 1
	if cycle.DeliverableDays == 0 {
		return false, nil
	}
	orderID, cycleID, err := s.openCycle(ctx, tx, subID, plan,
		SubscribeInput{UserID: userID, AddressID: addressID}, cycle, quote)
	if err != nil {
		return false, err
	}
	if err := s.notifyRenewalDue(ctx, tx, subID, userID, plan, cycle, quote.TotalMinor); err != nil {
		return false, err
	}
	if err := s.repo.RecordEvent(ctx, tx, subID, "renewal_opened", "active", "active",
		"system", uuid.NullUUID{}, mustJSON(map[string]any{
			"cycle_id": cycleID, "cycle_no": cycle.No, "order_id": orderID})); err != nil {
		return false, err
	}
	return true, s.insertOutbox(ctx, tx, subID, "subscription.renewal_opened", map[string]any{
		"subscription_id": subID, "user_id": userID, "cycle_no": cycle.No,
		"order_id": orderID, "amount_minor": quote.TotalMinor, "currency": plan.Currency,
		"starts_on": cycle.Starts.Format("2006-01-02"),
	})
}

// OpenRenewals bills every auto-renewing subscription that is due.
func (s *Service) OpenRenewals(ctx context.Context, now time.Time, batch int) (int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT s.id
		  FROM subscriptions s
		  JOIN LATERAL (
		       SELECT ends_on, status FROM subscription_cycles
		        WHERE subscription_id = s.id AND status <> 'cancelled'
		        ORDER BY cycle_no DESC LIMIT 1) c ON true
		 WHERE s.status = 'active' AND s.renewal_mode = 'auto' AND s.rotation_id IS NULL
		   AND c.status = 'paid' AND c.ends_on <= CURRENT_DATE + $2::int
		 ORDER BY c.ends_on
		 LIMIT $1`, batch, RenewalLeadDays)
	if err != nil {
		return 0, fmt.Errorf("subscriptions: due renewals: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return 0, fmt.Errorf("subscriptions: due renewals: %w", err)
	}
	opened := 0
	for _, id := range ids {
		var ok bool
		err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
			var err error
			ok, err = s.openRenewal(ctx, tx, id, now)
			return err
		})
		if err != nil {
			slog.ErrorContext(ctx, "renewal failed", "subscription_id", id, "error", err)
			continue
		}
		if ok {
			opened++
		}
	}
	return opened, nil
}

// voidUnpaidRenewals cancels a subscription's unpaid renewal cycles and their
// billing orders so they can no longer be paid.
func voidUnpaidRenewals(ctx context.Context, tx pgx.Tx, subID uuid.UUID) error {
	return voidUnpaidCycles(ctx, tx, subID, 2)
}

// voidUnpaidCycles cancels the unpaid cycles numbered fromCycle and up, with
// their billing orders and quotes.
func voidUnpaidCycles(ctx context.Context, tx pgx.Tx, subID uuid.UUID, fromCycle int) error {
	rows, err := tx.Query(ctx, `
		UPDATE subscription_cycles SET status = 'cancelled'
		 WHERE subscription_id = $1 AND cycle_no >= $2 AND status IN ('pending', 'past_due')
		RETURNING order_id`, subID, fromCycle)
	if err != nil {
		return fmt.Errorf("subscriptions: void unpaid cycles: %w", err)
	}
	orders, err := pgx.CollectRows(rows, pgx.RowTo[*uuid.UUID])
	if err != nil {
		return fmt.Errorf("subscriptions: void renewal: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(orders))
	for _, o := range orders {
		if o != nil {
			ids = append(ids, *o)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE orders SET status = 'cancelled'
		 WHERE id = ANY($1) AND status = 'pending_payment'`, ids); err != nil {
		return fmt.Errorf("subscriptions: void renewal order: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE price_quotes SET state = 'void' WHERE order_id = ANY($1) AND state = 'estimated'`, ids); err != nil {
		return fmt.Errorf("subscriptions: void renewal quote: %w", err)
	}
	return nil
}

// SetRenewal turns automatic renewal on or off. Off ends the subscription
// with its last paid cycle and voids an unpaid next cycle; on bills the next
// cycle straight away if it is already due.
func (s *Service) SetRenewal(ctx context.Context, userID, subID uuid.UUID, auto bool, now time.Time) error {
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		live, err := s.loadOwned(ctx, tx, userID, subID)
		if err != nil {
			return err
		}
		if live.RotationID.Valid {
			return renewalInvalid("renewal", "rotation_week", "renewal is managed on the rotation")
		}
		if live.Status != "active" && live.Status != "paused" {
			return httpx.Coded(httpx.ErrConflict, "not_live", "Only a running subscription can change renewal.")
		}
		if auto {
			if _, err := tx.Exec(ctx, `
				UPDATE subscriptions SET renewal_mode = 'auto', end_date = NULL, updated_at = now()
				 WHERE id = $1`, subID); err != nil {
				return fmt.Errorf("subscriptions: renewal on: %w", err)
			}
			if err := s.repo.RecordEvent(ctx, tx, subID, "renewal_on", live.Status, live.Status,
				"customer", uuid.NullUUID{UUID: userID, Valid: true}, mustJSON(map[string]any{})); err != nil {
				return err
			}
			_, err := s.openRenewal(ctx, tx, subID, now)
			return err
		}
		if err := voidUnpaidRenewals(ctx, tx, subID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE subscriptions
			   SET renewal_mode = 'manual', updated_at = now(),
			       end_date = (SELECT max(ends_on) FROM subscription_cycles
			                    WHERE subscription_id = $1 AND status = 'paid')
			 WHERE id = $1`, subID); err != nil {
			return fmt.Errorf("subscriptions: renewal off: %w", err)
		}
		return s.repo.RecordEvent(ctx, tx, subID, "renewal_off", live.Status, live.Status,
			"customer", uuid.NullUUID{UUID: userID, Valid: true}, mustJSON(map[string]any{}))
	})
}

// canRenew is whether a one-tap renew is offered for a subscription.
func canRenew(status, mode string, rotation, renewedBy *uuid.UUID) bool {
	if rotation != nil || renewedBy != nil {
		return false
	}
	switch status {
	case "completed", "cancelled":
		return true
	case "active", "paused":
		return mode != "auto"
	}
	return false
}

// Renew starts a new subscription with the same kitchen, plan, address and
// schedule, beginning the day after the old one's last paid day (or tomorrow).
// It renews automatically and starts pending payment like any checkout.
func (s *Service) Renew(ctx context.Context, userID, subID uuid.UUID, now time.Time) (*Result, error) {
	var in SubscribeInput
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var (
			planID, addressID  uuid.UUID
			tierID, rotationID uuid.NullUUID
			status, mode, tz   string
			paidEnd            *time.Time
			successor          *uuid.UUID
			companyID          *uuid.UUID
		)
		err := tx.QueryRow(ctx, `
			SELECT s.plan_id, s.tier_id, s.address_id, s.status, s.renewal_mode, s.rotation_id, s.timezone,
			       (SELECT max(ends_on) FROM subscription_cycles WHERE subscription_id = s.id AND status = 'paid'),
			       (SELECT n.id FROM subscriptions n WHERE n.renewed_from_id = s.id AND n.status <> 'cancelled' LIMIT 1),
			       s.company_id
			  FROM subscriptions s WHERE s.id = $1 AND s.user_id = $2`, subID, userID).
			Scan(&planID, &tierID, &addressID, &status, &mode, &rotationID, &tz, &paidEnd, &successor, &companyID)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("subscriptions: renew load: %w", err)
		}
		if successor != nil {
			return httpx.Coded(httpx.ErrConflict, "already_renewed", "This subscription has already been renewed.")
		}
		if companyID != nil {
			return httpx.Coded(httpx.ErrConflict, "company_lunch",
				"Office lunches are started again from your company's lunch page.")
		}
		var rot *uuid.UUID
		if rotationID.Valid {
			rot = &rotationID.UUID
		}
		if !canRenew(status, mode, rot, nil) {
			return httpx.Coded(httpx.ErrConflict, "not_renewable", "This subscription renews automatically or cannot be renewed.")
		}
		loc, lerr := time.LoadLocation(tz)
		if lerr != nil {
			loc = time.UTC
		}
		start := dayIn(now, loc).AddDate(0, 0, 1)
		if paidEnd != nil && (status == "active" || status == "paused") {
			if next := dayIn(*paidEnd, loc).AddDate(0, 0, 1); next.After(start) {
				start = next
			}
		}
		rules, err := s.repo.LoadRules(ctx, tx, subID)
		if err != nil {
			return err
		}
		in = SubscribeInput{
			UserID: userID, PlanID: planID, TierID: tierID, AddressID: addressID,
			StartDate: start, Rules: rules, RenewalMode: "auto",
			renewedFrom: uuid.NullUUID{UUID: subID, Valid: true},
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	res, err := s.Subscribe(ctx, in)
	if errors.Is(err, httpx.ErrNotFound) {
		return nil, renewalInvalid("plan_id", "plan_unavailable", "this plan is no longer on sale")
	}
	if err != nil && isUniqueViolation(err, "subscriptions_renewed_from_key") {
		return nil, httpx.Coded(httpx.ErrConflict, "already_renewed", "This subscription has already been renewed.")
	}
	return res, err
}

// SettleRenewals moves unpaid renewals into dunning and closes subscriptions
// and rotations whose last day has passed.
func (s *Service) SettleRenewals(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `
		UPDATE subscription_cycles SET status = 'past_due', next_attempt_at = now()
		 WHERE status = 'pending' AND cycle_no > 1 AND starts_on <= CURRENT_DATE`); err != nil {
		return fmt.Errorf("subscriptions: overdue renewals: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `
		WITH ended AS (
		    UPDATE subscriptions SET status = 'completed', updated_at = now()
		     WHERE status IN ('active', 'paused') AND end_date IS NOT NULL AND end_date < CURRENT_DATE
		    RETURNING id)
		INSERT INTO subscription_events (subscription_id, event_type, from_status, to_status, actor_type, metadata)
		SELECT id, 'completed', 'active', 'completed', 'system', '{}'::jsonb FROM ended`); err != nil {
		return fmt.Errorf("subscriptions: complete ended: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE meal_rotations SET status = 'completed', updated_at = now()
		 WHERE status = 'active' AND end_date < CURRENT_DATE`); err != nil {
		return fmt.Errorf("subscriptions: complete rotations: %w", err)
	}
	// An automatic rotation renewal nobody paid for before it started.
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id FROM meal_rotations
		 WHERE status = 'pending_payment' AND renewed_from_id IS NOT NULL
		   AND start_date < CURRENT_DATE`)
	if err != nil {
		return fmt.Errorf("subscriptions: lapsed rotation renewals: %w", err)
	}
	type lapsed struct{ id, user uuid.UUID }
	var list []lapsed
	for rows.Next() {
		var l lapsed
		if err := rows.Scan(&l.id, &l.user); err != nil {
			rows.Close()
			return fmt.Errorf("subscriptions: lapsed rotation renewals: %w", err)
		}
		list = append(list, l)
	}
	rows.Close()
	for _, l := range list {
		if err := s.CancelRotation(ctx, l.user, l.id, "renewal not paid before it started"); err != nil {
			slog.ErrorContext(ctx, "lapsed rotation renewal cancel failed", "rotation_id", l.id, "error", err)
		}
	}
	return nil
}

// --- rotations ----------------------------------------------------------------

// RenewRotation starts the next rotation with the same four kitchens, address,
// days and meals. It begins the day after this one ends (or tomorrow), and when
// the two run back to back the kitchen that cooked the last week does not cook
// the first. The new rotation is pending payment and paid in one go.
func (s *Service) RenewRotation(ctx context.Context, userID, id uuid.UUID, now time.Time) (Rotation, error) {
	r, err := s.MyRotation(ctx, userID, id)
	if err != nil {
		return Rotation{}, err
	}
	if r.RenewedByID != nil {
		return Rotation{}, httpx.Coded(httpx.ErrConflict, "already_renewed", "This rotation has already been renewed.")
	}
	if r.Status == "pending_payment" {
		return Rotation{}, httpx.Coded(httpx.ErrConflict, "not_paid", "Pay for this rotation before renewing it.")
	}
	if len(r.Weeks) != RotationKitchens {
		return Rotation{}, httpx.Coded(httpx.ErrConflict, "not_renewable", "This rotation cannot be renewed.")
	}
	var addressID uuid.UUID
	var tz string
	if err := s.pool.QueryRow(ctx, `
		SELECT r.address_id, s.timezone FROM meal_rotations r
		  JOIN subscriptions s ON s.rotation_id = r.id
		 WHERE r.id = $1 ORDER BY s.rotation_week LIMIT 1`, id).Scan(&addressID, &tz); err != nil {
		return Rotation{}, fmt.Errorf("subscriptions: rotation address: %w", err)
	}
	loc, lerr := time.LoadLocation(tz)
	if lerr != nil {
		loc = time.UTC
	}
	end, err := time.ParseInLocation("2006-01-02", r.EndDate, loc)
	if err != nil {
		return Rotation{}, fmt.Errorf("subscriptions: rotation end: %w", err)
	}
	start := end.AddDate(0, 0, 1)
	notFirst := r.Weeks[RotationKitchens-1].BranchID
	if tomorrow := dayIn(now, loc).AddDate(0, 0, 1); start.Before(tomorrow) {
		start, notFirst = tomorrow, uuid.Nil
	}
	plans := make([]uuid.UUID, 0, RotationKitchens)
	for _, w := range r.Weeks {
		plans = append(plans, w.PlanID)
	}
	return s.createRotation(ctx, RotationInput{
		UserID: userID, AddressID: addressID, PlanIDs: plans, StartDate: start,
		Weekdays: r.Weekdays, Slots: r.MealSlots,
	}, uuid.NullUUID{UUID: id, Valid: true}, notFirst)
}

// SetRotationRenewal turns automatic renewal of a rotation on or off. Off also
// cancels an automatic renewal that has not been paid yet.
func (s *Service) SetRotationRenewal(ctx context.Context, userID, id uuid.UUID, auto bool) error {
	r, err := s.MyRotation(ctx, userID, id)
	if err != nil {
		return err
	}
	if r.Status != "active" && r.Status != "pending_payment" {
		return httpx.Coded(httpx.ErrConflict, "not_live", "Only a running rotation can change renewal.")
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE meal_rotations SET auto_renew = $3, updated_at = now()
		 WHERE id = $1 AND user_id = $2`, id, userID, auto); err != nil {
		return fmt.Errorf("subscriptions: rotation renewal: %w", err)
	}
	if !auto && r.RenewedByID != nil && r.RenewedByStatus != nil && *r.RenewedByStatus == "pending_payment" {
		return s.CancelRotation(ctx, userID, *r.RenewedByID, "automatic renewal turned off")
	}
	return nil
}

// OpenRotationRenewals renews every auto-renewing rotation that ends within
// RenewalLeadDays and has not been renewed yet. Only rotations still running
// qualify, so a lapsed renewal is not recreated after it is cancelled.
func (s *Service) OpenRotationRenewals(ctx context.Context, now time.Time, batch int) (int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.user_id FROM meal_rotations r
		 WHERE r.status = 'active' AND r.auto_renew
		   AND r.end_date >= CURRENT_DATE AND r.end_date <= CURRENT_DATE + $2::int
		   AND NOT EXISTS (
		       SELECT 1 FROM meal_rotations n
		        WHERE n.renewed_from_id = r.id AND n.status <> 'cancelled')
		 ORDER BY r.end_date
		 LIMIT $1`, batch, RenewalLeadDays)
	if err != nil {
		return 0, fmt.Errorf("subscriptions: due rotation renewals: %w", err)
	}
	type due struct{ id, user uuid.UUID }
	var list []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.user); err != nil {
			rows.Close()
			return 0, fmt.Errorf("subscriptions: due rotation renewals: %w", err)
		}
		list = append(list, d)
	}
	rows.Close()
	opened := 0
	for _, d := range list {
		if _, err := s.RenewRotation(ctx, d.user, d.id, now); err != nil {
			slog.ErrorContext(ctx, "rotation renewal failed", "rotation_id", d.id, "error", err)
			continue
		}
		opened++
	}
	return opened, nil
}
