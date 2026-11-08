package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/ledger"
	"github.com/marketplace/api/internal/modules/payments"
)

// walletStub wraps the real payments.Service so the test exercises the
// off-gateway confirm path (and its payments_amount_positive constraint) while
// still recording which orders were confirmed.
type walletStub struct {
	real   *payments.Service
	orders []uuid.UUID
}

func (w *walletStub) AuthorizeFromWallet(ctx context.Context, userID, orderID uuid.UUID,
	charge func(ctx context.Context, tx pgx.Tx, amountMinor int64, currency string) error) error {
	w.orders = append(w.orders, orderID)
	return w.real.AuthorizeFromWallet(ctx, userID, orderID, charge)
}

// guestFixture is a subFixture whose subscription is three weeks into a
// 4-week paid cycle with two weeks of completed deliveries, plus two other
// kitchens nearby: one priced like the customer's plan, one far dearer.
type guestFixture struct {
	subFixture
	lon, lat    float64
	wallet      *walletStub
	subID       uuid.UUID
	cycleID     uuid.UUID
	branchID    uuid.UUID
	guestPlan   uuid.UUID
	guestBranch uuid.UUID
	dearPlan    uuid.UUID
	now         time.Time
	// weekStart..weekEnd is the Sunday..Saturday after now: the guest week.
	weekStart, weekEnd string
}

// nextWednesdayNoon is the first Wednesday 12:00 Gulf time strictly after t.
func nextWednesdayNoon(t time.Time) time.Time {
	gst := time.FixedZone("GST", 4*3600)
	t = t.In(gst)
	d := time.Date(t.Year(), t.Month(), t.Day(), 12, 0, 0, 0, gst)
	for !d.After(t) || d.Weekday() != time.Wednesday {
		d = d.AddDate(0, 0, 1)
	}
	return d
}

func newGuestFixture(t *testing.T, cutoffHours int) guestFixture {
	t.Helper()
	f := guestFixture{subFixture: newSubFixture(t, cutoffHours)}
	f.wallet = &walletStub{real: payments.NewService(f.pool, payments.SandboxProvider{}, 1500, 500)}
	f.svc.wallet = f.wallet
	ctx := context.Background()

	// A spot of our own in the Gulf: the database is shared with other tests
	// whose kitchens would otherwise cover the address and crowd the offer.
	f.lon, f.lat = 51+rand.Float64()*4, 22+rand.Float64()*3
	for _, q := range []string{
		`UPDATE addresses SET point = ST_SetSRID(ST_MakePoint($2, $3), 4326)::geography WHERE id = $1`,
		`UPDATE vendor_branches SET point = ST_SetSRID(ST_MakePoint($2, $3), 4326)::geography,
		        service_area = ST_Multi(ST_Buffer(ST_SetSRID(ST_MakePoint($2, $3), 4326)::geography, 15000)::geometry)::geography
		  WHERE id = (SELECT branch_id FROM subscription_plans WHERE id = $1)`,
	} {
		id := f.addressID
		if strings.Contains(q, "vendor_branches") {
			id = f.planID
		}
		if _, err := f.pool.Exec(ctx, q, id, f.lon, f.lat); err != nil {
			t.Fatalf("relocate: %v", err)
		}
	}

	// The coming Wednesday noon in Dubai (never in the past, since Subscribe
	// checks the real clock): next week starts in four days.
	f.now = nextWednesdayNoon(time.Now())
	f.weekStart = f.now.AddDate(0, 0, 4).Format("2006-01-02")
	f.weekEnd = f.now.AddDate(0, 0, 10).Format("2006-01-02")
	start := f.now.AddDate(0, 0, -17) // Sunday three weeks ago
	cycleEnd := start.AddDate(0, 0, 27)

	// Far enough out to clear any plan cutoff against the real clock; the
	// fixture rewrites the dates below anyway.
	res, err := f.svc.Subscribe(ctx, f.input(f.now.AddDate(0, 0, 9)))
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	f.subID = res.ID
	if err := f.pool.QueryRow(ctx, `SELECT branch_id FROM subscriptions WHERE id = $1`, f.subID).Scan(&f.branchID); err != nil {
		t.Fatal(err)
	}
	// Rewrite the fresh subscription into a paid 4-week cycle, 28 days x 1
	// slot at 280 AED: 10 AED a meal.
	stmts := []struct {
		sql  string
		args []any
	}{
		{`UPDATE subscriptions SET status = 'active', start_date = $2 WHERE id = $1`, []any{f.subID, start}},
		{`UPDATE subscription_cycles SET status = 'paid', paid_at = now(), starts_on = $2, ends_on = $3,
		     amount_minor = 280000, deliverable_days = 28 WHERE subscription_id = $1`, []any{f.subID, start, cycleEnd}},
		{`UPDATE orders SET status = 'confirmed' WHERE source_subscription_id = $1`, []any{f.subID}},
	}
	for _, s := range stmts {
		if _, err := f.pool.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("fixture: %v\n%s", err, s.sql)
		}
	}
	if err := f.pool.QueryRow(ctx, `SELECT id FROM subscription_cycles WHERE subscription_id = $1`, f.subID).Scan(&f.cycleID); err != nil {
		t.Fatal(err)
	}
	// Two weeks of completed deliveries, then this week's scheduled ones and
	// next week's, exactly as the materialiser would have left them.
	for d := 0; d < 28; d++ {
		day := start.AddDate(0, 0, d)
		status := "scheduled"
		if d < 14 {
			status = "completed"
		}
		if _, err := f.pool.Exec(ctx, `
			INSERT INTO fulfillments (subscription_id, branch_id, kind, status, sequence, scheduled_date,
			    window_id, address_id, address_line, point, payload_ref, completed_at)
			VALUES ($1, $2, 'delivery', $3::fulfillment_status, 1, $4, $5, $6, 'Home',
			    ST_SetSRID(ST_MakePoint(55.23, 25.14), 4326)::geography, 'lunch',
			    CASE WHEN $3::text = 'completed' THEN $4::date + time '13:00' END)`,
			f.subID, f.branchID, status, day, f.windowID, f.addressID); err != nil {
			t.Fatalf("fulfilment: %v", err)
		}
	}

	f.guestPlan, f.guestBranch = f.addKitchen(t, "Guest", 70000)
	f.dearPlan, _ = f.addKitchen(t, "Dear", 140000)
	return f
}

func (f *guestFixture) addKitchen(t *testing.T, name string, weekPrice int64) (planID, branchID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	vendorID, windowID := uuid.New(), uuid.New()
	planID, branchID = uuid.New(), uuid.New()
	stmts := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO vendors (id, slug, legal_name, trade_name) VALUES ($1, $2, 'G LLC', $3)`,
			[]any{vendorID, fmt.Sprintf("guest-%s-%s", name, uuid.New().String()[:8]), name}},
		{`INSERT INTO vendor_branches (id, vendor_id, name, point, service_area) VALUES ($1, $2, $3,
			ST_SetSRID(ST_MakePoint($4, $5), 4326)::geography,
			ST_Multi(ST_Buffer(ST_SetSRID(ST_MakePoint($4, $5), 4326)::geography, 15000)::geometry)::geography)`,
			[]any{branchID, vendorID, name, f.lon, f.lat}},
		{`INSERT INTO time_windows (id, branch_id, label, starts_at, ends_at) VALUES ($1, $2, 'Lunch', '12:00', '14:00')`,
			[]any{windowID, branchID}},
		{`INSERT INTO subscription_plans (id, branch_id, service_type, name, cycle_days, days_per_week,
			cutoff_hours, price_minor, delivery_weekdays)
			VALUES ($1, $2, 'meals_subscription', $3, 7, 7, 12, $4, '{0,1,2,3,4,5,6}')`,
			[]any{planID, branchID, name + " plan", weekPrice}},
	}
	for _, s := range stmts {
		if _, err := f.pool.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("kitchen %s: %v\n%s", name, err, s.sql)
		}
	}
	return planID, branchID
}

func (f *guestFixture) walletBalance(t *testing.T) int64 {
	t.Helper()
	ctx := context.Background()
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	uid := f.userID
	bal, err := ledger.Balance(ctx, tx, ledger.AccountRef{
		Type: ledger.CustomerWallet, OwnerType: ledger.OwnerUser, OwnerID: &uid, Currency: "AED"})
	if err != nil {
		t.Fatal(err)
	}
	return bal
}

func TestGuestWeekOfferAndStart(t *testing.T) {
	f := newGuestFixture(t, 12)
	ctx := context.Background()

	offer, err := f.svc.GuestWeekOffer(ctx, f.userID, f.subID, f.now)
	if err != nil {
		t.Fatalf("offer: %v", err)
	}
	if !offer.Eligible {
		t.Fatalf("expected an eligible offer, got reason %q", offer.Reason)
	}
	if offer.WeekStart != f.weekStart || offer.WeekEnd != f.weekEnd {
		t.Fatalf("week = %s..%s, want %s..%s", offer.WeekStart, offer.WeekEnd, f.weekStart, f.weekEnd)
	}
	if offer.DeliveryDays != 7 || offer.Meals != 7 {
		t.Fatalf("days/meals = %d/%d, want 7/7", offer.DeliveryDays, offer.Meals)
	}
	// 280 AED over 28 days, one week paused, nothing recognised: 70 AED credit.
	if offer.CreditMinor != 70000 || offer.CurrentPerMealMinor != 10000 {
		t.Fatalf("credit %d per-meal %d, want 70000 / 10000", offer.CreditMinor, offer.CurrentPerMealMinor)
	}
	// Other tests leave kitchens behind in a shared database, so look only at
	// this fixture's: the guest kitchen is in, the dear one (outside the band)
	// and the customer's own kitchen are out.
	var opt GuestWeekOption
	for _, o := range offer.Options {
		switch o.PlanID {
		case f.guestPlan:
			opt = o
		case f.dearPlan, f.planID:
			t.Fatalf("option %s (%s) must not be offered", o.PlanName, o.PlanID)
		}
	}
	if opt.PlanID != f.guestPlan || len(offer.Options) != 1 {
		t.Fatalf("options = %+v, want only the guest kitchen", offer.Options)
	}
	if opt.WeekMinor != 70000 || opt.WalletMinor != 70000 || opt.DueMinor != 0 || opt.LeftoverMinor != 0 {
		t.Fatalf("option money = %+v", opt)
	}

	res, err := f.svc.StartGuestWeek(ctx, f.userID, f.subID, f.guestPlan, f.now)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if res.Status != "active" || res.DueMinor != 0 || res.WalletMinor != 70000 || res.PendingOrderID != nil {
		t.Fatalf("result = %+v", res)
	}
	if len(f.wallet.orders) != 1 {
		t.Fatalf("wallet confirmations = %d, want 1", len(f.wallet.orders))
	}

	// The credit went into the wallet and straight back out: net zero.
	if bal := f.walletBalance(t); bal != 0 {
		t.Fatalf("wallet balance = %d, want 0", bal)
	}
	var journals int
	if err := f.pool.QueryRow(ctx, `
		SELECT count(*) FROM ledger_journals
		 WHERE event_type IN ('subscription_credited', 'subscription_paid_from_wallet')
		   AND reference_id IN ($1, (SELECT id FROM subscription_cycles WHERE subscription_id = $2))`,
		f.cycleID, res.GuestSubscriptionID).Scan(&journals); err != nil {
		t.Fatal(err)
	}
	if journals != 2 {
		t.Fatalf("ledger journals = %d, want credit + wallet payment", journals)
	}

	// Original: next week's deliveries cancelled, nothing else touched, and the
	// pause allowance untouched (this is not a holiday).
	var cancelled, scheduled, completed, pauseUsed int
	if err := f.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE status = 'cancelled'),
		       count(*) FILTER (WHERE status = 'scheduled'),
		       count(*) FILTER (WHERE status = 'completed'),
		       (SELECT pause_days_used FROM subscriptions WHERE id = $1)
		  FROM fulfillments WHERE subscription_id = $1`, f.subID).Scan(&cancelled, &scheduled, &completed, &pauseUsed); err != nil {
		t.Fatal(err)
	}
	if cancelled != 7 || completed != 14 || scheduled != 7 || pauseUsed != 0 {
		t.Fatalf("fulfilments cancelled/scheduled/completed = %d/%d/%d, pause used %d", cancelled, scheduled, completed, pauseUsed)
	}
	var origEnd, guestStart, guestEnd time.Time
	var guestStatus, guestBranch string
	var guestAmount int64
	if err := f.pool.QueryRow(ctx, `
		SELECT o.end_date IS NULL, g.start_date, g.end_date, g.status, g.branch_id::text, c.amount_minor
		  FROM subscriptions o, subscriptions g
		  JOIN subscription_cycles c ON c.subscription_id = g.id
		 WHERE o.id = $1 AND g.id = $2`, f.subID, res.GuestSubscriptionID).
		Scan(new(bool), &guestStart, &guestEnd, &guestStatus, &guestBranch, &guestAmount); err != nil {
		t.Fatal(err)
	}
	_ = origEnd
	if guestStart.Format("2006-01-02") != f.weekStart || guestEnd.Format("2006-01-02") != f.weekEnd {
		t.Fatalf("guest dates %s..%s", guestStart, guestEnd)
	}
	if guestStatus != "active" || guestBranch != f.guestBranch.String() || guestAmount != 70000 {
		t.Fatalf("guest status %s branch %s amount %d", guestStatus, guestBranch, guestAmount)
	}

	// Recognition pays the guest kitchen, not the original, for that week:
	// the original has no completed delivery in the guest week to recognise.
	var origThatWeek int
	if err := f.pool.QueryRow(ctx, `
		SELECT count(*) FROM fulfillments WHERE subscription_id = $1
		   AND scheduled_date BETWEEN $2::date AND $3::date AND status <> 'cancelled'`, f.subID, f.weekStart, f.weekEnd).Scan(&origThatWeek); err != nil {
		t.Fatal(err)
	}
	if origThatWeek != 0 {
		t.Fatalf("original kitchen still has %d live deliveries in the guest week", origThatWeek)
	}

	// Once per cycle: a second attempt is a conflict and the offer says so.
	_, err = f.svc.StartGuestWeek(ctx, f.userID, f.subID, f.guestPlan, f.now)
	var coded *httpx.CodedError
	if !errors.As(err, &coded) || coded.Code != "guest_week_exists" {
		t.Fatalf("second start: %v", err)
	}
	offer, err = f.svc.GuestWeekOffer(ctx, f.userID, f.subID, f.now)
	if err != nil || offer.Eligible || offer.Reason != "already_used" {
		t.Fatalf("offer after start = %+v, %v", offer, err)
	}

	// My meals shows both halves with the other kitchen's name.
	mine, err := f.svc.Mine(ctx, f.userID)
	if err != nil {
		t.Fatal(err)
	}
	var sawOrig, sawGuest bool
	for _, m := range mine {
		switch m.ID {
		case f.subID:
			sawOrig = m.GuestWeek != nil && m.GuestWeek.KitchenName == "Guest" && m.GuestWeek.GuestSubscriptionID == res.GuestSubscriptionID
		case res.GuestSubscriptionID:
			sawGuest = m.GuestOf != nil && m.GuestOf.KitchenName == "T" && m.GuestOf.SubscriptionID == f.subID
		}
	}
	if !sawOrig || !sawGuest {
		t.Fatalf("guest week links missing: orig %v guest %v", sawOrig, sawGuest)
	}

	// The guest week's pause was credited to the wallet, so the original's
	// detail does not offer to resume it.
	detail, err := f.svc.MyDetail(ctx, f.userID, f.subID, f.now)
	if err != nil {
		t.Fatal(err)
	}
	var guestPause *MyPause
	for i := range detail.Pauses {
		if detail.Pauses[i].From == f.weekStart {
			guestPause = &detail.Pauses[i]
		}
	}
	if guestPause == nil || guestPause.CanResume {
		t.Fatalf("guest-week pause in detail = %+v, want one that cannot be resumed", guestPause)
	}
}

func TestGuestWeekChargesTheDifferenceWhenTheGuestIsDearer(t *testing.T) {
	f := newGuestFixture(t, 12)
	ctx := context.Background()
	// 80.5 AED a week: 11.5 a meal, exactly +15%.
	dearerPlan, _ := f.addKitchen(t, "Dearer", 80500)

	res, err := f.svc.StartGuestWeek(ctx, f.userID, f.subID, dearerPlan, f.now)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if res.Status != "pending_payment" || res.PendingOrderID == nil {
		t.Fatalf("expected a pending order for the difference, got %+v", res)
	}
	if res.WalletMinor != 70000 || res.DueMinor != 10500 || res.LeftoverMinor != 0 {
		t.Fatalf("split = wallet %d due %d leftover %d", res.WalletMinor, res.DueMinor, res.LeftoverMinor)
	}
	var authorized, total int64
	if err := f.pool.QueryRow(ctx, `SELECT authorized_minor, total_minor FROM price_quotes WHERE order_id = $1`,
		*res.PendingOrderID).Scan(&authorized, &total); err != nil {
		t.Fatal(err)
	}
	if authorized != 10500 || total != 80500 {
		t.Fatalf("quote authorizes %d of %d; want the card to pay 10500 of 80500", authorized, total)
	}
	if len(f.wallet.orders) != 0 {
		t.Fatal("a guest week with a card balance must not be confirmed off-gateway")
	}
	if bal := f.walletBalance(t); bal != 0 {
		t.Fatalf("wallet balance = %d, want 0 (all credit spent on the guest week)", bal)
	}
}

func TestGuestWeekLeavesTheDifferenceInTheWalletWhenTheGuestIsCheaper(t *testing.T) {
	f := newGuestFixture(t, 12)
	ctx := context.Background()
	cheaperPlan, _ := f.addKitchen(t, "Cheaper", 63000) // 9 AED a meal, -10%

	res, err := f.svc.StartGuestWeek(ctx, f.userID, f.subID, cheaperPlan, f.now)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if res.Status != "active" || res.WalletMinor != 63000 || res.DueMinor != 0 || res.LeftoverMinor != 7000 {
		t.Fatalf("result = %+v", res)
	}
	// Ledger balances are debit-positive, so wallet credit shows negative.
	if bal := f.walletBalance(t); bal != -7000 {
		t.Fatalf("wallet balance = %d, want -7000 (7 AED of credit kept)", bal)
	}
}

func TestGuestWeekEligibilityGates(t *testing.T) {
	ctx := context.Background()

	t.Run("cutoff", func(t *testing.T) {
		// Cutoff of 5 days: on Wednesday, next Sunday is too close.
		f := newGuestFixture(t, 5*24)
		offer, err := f.svc.GuestWeekOffer(ctx, f.userID, f.subID, f.now)
		if err != nil || offer.Eligible || offer.Reason != "past_cutoff" {
			t.Fatalf("offer = %+v, %v; want past_cutoff", offer, err)
		}
		_, err = f.svc.StartGuestWeek(ctx, f.userID, f.subID, f.guestPlan, f.now)
		if fieldCode(err, "week_start") != "past_cutoff" {
			t.Fatalf("start past cutoff: %v", err)
		}
	})

	t.Run("too few delivered weeks", func(t *testing.T) {
		f := newGuestFixture(t, 12)
		if _, err := f.pool.Exec(ctx, `
			UPDATE fulfillments SET status = 'scheduled', completed_at = NULL
			 WHERE subscription_id = $1 AND scheduled_date < $2`, f.subID, f.now.AddDate(0, 0, -10)); err != nil {
			t.Fatal(err)
		}
		offer, err := f.svc.GuestWeekOffer(ctx, f.userID, f.subID, f.now)
		if err != nil || offer.Eligible || offer.Reason != "too_few_weeks" {
			t.Fatalf("offer = %+v, %v; want too_few_weeks", offer, err)
		}
	})

	t.Run("dismissed", func(t *testing.T) {
		f := newGuestFixture(t, 12)
		if err := f.svc.DismissGuestWeek(ctx, f.userID, f.subID); err != nil {
			t.Fatal(err)
		}
		if err := f.svc.DismissGuestWeek(ctx, f.userID, f.subID); err != nil {
			t.Fatalf("dismiss must be idempotent: %v", err)
		}
		offer, err := f.svc.GuestWeekOffer(ctx, f.userID, f.subID, f.now)
		if err != nil || offer.Eligible || offer.Reason != "dismissed" {
			t.Fatalf("offer = %+v, %v; want dismissed", offer, err)
		}
	})

	t.Run("not an option", func(t *testing.T) {
		f := newGuestFixture(t, 12)
		_, err := f.svc.StartGuestWeek(ctx, f.userID, f.subID, f.dearPlan, f.now)
		if fieldCode(err, "plan_id") != "not_eligible" {
			t.Fatalf("dear plan: %v", err)
		}
		_, err = f.svc.StartGuestWeek(ctx, f.userID, f.subID, f.planID, f.now)
		if fieldCode(err, "plan_id") != "not_eligible" {
			t.Fatalf("own plan: %v", err)
		}
	})

	t.Run("someone else's subscription", func(t *testing.T) {
		f := newGuestFixture(t, 12)
		_, err := f.svc.GuestWeekOffer(ctx, uuid.New(), f.subID, f.now)
		if !errors.Is(err, httpx.ErrNotFound) {
			t.Fatalf("want 404, got %v", err)
		}
	})
}

func TestCancelPersistsReasonCode(t *testing.T) {
	f := newGuestFixture(t, 12)
	ctx := context.Background()

	if err := f.svc.CancelWithCode(ctx, f.userID, f.subID, "hangry", ""); err == nil {
		t.Fatal("unknown code must be rejected")
	}
	if err := f.svc.CancelWithCode(ctx, f.userID, f.subID, "bored_of_food", "same dal every day"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	var status string
	var code, note *string
	if err := f.pool.QueryRow(ctx, `
		SELECT status, cancellation_code, cancellation_reason FROM subscriptions WHERE id = $1`, f.subID).
		Scan(&status, &code, &note); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" || code == nil || *code != "bored_of_food" || note == nil || *note != "same dal every day" {
		t.Fatalf("status %s code %v note %v", status, code, note)
	}
	reasons, err := f.svc.CancellationReasons(ctx, 30)
	if err != nil {
		t.Fatal(err)
	}
	var bored int
	for _, r := range reasons {
		if r.Code == "bored_of_food" {
			bored = r.Subscriptions
		}
	}
	if bored < 1 {
		t.Fatalf("breakdown = %+v, want bored_of_food counted", reasons)
	}
}

func TestCancellingAnUnpaidGuestWeekGivesTheOriginalWeekBack(t *testing.T) {
	f := newGuestFixture(t, 12)
	ctx := context.Background()
	dearerPlan, _ := f.addKitchen(t, "Dearer", 80500)

	res, err := f.svc.StartGuestWeek(ctx, f.userID, f.subID, dearerPlan, f.now)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if res.Status != "pending_payment" {
		t.Fatalf("expected a pending guest week, got %+v", res)
	}
	// Wallet: +70000 credit, -70000 applied to the guest week.
	if bal := f.walletBalance(t); bal != 0 {
		t.Fatalf("wallet before cancel = %d, want 0", bal)
	}

	// The customer never pays the difference and drops the guest week.
	if err := f.svc.Cancel(ctx, f.userID, res.GuestSubscriptionID, "changed my mind"); err != nil {
		t.Fatalf("cancel guest: %v", err)
	}

	var pauseState string
	if err := f.pool.QueryRow(ctx, `
		SELECT p.state FROM subscription_pauses p
		  JOIN subscription_guest_weeks g ON g.pause_id = p.id
		 WHERE g.guest_subscription_id = $1`, res.GuestSubscriptionID).Scan(&pauseState); err != nil {
		t.Fatal(err)
	}
	if pauseState != "cancelled" {
		t.Fatalf("original pause state = %s, want cancelled (still before cutoff)", pauseState)
	}
	var restored int
	if err := f.pool.QueryRow(ctx, `
		SELECT count(*) FROM fulfillments WHERE subscription_id = $1
		   AND scheduled_date BETWEEN $2::date AND $3::date AND status = 'scheduled'`,
		f.subID, f.weekStart, f.weekEnd).Scan(&restored); err != nil {
		t.Fatal(err)
	}
	if restored != 7 {
		t.Fatalf("restored deliveries = %d, want 7", restored)
	}
	// Guest share returned (+70000), pause credit reversed (-70000): the
	// customer is exactly where they started.
	if bal := f.walletBalance(t); bal != 0 {
		t.Fatalf("wallet after cancel = %d, want 0", bal)
	}
	var guestCycle, guestOrder string
	if err := f.pool.QueryRow(ctx, `
		SELECT c.status, o.status FROM subscription_cycles c JOIN orders o ON o.id = c.order_id
		 WHERE c.subscription_id = $1`, res.GuestSubscriptionID).Scan(&guestCycle, &guestOrder); err != nil {
		t.Fatal(err)
	}
	if guestCycle != "cancelled" || guestOrder != "cancelled" {
		t.Fatalf("guest cycle/order = %s/%s, want cancelled", guestCycle, guestOrder)
	}
}

func TestCancelAfterGuestWeekDoesNotCreditThePausedWeekTwice(t *testing.T) {
	f := newGuestFixture(t, 12)
	ctx := context.Background()

	res, err := f.svc.StartGuestWeek(ctx, f.userID, f.subID, f.guestPlan, f.now)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if res.Status != "active" {
		t.Fatalf("result = %+v", res)
	}
	// 70000 credited by the pause, 70000 spent on the guest week.
	if bal := f.walletBalance(t); bal != 0 {
		t.Fatalf("wallet = %d, want 0", bal)
	}

	if err := f.svc.Cancel(ctx, f.userID, f.subID, "moving"); err != nil {
		t.Fatalf("cancel original: %v", err)
	}
	// 280000 paid, nothing recognised, 70000 already credited for the paused
	// week: 210000 comes back, not 280000.
	if bal := f.walletBalance(t); bal != -210000 {
		t.Fatalf("wallet after cancel = %d, want -210000 (credit of 210 AED)", bal)
	}
}

func TestGuestWeekHonoursTheGuestKitchensOwnCutoff(t *testing.T) {
	f := newGuestFixture(t, 12)
	ctx := context.Background()
	// Same price as the guest kitchen, but it needs five days' notice: on
	// Wednesday, Sunday is too close for it.
	slowPlan, _ := f.addKitchen(t, "Slow", 70000)
	if _, err := f.pool.Exec(ctx, `UPDATE subscription_plans SET cutoff_hours = 120 WHERE id = $1`, slowPlan); err != nil {
		t.Fatal(err)
	}
	offer, err := f.svc.GuestWeekOffer(ctx, f.userID, f.subID, f.now)
	if err != nil || !offer.Eligible {
		t.Fatalf("offer = %+v, %v", offer, err)
	}
	for _, o := range offer.Options {
		if o.PlanID == slowPlan {
			t.Fatal("a kitchen past its own cutoff must not be offered")
		}
	}
	_, err = f.svc.StartGuestWeek(ctx, f.userID, f.subID, slowPlan, f.now)
	if fieldCode(err, "plan_id") != "not_eligible" {
		t.Fatalf("start with slow kitchen: %v", err)
	}
}

// A paused week carries one credit. A guest week over a wallet pause, or a
// pause over a guest week, would credit the same days twice.
func TestGuestWeekAndPauseNeverCreditTheSameWeekTwice(t *testing.T) {
	ctx := context.Background()
	weekOf := func(f guestFixture) (time.Time, time.Time) {
		from, _ := time.Parse("2006-01-02", f.weekStart)
		to, _ := time.Parse("2006-01-02", f.weekEnd)
		return from, to
	}
	allowPauses := func(t *testing.T, f guestFixture) {
		if _, err := f.pool.Exec(ctx, `UPDATE subscription_plans SET allowed_pause_days = 14 WHERE id = $1`, f.planID); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("pause first, then guest week", func(t *testing.T) {
		f := newGuestFixture(t, 12)
		allowPauses(t, f)
		from, to := weekOf(f)
		if err := f.svc.Pause(ctx, PauseInput{UserID: f.userID, SubscriptionID: f.subID,
			From: from, To: to, CreditMode: "wallet"}); err != nil {
			t.Fatalf("pause: %v", err)
		}
		offer, err := f.svc.GuestWeekOffer(ctx, f.userID, f.subID, f.now)
		if err != nil || offer.Eligible || offer.Reason != "already_paused" {
			t.Fatalf("offer = %+v, %v; want already_paused", offer, err)
		}
		_, err = f.svc.StartGuestWeek(ctx, f.userID, f.subID, f.guestPlan, f.now)
		if codeOf(err) != "guest_week_already_paused" {
			t.Fatalf("start over a pause: %v", err)
		}
		// Ledger balances are debit-positive: one pause credit, not two.
		if bal := f.walletBalance(t); bal != -70000 {
			t.Fatalf("wallet = %d, want the single pause credit -70000", bal)
		}
	})

	t.Run("guest week first, then pause", func(t *testing.T) {
		f := newGuestFixture(t, 12)
		allowPauses(t, f)
		from, to := weekOf(f)
		if _, err := f.svc.StartGuestWeek(ctx, f.userID, f.subID, f.guestPlan, f.now); err != nil {
			t.Fatalf("start: %v", err)
		}
		err := f.svc.Pause(ctx, PauseInput{UserID: f.userID, SubscriptionID: f.subID,
			From: from, To: to, CreditMode: "wallet"})
		if fieldCode(err, "from_date") != "overlaps_pause" {
			t.Fatalf("pause over a guest week: %v", err)
		}
		// Overlapping by a single day is still an overlap.
		err = f.svc.Pause(ctx, PauseInput{UserID: f.userID, SubscriptionID: f.subID,
			From: to, To: to.AddDate(0, 0, 3), CreditMode: "wallet"})
		if fieldCode(err, "from_date") != "overlaps_pause" {
			t.Fatalf("pause touching a guest week: %v", err)
		}
		if bal := f.walletBalance(t); bal != 0 {
			t.Fatalf("wallet = %d, want 0 (the credit was spent on the guest week)", bal)
		}
		// The database refuses the overlap even if the service check is bypassed.
		_, err = f.pool.Exec(ctx, `
			INSERT INTO subscription_pauses (subscription_id, from_date, to_date, credit_mode)
			VALUES ($1, $2, $3, 'extend')`, f.subID, from, to)
		if err == nil {
			t.Fatal("exclusion constraint let an overlapping pause in")
		}
	})
}
