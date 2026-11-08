package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/ledger"
	"github.com/marketplace/api/internal/platform/postgres"
	"github.com/marketplace/api/internal/seam/servicetype"
)

// SwapCutoffDays is how many days before a rotation week starts its kitchen
// can still be changed: the kitchen has to know its numbers before it shops.
// Measured in the kitchen's time zone, calendar day to calendar day.
const SwapCutoffDays = 3

// SwapOption is a kitchen that could cook one rotation week instead of the
// kitchen currently booked for it.
type SwapOption struct {
	BranchID      uuid.UUID `json:"branch_id"`
	KitchenName   string    `json:"kitchen_name"`
	KitchenNameAr *string   `json:"kitchen_name_ar"`
	Cuisines      []string  `json:"cuisines"`
	CoverImageURL *string   `json:"cover_image_url"`
	Rating        *float64  `json:"rating"`
	RatingCount   int       `json:"rating_count"`
	PlanID        uuid.UUID `json:"plan_id"`
	PlanName      string    `json:"plan_name"`
	// PriceMinor is the kitchen's weekly rotation price, tax included.
	PriceMinor int64 `json:"price_minor"`
	// DifferenceMinor is PriceMinor less the current week's price: positive is
	// paid on swapping, negative is credited to the wallet.
	DifferenceMinor int64  `json:"difference_minor"`
	Currency        string `json:"currency"`
}

// SwapOptions is what a customer may change one rotation week to.
type SwapOptions struct {
	Week              int          `json:"week"`
	CurrentPriceMinor int64        `json:"current_price_minor"`
	SwapUntil         string       `json:"swap_until"`
	Currency          string       `json:"currency"`
	Items             []SwapOption `json:"items"`
}

// SwapResult is a swapped week: the rotation as it now stands and what, if
// anything, is still to pay on the replacement week's first order.
type SwapResult struct {
	Rotation Rotation
	OrderID  uuid.UUID
	DueMinor int64
	Currency string
}

type swapQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type swapWeekRow struct {
	week       int
	subID      uuid.UUID
	planID     uuid.UUID
	branchID   uuid.UUID
	start, end time.Time
	status     string
	tz         string
	priceMinor int64
}

type swapTarget struct {
	rotationStatus string
	addressID      uuid.UUID
	slots          []string
	weekdays       []int
	currency       string
	target         swapWeekRow
	others         []swapWeekRow
	swappedTo      uuid.NullUUID
	loc            *time.Location
	// neighbours are kitchens cooking the week right before week 1 (the
	// previous rotation's last week) or right after week 4 (the prepared
	// renewal's first week): the same kitchen must not cook two weeks running.
	neighbours []uuid.UUID
}

func swapConflict(code, msg string) error { return httpx.Coded(httpx.ErrConflict, code, msg) }

// swapUntil is the last day a week starting on start can still be swapped.
func swapUntil(start time.Time) time.Time { return start.AddDate(0, 0, -SwapCutoffDays) }

// swapOpen says whether a week of a rotation in rotationStatus can be swapped
// at now. Only the window and the states: ownership and "already swapped" are
// the caller's.
func swapOpen(rotationStatus, weekStatus string, start time.Time, loc *time.Location, now time.Time) error {
	if rotationStatus != "active" {
		return swapConflict("rotation_not_active", "Only a running rotation can change kitchens.")
	}
	if weekStatus == "cancelled" || weekStatus == "completed" {
		return swapConflict("week_started", "This week can no longer be changed.")
	}
	today := dayIn(now, loc)
	if !today.Before(start) {
		return swapConflict("week_started", "This week has already started.")
	}
	if today.After(swapUntil(start)) {
		return swapConflict("swap_cutoff_passed",
			fmt.Sprintf("A week's kitchen can be changed until %d days before it starts.", SwapCutoffDays))
	}
	return nil
}

func (s *Service) loadSwapTarget(ctx context.Context, q swapQuerier, userID, rotationID uuid.UUID, week int, lock bool) (swapTarget, error) {
	var t swapTarget
	if week < 1 || week > RotationKitchens {
		return t, httpx.ErrNotFound
	}
	sql := `SELECT status, address_id, meal_slots, delivery_weekdays, currency
		  FROM meal_rotations WHERE id = $1 AND user_id = $2`
	if lock {
		sql += ` FOR UPDATE`
	}
	var weekdays []int16
	err := q.QueryRow(ctx, sql, rotationID, userID).Scan(&t.rotationStatus, &t.addressID, &t.slots, &weekdays, &t.currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, httpx.ErrNotFound
	}
	if err != nil {
		return t, fmt.Errorf("subscriptions: swap rotation: %w", err)
	}
	for _, d := range weekdays {
		t.weekdays = append(t.weekdays, int(d))
	}
	rows, err := q.Query(ctx, `
		SELECT s.rotation_week, s.id, s.plan_id, s.branch_id, s.start_date, s.end_date, s.status, s.timezone,
		       COALESCE(c.amount_minor, 0)
		  FROM subscriptions s
		  LEFT JOIN subscription_cycles c ON c.subscription_id = s.id AND c.cycle_no = 1
		 WHERE s.rotation_id = $1 AND s.status <> 'cancelled'
		 ORDER BY s.rotation_week`, rotationID)
	if err != nil {
		return t, fmt.Errorf("subscriptions: swap weeks: %w", err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var w swapWeekRow
		if err := rows.Scan(&w.week, &w.subID, &w.planID, &w.branchID, &w.start, &w.end, &w.status, &w.tz, &w.priceMinor); err != nil {
			return t, fmt.Errorf("subscriptions: scan swap week: %w", err)
		}
		if w.week == week {
			t.target, found = w, true
		} else {
			t.others = append(t.others, w)
		}
	}
	if err := rows.Err(); err != nil {
		return t, err
	}
	if err := q.QueryRow(ctx, `
		SELECT new_subscription_id FROM meal_rotation_swaps
		 WHERE rotation_id = $1 AND rotation_week = $2`, rotationID, week).Scan(&t.swappedTo.UUID); err == nil {
		t.swappedTo.Valid = true
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return t, fmt.Errorf("subscriptions: swap record: %w", err)
	}
	if !found {
		if t.swappedTo.Valid || t.rotationStatus == "cancelled" {
			return t, swapConflict("week_started", "This week can no longer be changed.")
		}
		return t, httpx.ErrNotFound
	}
	if t.loc, err = time.LoadLocation(t.target.tz); err != nil {
		t.loc = time.UTC
	}
	t.target.start, t.target.end = dayIn(t.target.start, t.loc), dayIn(t.target.end, t.loc)
	switch week {
	case 1:
		rows, err = q.Query(ctx, `
			SELECT s.branch_id FROM subscriptions s
			 WHERE s.rotation_id = (SELECT renewed_from_id FROM meal_rotations WHERE id = $1)
			   AND s.rotation_week = $2 AND s.status <> 'cancelled'`, rotationID, RotationKitchens)
	case RotationKitchens:
		rows, err = q.Query(ctx, `
			SELECT s.branch_id FROM subscriptions s
			  JOIN meal_rotations n ON n.id = s.rotation_id
			 WHERE n.renewed_from_id = $1 AND n.status <> 'cancelled'
			   AND s.rotation_week = 1 AND s.status <> 'cancelled'`, rotationID)
	default:
		return t, nil
	}
	if err != nil {
		return t, fmt.Errorf("subscriptions: swap neighbours: %w", err)
	}
	t.neighbours, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return t, fmt.Errorf("subscriptions: swap neighbours: %w", err)
	}
	return t, nil
}

// SwapOptions lists the kitchens one rotation week could change to: on the
// rotation, delivering to the address on the rotation's days and meals, not
// already cooking another week, best rated first.
func (s *Service) SwapOptions(ctx context.Context, userID, rotationID uuid.UUID, week int) (SwapOptions, error) {
	t, err := s.loadSwapTarget(ctx, s.pool, userID, rotationID, week, false)
	if err != nil {
		return SwapOptions{}, err
	}
	if t.swappedTo.Valid {
		return SwapOptions{}, swapConflict("already_swapped", "This week's kitchen has already been changed once.")
	}
	if err := swapOpen(t.rotationStatus, t.target.status, t.target.start, t.loc, time.Now()); err != nil {
		return SwapOptions{}, err
	}
	exclude := []uuid.UUID{t.target.branchID}
	for _, o := range t.others {
		exclude = append(exclude, o.branchID)
	}
	exclude = append(exclude, t.neighbours...)
	var serviceType string
	if err := s.pool.QueryRow(ctx, `SELECT service_type FROM subscriptions WHERE id = $1`, t.target.subID).Scan(&serviceType); err != nil {
		return SwapOptions{}, fmt.Errorf("subscriptions: swap service type: %w", err)
	}
	vertical, err := s.verticals.Get(serviceType)
	if err != nil {
		return SwapOptions{}, fmt.Errorf("%w: service type %q", httpx.ErrNotFound, serviceType)
	}
	weekdays := make([]int16, 0, len(t.weekdays))
	for _, d := range t.weekdays {
		weekdays = append(weekdays, int16(d))
	}
	// One plan per kitchen: its cheapest rotation plan that fits the schedule.
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (b.id) b.id, b.name, b.name_ar, b.cuisines, b.cover_image_url,
		       b.rating::float8, b.rating_count,
		       p.id, p.name, p.rotation_week_price_minor, p.tax_rate_bp, p.currency
		  FROM subscription_plans p
		  JOIN vendor_branches b ON b.id = p.branch_id
		  JOIN vendors v ON v.id = b.vendor_id
		  JOIN addresses a ON a.id = $1
		 WHERE p.is_active AND p.rotation_enabled AND p.rotation_week_price_minor IS NOT NULL
		   AND p.service_type = $2 AND p.currency = $3
		   AND p.delivery_weekdays @> $4::smallint[]
		   AND p.meal_slots @> $5::text[]
		   AND b.is_active AND b.deleted_at IS NULL
		   AND v.status = 'active' AND v.deleted_at IS NULL
		   AND b.id <> ALL ($6::uuid[])
		   AND b.service_area IS NOT NULL AND ST_Intersects(b.service_area, a.point)
		   AND EXISTS (SELECT 1 FROM time_windows w WHERE w.branch_id = b.id AND w.is_active)
		   AND NOT EXISTS (SELECT 1 FROM meal_tastes t
		                    WHERE t.user_id = $7 AND t.subject_kind = 'kitchen'
		                      AND t.subject_id = b.id AND t.sentiment = 'avoid')
		 ORDER BY b.id, p.rotation_week_price_minor, p.name`,
		t.addressID, serviceType, t.currency, weekdays, t.slots, exclude, userID)
	if err != nil {
		return SwapOptions{}, fmt.Errorf("subscriptions: swap options: %w", err)
	}
	defer rows.Close()
	out := SwapOptions{Week: week, CurrentPriceMinor: t.target.priceMinor, Currency: t.currency,
		SwapUntil: swapUntil(t.target.start).Format("2006-01-02"), Items: []SwapOption{}}
	for rows.Next() {
		var o SwapOption
		var weekPrice int64
		var taxBP int
		if err := rows.Scan(&o.BranchID, &o.KitchenName, &o.KitchenNameAr, &o.Cuisines, &o.CoverImageURL,
			&o.Rating, &o.RatingCount, &o.PlanID, &o.PlanName, &weekPrice, &taxBP, &o.Currency); err != nil {
			return SwapOptions{}, fmt.Errorf("subscriptions: scan swap option: %w", err)
		}
		q, err := vertical.Pricing.Quote(ctx, servicetype.PricingContext{
			BranchID: o.BranchID, ServiceType: serviceType, Currency: o.Currency,
			Lines: []servicetype.PricingLine{{
				OfferingID: o.PlanID, Name: o.PlanName, UnitPriceMinor: weekPrice, TaxRateBP: taxBP, Quantity: 1,
			}},
		})
		if err != nil {
			return SwapOptions{}, fmt.Errorf("subscriptions: swap quote: %w", err)
		}
		o.PriceMinor = q.TotalMinor
		o.DifferenceMinor = q.TotalMinor - t.target.priceMinor
		if o.Cuisines == nil {
			o.Cuisines = []string{}
		}
		out.Items = append(out.Items, o)
	}
	if err := rows.Err(); err != nil {
		return SwapOptions{}, err
	}
	sort.SliceStable(out.Items, func(i, j int) bool {
		a, b := out.Items[i], out.Items[j]
		switch {
		case (a.Rating != nil) != (b.Rating != nil):
			return a.Rating != nil
		case a.Rating != nil && *a.Rating != *b.Rating:
			return *a.Rating > *b.Rating
		case a.RatingCount != b.RatingCount:
			return a.RatingCount > b.RatingCount
		}
		return a.KitchenName < b.KitchenName
	})
	return out, nil
}

// SwapRotationWeek changes the kitchen of one future week of a running
// rotation. The booked week's subscription is cancelled as any subscription
// is, so its unearned money is credited to the wallet, and a replacement
// subscription takes its rotation_week and dates. The wallet credit pays for
// the replacement: a dearer kitchen leaves the difference to collect on the
// new week's order (DueMinor), a cheaper one leaves it in the wallet. The
// cancelled kitchen recognises nothing for the week, so it is never paid for
// it; the new kitchen is paid as its meals go out.
//
// A week is swapped at most once; repeating the same swap returns the result
// of the first. If the first swap's difference was never collected (the
// customer abandoned or failed the payment) the repeat returns the same
// order and amount so the payment can be retried; payments.Authorize locks
// the quote, so two retries cannot both record a hold.
func (s *Service) SwapRotationWeek(ctx context.Context, userID, rotationID uuid.UUID, week int, planID uuid.UUID) (SwapResult, error) {
	var res SwapResult
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		t, err := s.loadSwapTarget(ctx, tx, userID, rotationID, week, true)
		if err != nil {
			return err
		}
		if t.swappedTo.Valid {
			if t.target.planID != planID {
				return swapConflict("already_swapped", "This week's kitchen has already been changed once.")
			}
			res.OrderID, res.DueMinor, res.Currency, err = s.swapDue(ctx, tx, t.swappedTo.UUID)
			return err
		}
		now := time.Now()
		if err := swapOpen(t.rotationStatus, t.target.status, t.target.start, t.loc, now); err != nil {
			return err
		}
		if len(t.others) != RotationKitchens-1 {
			return swapConflict("rotation_not_active", "Only a running rotation can change kitchens.")
		}
		if planID == t.target.planID {
			return rotationInvalid("plan_id", "same_kitchen", "choose a different kitchen for this week")
		}
		in := RotationInput{UserID: userID, AddressID: t.addressID, PlanIDs: []uuid.UUID{planID},
			StartDate: t.target.start, Weekdays: t.weekdays, Slots: t.slots, ContinuesRotation: true}
		for _, o := range t.others {
			in.PlanIDs = append(in.PlanIDs, o.planID)
		}
		plans, _, loc, err := s.prepareRotation(ctx, tx, in)
		if err != nil {
			return err
		}
		p := plans[0]
		if p.BranchID == t.target.branchID {
			return rotationInvalid("plan_id", "same_kitchen", "choose a different kitchen for this week")
		}
		for _, n := range t.neighbours {
			if n == p.BranchID {
				return rotationInvalid("plan_id", "follows_last_week",
					"this kitchen cooks the week right next to this one")
			}
		}

		paid, err := s.paidCycles(ctx, tx, t.target.subID)
		if err != nil {
			return err
		}
		if len(paid) == 0 {
			return swapConflict("rotation_not_active", "Pay for this rotation before changing a kitchen.")
		}
		if paid[0].RecognisedMinor > 0 {
			return swapConflict("week_started", "This week has already started.")
		}
		oldPrice, newPrice := paid[0].AmountMinor, p.Quote.TotalMinor
		if paid[0].Currency != p.Currency {
			return rotationInvalid("plan_ids", "incompatible_plans", "these plans cannot be combined in one rotation")
		}

		owner := uuid.NullUUID{UUID: userID, Valid: true}
		if err := s.cancelInTx(ctx, tx, t.target.subID, owner, "customer", userID, "kitchen_swap", ""); err != nil {
			return err
		}

		subID := uuid.New()
		if err := s.repo.InsertSubscription(ctx, tx, insertParams{
			ID: subID, UserID: userID, Plan: p.PlanTemplate,
			AddressID: t.addressID, Start: t.target.start, End: t.target.end, RenewalMode: "none",
			RotationID: uuid.NullUUID{UUID: rotationID, Valid: true}, RotationWeek: week,
		}); err != nil {
			return err
		}
		if err := s.repo.InsertRules(ctx, tx, subID, p.Rules); err != nil {
			return err
		}
		due := newPrice - oldPrice
		if due < 0 {
			due = 0
		}
		quote := p.Quote
		quote.AuthorizeMinor = due
		cycle := FirstCycle(t.target.start, 7, p.Rules, loc, newPrice, p.Currency)
		orderID, cycleID, err := s.openCycle(ctx, tx, subID, p.PlanTemplate,
			SubscribeInput{UserID: userID, AddressID: t.addressID}, cycle, quote)
		if err != nil {
			return err
		}
		if applied := min(oldPrice, newPrice); applied > 0 {
			if _, err := ledger.PostSubscriptionWalletApplied(ctx, tx, ledger.SubscriptionCredit{
				SubscriptionID: subID, CycleID: cycleID, UserID: userID,
				Currency: p.Currency, AmountMinor: applied, Reason: "rotation week swap",
			}); err != nil {
				return err
			}
		}
		if err := s.repo.RecordEvent(ctx, tx, subID, "created", "", "pending_payment",
			"customer", owner, mustJSON(map[string]any{"plan_id": p.ID, "cycle_id": cycleID,
				"rotation_id": rotationID, "rotation_week": week, "swapped_from": t.target.subID})); err != nil {
			return err
		}
		if due == 0 {
			if err := activateSwapped(ctx, tx, subID, orderID, cycleID, userID, min(oldPrice, newPrice), p.Currency); err != nil {
				return err
			}
		}
		if err := s.insertOutbox(ctx, tx, subID, "subscription.created", map[string]any{
			"subscription_id": subID, "user_id": userID,
			"branch_id": p.BranchID, "vendor_id": p.VendorID, "plan_id": p.ID,
			"order_id": orderID, "amount_minor": newPrice,
			"currency": p.Currency, "rotation_id": rotationID, "swapped_from": t.target.subID,
		}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO meal_rotation_swaps (rotation_id, rotation_week, old_subscription_id,
			    new_subscription_id, old_amount_minor, new_amount_minor, currency)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			rotationID, week, t.target.subID, subID, oldPrice, newPrice, p.Currency); err != nil {
			if isUniqueViolation(err, "meal_rotation_swaps_rotation_id_rotation_week_key") {
				return swapConflict("already_swapped", "This week's kitchen has already been changed once.")
			}
			return fmt.Errorf("subscriptions: record swap: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE meal_rotations SET amount_minor = amount_minor + $2, updated_at = now()
			 WHERE id = $1`, rotationID, newPrice-oldPrice); err != nil {
			return fmt.Errorf("subscriptions: swap total: %w", err)
		}
		res.OrderID, res.DueMinor, res.Currency = orderID, due, p.Currency
		return nil
	})
	if err != nil {
		return res, err
	}
	res.Rotation, err = s.MyRotation(ctx, userID, rotationID)
	return res, err
}

// swapDue is what is still owed on a swapped-in week's first order.
func (s *Service) swapDue(ctx context.Context, tx pgx.Tx, subID uuid.UUID) (uuid.UUID, int64, string, error) {
	var orderID uuid.UUID
	var due int64
	var status, currency string
	err := tx.QueryRow(ctx, `
		SELECT o.id, o.status, q.authorized_minor, q.currency
		  FROM subscription_cycles c
		  JOIN orders o ON o.id = c.order_id
		  JOIN price_quotes q ON q.order_id = o.id AND q.superseded_by IS NULL
		 WHERE c.subscription_id = $1 AND c.cycle_no = 1`, subID).Scan(&orderID, &status, &due, &currency)
	if err != nil {
		return uuid.Nil, 0, "", fmt.Errorf("subscriptions: swap order: %w", err)
	}
	if status != "pending_payment" {
		// Paid (or cancelled): nothing to collect, and nothing to authorize
		// again.
		due = 0
	}
	return orderID, due, currency, nil
}

// walletProvider mirrors payments.WalletProvider: the provider recorded on a
// payment settled entirely from the customer's wallet.
const walletProvider = "customer_wallet"

// activateSwapped settles a replacement week paid entirely from the wallet,
// the way a paid order activates a subscription.
func activateSwapped(ctx context.Context, tx pgx.Tx, subID, orderID, cycleID, userID uuid.UUID, walletMinor int64, currency string) error {
	var quoteID uuid.UUID
	if err := tx.QueryRow(ctx, `
		UPDATE price_quotes SET state = 'authorized' WHERE order_id = $1 RETURNING id`, orderID).Scan(&quoteID); err != nil {
		return fmt.Errorf("subscriptions: swap quote: %w", err)
	}
	// The same audit row payments.AuthorizeFromWallet writes, so a week paid
	// entirely from the wallet shows up in payment history like any other.
	if walletMinor > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO payments (order_id, quote_id, user_id, kind, status, provider, provider_ref, amount_minor, currency)
			VALUES ($1, $2, $3, 'authorization', 'succeeded', $4, $5, $6, $7)`,
			orderID, quoteID, userID, walletProvider, "wallet:"+userID.String()+":"+orderID.String(),
			walletMinor, currency); err != nil {
			return fmt.Errorf("subscriptions: swap wallet payment: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE orders SET status = 'confirmed', confirmed_at = now() WHERE id = $1`, orderID); err != nil {
		return fmt.Errorf("subscriptions: swap order: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE subscription_cycles SET status = 'paid', paid_at = now() WHERE id = $1`, cycleID); err != nil {
		return fmt.Errorf("subscriptions: swap cycle: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE subscriptions SET status = 'active', updated_at = now() WHERE id = $1`, subID); err != nil {
		return fmt.Errorf("subscriptions: swap activate: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO subscription_events
		    (subscription_id, event_type, from_status, to_status, actor_type, actor_id, metadata)
		VALUES ($1, 'activated', 'pending_payment', 'active', 'customer', $2,
		        jsonb_build_object('order_id', $3::uuid))`, subID, userID, orderID); err != nil {
		return fmt.Errorf("subscriptions: swap activation event: %w", err)
	}
	return nil
}

// markSwappable fills each week's swap window from the rotation's state.
func markSwappable(r *Rotation, now time.Time) {
	for i := range r.Weeks {
		w := &r.Weeks[i]
		loc, err := time.LoadLocation(w.tz)
		if err != nil {
			loc = time.UTC
		}
		start, err := time.ParseInLocation("2006-01-02", w.StartsOn, loc)
		if err != nil {
			continue
		}
		until := swapUntil(start).Format("2006-01-02")
		w.SwapUntil = &until
		status := ""
		if w.Status != nil {
			status = *w.Status
		}
		w.Swappable = !w.Swapped && swapOpen(r.Status, status, start, loc, now) == nil
	}
}

// VoidUnpaidSwaps clears swapped-in weeks whose dearer kitchen's difference
// was never paid once the swap cutoff for that week has passed in the
// kitchen's time zone. Until then the customer can still pay (repeating the
// swap retries the payment); after it, the new kitchen would have to shop for
// a week nobody confirmed. The replacement subscription is cancelled the way
// any unpaid one is, which voids its order and returns the wallet money
// applied to it, so the week's whole price is back in the wallet (the old
// week's share went there when the swap cancelled it). The rotation's total
// drops by the week, and the customer is told why the week is empty.
func (s *Service) VoidUnpaidSwaps(ctx context.Context, now time.Time, batch int) (int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT sw.id, sw.rotation_id, sw.rotation_week, sw.new_subscription_id, sw.new_amount_minor,
		       sw.currency, s.user_id, s.start_date, s.timezone, b.name, b.name_ar
		  FROM meal_rotation_swaps sw
		  JOIN subscriptions s ON s.id = sw.new_subscription_id
		  JOIN vendor_branches b ON b.id = s.branch_id
		 WHERE sw.voided_at IS NULL AND s.status = 'pending_payment'
		 ORDER BY s.start_date
		 LIMIT $1`, batch)
	if err != nil {
		return 0, fmt.Errorf("subscriptions: unpaid swaps: %w", err)
	}
	type cand struct {
		id, rotationID, subID, userID uuid.UUID
		week                          int
		amount                        int64
		currency, tz, name            string
		nameAr                        *string
		start                         time.Time
	}
	var cands []cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.id, &c.rotationID, &c.week, &c.subID, &c.amount, &c.currency,
			&c.userID, &c.start, &c.tz, &c.name, &c.nameAr); err != nil {
			rows.Close()
			return 0, fmt.Errorf("subscriptions: scan unpaid swap: %w", err)
		}
		cands = append(cands, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	voided := 0
	for _, c := range cands {
		loc, err := time.LoadLocation(c.tz)
		if err != nil {
			loc = time.UTC
		}
		start := time.Date(c.start.Year(), c.start.Month(), c.start.Day(), 0, 0, 0, 0, loc)
		if !dayIn(now, loc).After(swapUntil(start)) {
			continue
		}
		err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `
				UPDATE meal_rotation_swaps SET voided_at = now()
				 WHERE id = $1 AND voided_at IS NULL`, c.id)
			if err != nil {
				return fmt.Errorf("subscriptions: void swap: %w", err)
			}
			if tag.RowsAffected() == 0 {
				return nil
			}
			if err := s.cancelInTx(ctx, tx, c.subID, uuid.NullUUID{}, "system", uuid.Nil,
				"kitchen change not paid for", ""); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				UPDATE meal_rotations SET amount_minor = greatest(amount_minor - $2, 0), updated_at = now()
				 WHERE id = $1`, c.rotationID, c.amount); err != nil {
				return fmt.Errorf("subscriptions: swap total: %w", err)
			}
			nameAr := c.name
			if c.nameAr != nil && *c.nameAr != "" {
				nameAr = *c.nameAr
			}
			return s.notifyOn(ctx, tx, c.userID, "meals.swap_unpaid", []string{"push"}, map[string]string{
				"week": strconv.Itoa(c.week), "kitchen": c.name, "kitchen_ar": nameAr,
				"amount": formatAmount(c.amount, c.currency),
				"link":   "/en/rotations/" + c.rotationID.String(),
			}, "meals-swap-unpaid:"+c.id.String())
		})
		if err != nil {
			slog.ErrorContext(ctx, "void unpaid swap failed", "swap_id", c.id, "error", err)
			continue
		}
		voided++
	}
	return voided, nil
}
