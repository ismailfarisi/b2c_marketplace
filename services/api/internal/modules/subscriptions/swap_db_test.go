package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/payments"
	"github.com/marketplace/api/internal/seam/servicetype"
	"github.com/marketplace/api/internal/verticals/meals"
)

// A paid, running rotation of four kitchens plus two more kitchens on the
// rotation to swap to: one dearer, one cheaper than every booked week.
type swapFixture struct {
	svc       *Service
	pool      *pgxpool.Pool
	userID    uuid.UUID
	addressID uuid.UUID
	branches  []uuid.UUID
	plans     []uuid.UUID
	prices    []int64
	rotation  Rotation
}

const (
	swapDearer  = 4
	swapCheaper = 5
)

func newSwapFixture(t *testing.T, startIn int) swapFixture {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	reg := servicetype.NewRegistry()
	meals.Register(reg)

	f := swapFixture{svc: NewService(pool, reg, Config{}), pool: pool, userID: uuid.New(), addressID: uuid.New(),
		prices: []int64{10000, 12000, 14000, 16000, 25000, 5000}}
	n := rand.IntN(10_000_000)
	exec := func(sql string, args ...any) {
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture: %v\n%s", err, sql)
		}
	}
	exec(`INSERT INTO users (id, phone) VALUES ($1, $2)`, f.userID, fmt.Sprintf("+97158%07d", n))
	exec(`INSERT INTO addresses (id, user_id, label, city, point) VALUES ($1, $2, 'Home', 'Dubai',
		ST_SetSRID(ST_MakePoint(55.23, 25.14), 4326)::geography)`, f.addressID, f.userID)
	for i, price := range f.prices {
		vendorID, branchID, planID := uuid.New(), uuid.New(), uuid.New()
		exec(`INSERT INTO vendors (id, slug, legal_name, trade_name, status) VALUES ($1, $2, 'T LLC', $3, 'active')`,
			vendorID, fmt.Sprintf("swaptest-%d-%d", n, i), fmt.Sprintf("Kitchen %d", i))
		exec(`INSERT INTO vendor_branches (id, vendor_id, name, point, service_area, rating, rating_count)
			VALUES ($1, $2, $3, ST_SetSRID(ST_MakePoint(55.23, 25.14), 4326)::geography,
			ST_Multi(ST_Buffer(ST_SetSRID(ST_MakePoint(55.23, 25.14), 4326)::geography, 15000)::geometry)::geography,
			$4, $5)`, branchID, vendorID, fmt.Sprintf("Swap kitchen %d-%d", n, i), ratingFor(i), 10*i)
		exec(`INSERT INTO time_windows (id, branch_id, label, starts_at, ends_at) VALUES ($1, $2, 'Lunch', '12:00', '14:00')`,
			uuid.New(), branchID)
		exec(`INSERT INTO subscription_plans (id, branch_id, service_type, name, cycle_days, days_per_week,
			cutoff_hours, price_minor, delivery_weekdays, meal_slots, rotation_enabled, rotation_week_price_minor)
			VALUES ($1, $2, 'meals_subscription', 'Rotation week', 7, 5, 12, $3, '{0,1,2,3,4}', '{lunch}', true, $3)`,
			planID, branchID, price)
		f.branches = append(f.branches, branchID)
		f.plans = append(f.plans, planID)
	}

	loc, _ := time.LoadLocation("Asia/Dubai")
	start := dayIn(time.Now(), loc).AddDate(0, 0, startIn)
	r, err := f.svc.CreateRotation(ctx, RotationInput{
		UserID: f.userID, AddressID: f.addressID, PlanIDs: f.plans[:RotationKitchens],
		StartDate: start, Weekdays: []int{0, 1, 2, 3, 4}, Slots: []string{"lunch"},
	})
	if err != nil {
		t.Fatalf("create rotation: %v", err)
	}
	// Paid, the way payments.AuthorizeRotation leaves it.
	exec(`UPDATE subscription_cycles SET status = 'paid', paid_at = now()
		 WHERE subscription_id IN (SELECT id FROM subscriptions WHERE rotation_id = $1)`, r.ID)
	exec(`UPDATE orders SET status = 'confirmed', confirmed_at = now()
		 WHERE id IN (SELECT order_id FROM subscription_cycles c JOIN subscriptions s ON s.id = c.subscription_id
		              WHERE s.rotation_id = $1)`, r.ID)
	exec(`UPDATE subscriptions SET status = 'active' WHERE rotation_id = $1`, r.ID)
	exec(`UPDATE meal_rotations SET status = 'active' WHERE id = $1`, r.ID)
	f.rotation, err = f.svc.MyRotation(ctx, f.userID, r.ID)
	if err != nil {
		t.Fatalf("load rotation: %v", err)
	}
	return f
}

// The cheaper kitchen is the only rated one, so "rated first" puts it ahead
// of the dearer one.
func ratingFor(i int) *float64 {
	if i == swapCheaper {
		v := 4.6
		return &v
	}
	return nil
}

func (f swapFixture) week(t *testing.T, n int) RotationWeek {
	t.Helper()
	r, err := f.svc.MyRotation(context.Background(), f.userID, f.rotation.ID)
	if err != nil {
		t.Fatalf("load rotation: %v", err)
	}
	for _, w := range r.Weeks {
		if w.Week == n {
			return w
		}
	}
	t.Fatalf("week %d missing", n)
	return RotationWeek{}
}

func (f swapFixture) total(t *testing.T) int64 {
	t.Helper()
	r, err := f.svc.MyRotation(context.Background(), f.userID, f.rotation.ID)
	if err != nil {
		t.Fatalf("load rotation: %v", err)
	}
	return r.AmountMinor
}

// wallet is the customer's wallet balance: credits less debits.
func (f swapFixture) wallet(t *testing.T) int64 {
	t.Helper()
	var bal int64
	if err := f.pool.QueryRow(context.Background(), `
		SELECT COALESCE(sum(CASE e.direction WHEN 'credit' THEN e.amount_minor ELSE -e.amount_minor END), 0)
		  FROM ledger_entries e JOIN ledger_accounts a ON a.id = e.account_id
		 WHERE a.type = 'customer_wallet' AND a.owner_id = $1`, f.userID).Scan(&bal); err != nil {
		t.Fatalf("wallet: %v", err)
	}
	return bal
}

func (f swapFixture) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v\n%s", err, sql)
	}
	return n
}

func (f swapFixture) subStatus(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var s string
	if err := f.pool.QueryRow(context.Background(), `SELECT status FROM subscriptions WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatalf("status: %v", err)
	}
	return s
}

func codeOf(err error) string {
	var c *httpx.CodedError
	if errors.As(err, &c) {
		return c.Code
	}
	return ""
}

func TestSwapWeekDearerKitchenOwesTheDifference(t *testing.T) {
	f := newSwapFixture(t, 7)
	ctx := context.Background()
	before := f.week(t, 3)
	total := f.total(t)

	res, err := f.svc.SwapRotationWeek(ctx, f.userID, f.rotation.ID, 3, f.plans[swapDearer])
	if err != nil {
		t.Fatalf("swap: %v", err)
	}
	want := f.prices[swapDearer] - before.PriceMinor
	if res.DueMinor != want {
		t.Fatalf("due %d, want %d", res.DueMinor, want)
	}
	after := f.week(t, 3)
	if after.BranchID != f.branches[swapDearer] || after.PriceMinor != f.prices[swapDearer] {
		t.Fatalf("week 3 is %s at %d, want the dearer kitchen at %d", after.BranchID, after.PriceMinor, f.prices[swapDearer])
	}
	if after.StartsOn != before.StartsOn || after.EndsOn != before.EndsOn {
		t.Fatalf("dates moved: %s–%s, want %s–%s", after.StartsOn, after.EndsOn, before.StartsOn, before.EndsOn)
	}
	if !after.Swapped || after.Swappable {
		t.Fatalf("week should be swapped once and not again: swapped=%v swappable=%v", after.Swapped, after.Swappable)
	}
	if got := f.total(t); got != total+want {
		t.Fatalf("rotation total %d, want %d", got, total+want)
	}
	if s := f.subStatus(t, *before.SubscriptionID); s != "cancelled" {
		t.Fatalf("old week %s, want cancelled", s)
	}
	// Awaiting the difference: nothing is active until it is paid.
	if s := f.subStatus(t, *after.SubscriptionID); s != "pending_payment" {
		t.Fatalf("new week %s, want pending_payment", s)
	}
	var authorized int64
	if err := f.pool.QueryRow(ctx, `SELECT authorized_minor FROM price_quotes WHERE order_id = $1`, res.OrderID).Scan(&authorized); err != nil {
		t.Fatalf("quote: %v", err)
	}
	if authorized != want {
		t.Fatalf("order authorizes %d, want %d", authorized, want)
	}
	// The old week's credit went entirely to the new week.
	if bal := f.wallet(t); bal != 0 {
		t.Fatalf("wallet %d, want 0", bal)
	}
	if n := f.count(t, `SELECT count(*) FROM ledger_entries e JOIN ledger_accounts a ON a.id = e.account_id
		WHERE a.type = 'vendor_payable'`); n != 0 {
		t.Fatalf("%d vendor payable entries, want none before any meal", n)
	}
}

func TestSwapWeekCheaperKitchenCreditsTheDifference(t *testing.T) {
	f := newSwapFixture(t, 7)
	ctx := context.Background()
	before := f.week(t, 2)
	total := f.total(t)

	res, err := f.svc.SwapRotationWeek(ctx, f.userID, f.rotation.ID, 2, f.plans[swapCheaper])
	if err != nil {
		t.Fatalf("swap: %v", err)
	}
	if res.DueMinor != 0 {
		t.Fatalf("due %d, want 0", res.DueMinor)
	}
	diff := before.PriceMinor - f.prices[swapCheaper]
	if bal := f.wallet(t); bal != diff {
		t.Fatalf("wallet %d, want %d", bal, diff)
	}
	if got := f.total(t); got != total-diff {
		t.Fatalf("rotation total %d, want %d", got, total-diff)
	}
	after := f.week(t, 2)
	if s := f.subStatus(t, *after.SubscriptionID); s != "active" {
		t.Fatalf("new week %s, want active", s)
	}
	if n := f.count(t, `SELECT count(*) FROM subscription_cycles WHERE subscription_id = $1 AND status = 'paid'`,
		*after.SubscriptionID); n != 1 {
		t.Fatalf("%d paid cycles on the new week, want 1", n)
	}
	if n := f.count(t, `SELECT count(*) FROM subscription_cycles WHERE subscription_id = $1 AND status = 'cancelled'`,
		*before.SubscriptionID); n != 1 {
		t.Fatalf("%d cancelled cycles on the old week, want 1", n)
	}
	// Deferred revenue holds exactly the new week's price for the swapped week:
	// the old price out on cancel, the applied credit back in.
	var deferred int64
	if err := f.pool.QueryRow(ctx, `
		SELECT COALESCE(sum(CASE e.direction WHEN 'credit' THEN e.amount_minor ELSE -e.amount_minor END), 0)
		  FROM ledger_entries e JOIN ledger_accounts a ON a.id = e.account_id
		  JOIN ledger_journals j ON j.id = e.journal_id
		 WHERE a.type = 'deferred_revenue' AND j.reference_id IN (
		       SELECT id FROM subscription_cycles WHERE subscription_id IN ($1, $2))`,
		*before.SubscriptionID, *after.SubscriptionID).Scan(&deferred); err != nil {
		t.Fatalf("deferred: %v", err)
	}
	if deferred != -diff {
		t.Fatalf("deferred revenue moved %d for the swap, want %d", deferred, -diff)
	}
}

func TestSwapWeekIsIdempotentAndOncePerWeek(t *testing.T) {
	f := newSwapFixture(t, 7)
	ctx := context.Background()
	first, err := f.svc.SwapRotationWeek(ctx, f.userID, f.rotation.ID, 4, f.plans[swapDearer])
	if err != nil {
		t.Fatalf("swap: %v", err)
	}
	total, wallet := f.total(t), f.wallet(t)
	entries := f.count(t, `SELECT count(*) FROM ledger_entries`)

	again, err := f.svc.SwapRotationWeek(ctx, f.userID, f.rotation.ID, 4, f.plans[swapDearer])
	if err != nil {
		t.Fatalf("repeat swap: %v", err)
	}
	if again.OrderID != first.OrderID || again.DueMinor != first.DueMinor {
		t.Fatalf("repeat answered %v/%d, want %v/%d", again.OrderID, again.DueMinor, first.OrderID, first.DueMinor)
	}
	if f.total(t) != total || f.wallet(t) != wallet || f.count(t, `SELECT count(*) FROM ledger_entries`) != entries {
		t.Fatal("a repeated swap moved money")
	}
	if n := f.count(t, `SELECT count(*) FROM meal_rotation_swaps WHERE rotation_id = $1`, f.rotation.ID); n != 1 {
		t.Fatalf("%d swap records, want 1", n)
	}
	if n := f.count(t, `SELECT count(*) FROM subscriptions WHERE rotation_id = $1 AND rotation_week = 4 AND status <> 'cancelled'`,
		f.rotation.ID); n != 1 {
		t.Fatalf("%d live subscriptions for week 4, want 1", n)
	}

	_, err = f.svc.SwapRotationWeek(ctx, f.userID, f.rotation.ID, 4, f.plans[swapCheaper])
	if codeOf(err) != "already_swapped" {
		t.Fatalf("second swap of the week: %v, want already_swapped", err)
	}
	_, err = f.svc.SwapOptions(ctx, f.userID, f.rotation.ID, 4)
	if codeOf(err) != "already_swapped" {
		t.Fatalf("options after swap: %v, want already_swapped", err)
	}
}

func TestSwapWeekRejectsKitchenAlreadyInRotation(t *testing.T) {
	f := newSwapFixture(t, 7)
	w1 := f.week(t, 1)
	_, err := f.svc.SwapRotationWeek(context.Background(), f.userID, f.rotation.ID, 3, w1.PlanID)
	if fieldCode(err, "plan_ids") != "same_kitchen_twice" {
		t.Fatalf("swap to week 1's kitchen: %v, want same_kitchen_twice", err)
	}
	w3 := f.week(t, 3)
	_, err = f.svc.SwapRotationWeek(context.Background(), f.userID, f.rotation.ID, 3, w3.PlanID)
	if fieldCode(err, "plan_id") != "same_kitchen" {
		t.Fatalf("swap to the same kitchen: %v, want same_kitchen", err)
	}
	if n := f.count(t, `SELECT count(*) FROM meal_rotation_swaps WHERE rotation_id = $1`, f.rotation.ID); n != 0 {
		t.Fatalf("%d swap records after refusals, want 0", n)
	}
}

func TestSwapWeekCutoff(t *testing.T) {
	// Week 1 starts in two days: inside the cutoff. Week 2 starts in nine.
	f := newSwapFixture(t, 2)
	ctx := context.Background()
	_, err := f.svc.SwapRotationWeek(ctx, f.userID, f.rotation.ID, 1, f.plans[swapDearer])
	if codeOf(err) != "swap_cutoff_passed" {
		t.Fatalf("swap inside cutoff: %v, want swap_cutoff_passed", err)
	}
	_, err = f.svc.SwapOptions(ctx, f.userID, f.rotation.ID, 1)
	if codeOf(err) != "swap_cutoff_passed" {
		t.Fatalf("options inside cutoff: %v, want swap_cutoff_passed", err)
	}
	if w := f.week(t, 1); w.Swappable {
		t.Fatal("week 1 marked swappable inside the cutoff")
	}
	if w := f.week(t, 2); !w.Swappable {
		t.Fatal("week 2 not marked swappable")
	}
	if _, err := f.svc.SwapRotationWeek(ctx, f.userID, f.rotation.ID, 2, f.plans[swapDearer]); err != nil {
		t.Fatalf("swap week 2: %v", err)
	}

	if _, err := f.pool.Exec(ctx, `UPDATE meal_rotations SET status = 'cancelled' WHERE id = $1`, f.rotation.ID); err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.SwapRotationWeek(ctx, f.userID, f.rotation.ID, 3, f.plans[swapCheaper])
	if codeOf(err) != "rotation_not_active" {
		t.Fatalf("swap on a cancelled rotation: %v, want rotation_not_active", err)
	}
}

func TestSwapOptionsListsEligibleKitchensRatedFirst(t *testing.T) {
	f := newSwapFixture(t, 7)
	opts, err := f.svc.SwapOptions(context.Background(), f.userID, f.rotation.ID, 3)
	if err != nil {
		t.Fatalf("options: %v", err)
	}
	w3 := f.week(t, 3)
	if opts.CurrentPriceMinor != w3.PriceMinor || opts.SwapUntil == "" {
		t.Fatalf("options header %+v", opts)
	}
	ours := []SwapOption{}
	for _, o := range opts.Items {
		for i, b := range f.branches {
			if o.BranchID == b {
				if i < RotationKitchens {
					t.Fatalf("kitchen %d is already in the rotation but was offered", i)
				}
				ours = append(ours, o)
			}
		}
	}
	if len(ours) != 2 || ours[0].BranchID != f.branches[swapCheaper] || ours[1].BranchID != f.branches[swapDearer] {
		t.Fatalf("offered %+v, want the rated cheaper kitchen then the dearer one", ours)
	}
	if ours[0].DifferenceMinor != f.prices[swapCheaper]-w3.PriceMinor || ours[1].DifferenceMinor != f.prices[swapDearer]-w3.PriceMinor {
		t.Fatalf("differences %d/%d", ours[0].DifferenceMinor, ours[1].DifferenceMinor)
	}
}

func TestSwapOpenWindow(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Dubai")
	now := time.Date(2026, 3, 10, 23, 30, 0, 0, loc)
	day := func(d int) time.Time { return time.Date(2026, 3, 10+d, 0, 0, 0, 0, loc) }
	cases := []struct {
		status, week string
		start        time.Time
		code         string
	}{
		{"active", "active", day(SwapCutoffDays), ""},
		{"active", "active", day(SwapCutoffDays - 1), "swap_cutoff_passed"},
		{"active", "active", day(0), "week_started"},
		{"active", "active", day(-7), "week_started"},
		{"active", "completed", day(10), "week_started"},
		{"pending_payment", "pending_payment", day(10), "rotation_not_active"},
		{"cancelled", "cancelled", day(10), "rotation_not_active"},
	}
	for _, c := range cases {
		err := swapOpen(c.status, c.week, c.start, loc, now)
		if codeOf(err) != c.code {
			t.Errorf("swapOpen(%s, %s, %s) = %v, want %q", c.status, c.week, c.start.Format("2006-01-02"), err, c.code)
		}
	}
}

// Paying the difference on a swapped week once is enough: a replay of the
// swap afterwards owes nothing, and two authorizations racing on the order
// record exactly one hold.
func TestSwapWeekDifferenceIsCollectedOnce(t *testing.T) {
	f := newSwapFixture(t, 7)
	ctx := context.Background()
	pay := payments.NewService(f.pool, payments.SandboxProvider{}, 1500, 500)

	res, err := f.svc.SwapRotationWeek(ctx, f.userID, f.rotation.ID, 3, f.plans[swapDearer])
	if err != nil {
		t.Fatalf("swap: %v", err)
	}
	if res.DueMinor <= 0 {
		t.Fatalf("due %d, want a difference to pay", res.DueMinor)
	}
	if w := f.week(t, 3); w.DueMinor == nil || *w.DueMinor != res.DueMinor {
		t.Fatalf("rotation week shows due %v, want %d while unpaid", w.DueMinor, res.DueMinor)
	}

	// Two taps on "Pay" at once.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = pay.Authorize(ctx, f.userID, res.OrderID, fmt.Sprintf("swap-%d", i))
		}(i)
	}
	wg.Wait()
	ok, conflict := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, httpx.ErrConflict):
			conflict++
		default:
			t.Fatalf("authorize: %v", err)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("authorizations: %d ok, %d conflict; want exactly one of each", ok, conflict)
	}
	if n := f.count(t, `SELECT count(*) FROM payments WHERE order_id = $1 AND status <> 'failed'`, res.OrderID); n != 1 {
		t.Fatalf("%d successful payments on the order, want 1", n)
	}
	if s := f.subStatus(t, *f.week(t, 3).SubscriptionID); s != "active" {
		t.Fatalf("paid week is %s, want active", s)
	}

	again, err := f.svc.SwapRotationWeek(ctx, f.userID, f.rotation.ID, 3, f.plans[swapDearer])
	if err != nil {
		t.Fatalf("repeat swap: %v", err)
	}
	if again.OrderID != res.OrderID || again.DueMinor != 0 {
		t.Fatalf("repeat after payment answered %v/%d, want %v/0", again.OrderID, again.DueMinor, res.OrderID)
	}
	if w := f.week(t, 3); w.DueMinor != nil {
		t.Fatalf("paid week still shows due %d", *w.DueMinor)
	}
	// A late retry against the paid order is a conflict, never a second hold.
	if _, err := pay.Authorize(ctx, f.userID, res.OrderID, "swap-late"); !errors.Is(err, httpx.ErrConflict) {
		t.Fatalf("authorize after payment: %v, want conflict", err)
	}
}

// The kitchen that cooks the last week of a rotation never cooks the first
// week of the renewal that follows it, and a swap cannot undo that from
// either side.
func TestSwapWeekKeepsNeighbouringRotationsApart(t *testing.T) {
	f := newSwapFixture(t, 1)
	ctx := context.Background()
	last := f.week(t, 4)
	// Two kitchens new to the customer first, so one of them gets week 1.
	plans := []uuid.UUID{f.plans[4], f.plans[5]}
	for i := 0; i < RotationKitchens; i++ {
		if p := f.plans[i]; p != last.PlanID && len(plans) < RotationKitchens {
			plans = append(plans, p)
		}
	}
	next, err := f.svc.RenewRotation(ctx, f.userID, f.rotation.ID, time.Now(), RenewOptions{PlanIDs: plans})
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	exec := func(sql string, args ...any) {
		if _, err := f.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%v\n%s", err, sql)
		}
	}
	exec(`UPDATE subscription_cycles SET status = 'paid', paid_at = now()
		 WHERE subscription_id IN (SELECT id FROM subscriptions WHERE rotation_id = $1)`, next.ID)
	exec(`UPDATE subscriptions SET status = 'active' WHERE rotation_id = $1`, next.ID)
	exec(`UPDATE meal_rotations SET status = 'active' WHERE id = $1`, next.ID)
	next, err = f.svc.MyRotation(ctx, f.userID, next.ID)
	if err != nil {
		t.Fatal(err)
	}
	if next.Weeks[0].BranchID == last.BranchID {
		t.Fatalf("the renewal starts with the old last kitchen")
	}

	// Renewal week 1 -> the old rotation's last kitchen: refused and not offered.
	_, err = f.svc.SwapRotationWeek(ctx, f.userID, next.ID, 1, last.PlanID)
	if fieldCode(err, "plan_id") != "follows_last_week" {
		t.Fatalf("swap renewal week 1 to the previous last kitchen: %v, want follows_last_week", err)
	}
	opts, err := f.svc.SwapOptions(ctx, f.userID, next.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range opts.Items {
		if o.BranchID == last.BranchID {
			t.Fatalf("the previous last kitchen is offered for renewal week 1")
		}
	}

	// Old rotation week 4 -> the renewal's first kitchen: the same rule.
	first := next.Weeks[0]
	_, err = f.svc.SwapRotationWeek(ctx, f.userID, f.rotation.ID, 4, first.PlanID)
	if code := fieldCode(err, "plan_id"); code != "follows_last_week" {
		t.Fatalf("swap old week 4 to the renewal's first kitchen: %v, want follows_last_week", err)
	}
	opts, err = f.svc.SwapOptions(ctx, f.userID, f.rotation.ID, 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range opts.Items {
		if o.BranchID == first.BranchID {
			t.Fatalf("the renewal's first kitchen is offered for the old week 4")
		}
	}
	if n := f.count(t, `SELECT count(*) FROM meal_rotation_swaps WHERE rotation_id IN ($1, $2)`, f.rotation.ID, next.ID); n != 0 {
		t.Fatalf("%d swap records after refusals, want 0", n)
	}
}
