package kitchen

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/marketplace/api/internal/modules/notify"
)

// A two-week menu serves week 2's dishes in the second week of the cycle: on
// the storefront's dishes-for-a-date row, on the kitchen page's calendar and
// on the prep sheet's defaults.
func TestMultiWeekMenuServesWeekTwo(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	n := rand.IntN(100_000_000)
	vendorID, branchID := uuid.New(), uuid.New()
	week1, week2 := uuid.New(), uuid.New()
	// Anchor the cycle on last week's Sunday, so this week is week 2.
	var thisSunday time.Time
	if err := pool.QueryRow(ctx, `
		SELECT d - extract(dow FROM d)::int FROM (SELECT (now() AT TIME ZONE 'Asia/Dubai')::date AS d) t`).Scan(&thisSunday); err != nil {
		t.Fatal(err)
	}
	anchor := thisSunday.AddDate(0, 0, -7)
	monday := thisSunday.AddDate(0, 0, 1)
	for _, st := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO vendors (id, slug, legal_name, trade_name, status) VALUES ($1, $2, 'T LLC', 'T', 'active')`,
			[]any{vendorID, fmt.Sprintf("multiweek-%d", n)}},
		{`INSERT INTO vendor_branches (id, vendor_id, name, timezone, point) VALUES ($1, $2, 'T', 'Asia/Dubai',
			ST_SetSRID(ST_MakePoint(55.23, 25.14), 4326)::geography)`,
			[]any{branchID, vendorID}},
		{`INSERT INTO subscription_plans (branch_id, service_type, name, cycle_days, days_per_week,
			cutoff_hours, price_minor, delivery_weekdays, meal_slots)
			VALUES ($1, $2, 'T plan', 7, 5, 12, 70000, '{0,1,2,3,4}', '{lunch}')`,
			[]any{branchID, planServiceType}},
		{`INSERT INTO menu_items (id, branch_id, name) VALUES ($1, $3, 'Week one biryani'), ($2, $3, 'Week two kabsa')`,
			[]any{week1, week2, branchID}},
	} {
		if _, err := pool.Exec(ctx, st.sql, st.args...); err != nil {
			t.Fatalf("%s: %v", st.sql, err)
		}
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM menu_cycles WHERE branch_id = $1`, branchID)
		_, _ = pool.Exec(bg, `DELETE FROM menu_items WHERE branch_id = $1`, branchID)
		_, _ = pool.Exec(bg, `DELETE FROM subscription_plans WHERE branch_id = $1`, branchID)
		_, _ = pool.Exec(bg, `DELETE FROM vendor_branches WHERE id = $1`, branchID)
		_, _ = pool.Exec(bg, `DELETE FROM vendors WHERE id = $1`, vendorID)
	})
	var cycleID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO menu_cycles (branch_id, name, weeks, anchor_date) VALUES ($1, 'T', 2, $2) RETURNING id`,
		branchID, anchor).Scan(&cycleID); err != nil {
		t.Fatal(err)
	}
	for week, dish := range map[int]uuid.UUID{1: week1, 2: week2} {
		for d := 0; d <= 4; d++ {
			if _, err := pool.Exec(ctx, `
				WITH day AS (INSERT INTO menu_cycle_days (cycle_id, week_no, weekday) VALUES ($1, $2, $3) RETURNING id)
				INSERT INTO menu_day_items (cycle_day_id, menu_item_id, meal_slot, is_default)
				SELECT id, $4, 'lunch', true FROM day`, cycleID, week, d, dish); err != nil {
				t.Fatal(err)
			}
		}
	}

	s := NewService(pool)
	dishOn := func(day time.Time) string {
		t.Helper()
		ds, err := s.DishesOn(ctx, DiscoverFilter{}, day)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range ds {
			if d.BranchID == branchID {
				return d.Name
			}
		}
		return ""
	}
	if got := dishOn(monday); got != "Week two kabsa" {
		t.Errorf("this Monday (week 2) serves %q", got)
	}
	if got := dishOn(monday.AddDate(0, 0, 7)); got != "Week one biryani" {
		t.Errorf("next Monday (week 1 again) serves %q", got)
	}

	// The prep sheet's default-dish query runs against the date's cycle week.
	if _, err := s.PrepSheet(ctx, branchID, monday); err != nil {
		t.Fatal(err)
	}

	k, err := s.PublicKitchen(ctx, branchID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if k.MenuWeeks != 2 || k.Menu.CurrentWeek != 2 || len(k.MenuCalendar) != 2 {
		t.Fatalf("menu_weeks=%d current=%d calendar=%d", k.MenuWeeks, k.Menu.CurrentWeek, len(k.MenuCalendar))
	}
	if sl := k.MenuCalendar[0].Slots; len(sl) == 0 || sl[0].Default.Name != "Week two kabsa" {
		t.Errorf("this week's calendar = %+v", sl)
	}
	if sl := k.MenuCalendar[1].Slots; len(sl) == 0 || sl[0].Default.Name != "Week one biryani" {
		t.Errorf("next week's calendar = %+v", sl)
	}
	for _, sl := range k.Menu.Slots {
		if sl.Week != 2 {
			t.Errorf("menu (this week) has a week %d slot", sl.Week)
		}
	}

	// Changing the menu length keeps this week's dishes where they are.
	if _, err := s.SetMenuWeeks(ctx, branchID, 3); err == nil {
		t.Error("extended to 3 weeks with an empty week 3")
	}
	if _, err := s.CopyMenuWeek(ctx, branchID, 1, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CopyMenuWeek(ctx, branchID, 1, 3); err == nil {
		t.Error("copied into a week that already has dishes")
	}
	m, err := s.SetMenuWeeks(ctx, branchID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if m.Weeks != 3 || m.CurrentWeek != 2 {
		t.Fatalf("after 3 weeks: weeks=%d current=%d", m.Weeks, m.CurrentWeek)
	}
	if got := dishOn(monday); got != "Week two kabsa" {
		t.Errorf("after extending, this Monday serves %q", got)
	}
}

// Shrinking the menu to one week must not leave customers with a week-2 dish
// nobody will cook: their future picks fall back to the default in the same
// transaction, and each affected subscription gets one reset event. Picks the
// one-week menu still serves, and days already fulfilled, stay.
func TestShrinkingTheMenuResetsOrphanedSelections(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	n := rand.IntN(100_000_000)
	vendorID, branchID, planID := uuid.New(), uuid.New(), uuid.New()
	userID, addressID, subID := uuid.New(), uuid.New(), uuid.New()
	week1, week2 := uuid.New(), uuid.New()
	var thisSunday time.Time
	if err := pool.QueryRow(ctx, `
		SELECT d - extract(dow FROM d)::int FROM (SELECT (now() AT TIME ZONE 'Asia/Dubai')::date AS d) t`).Scan(&thisSunday); err != nil {
		t.Fatal(err)
	}
	// This week is week 1; next week is week 2 of a two-week cycle.
	anchor := thisSunday
	nextMonday := thisSunday.AddDate(0, 0, 8)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	exec(`INSERT INTO vendors (id, slug, legal_name, trade_name, status) VALUES ($1, $2, 'T LLC', 'T', 'active')`,
		vendorID, fmt.Sprintf("shrink-%d", n))
	exec(`INSERT INTO vendor_branches (id, vendor_id, name, timezone, point) VALUES ($1, $2, 'T', 'Asia/Dubai',
		ST_SetSRID(ST_MakePoint(55.23, 25.14), 4326)::geography)`, branchID, vendorID)
	exec(`INSERT INTO subscription_plans (id, branch_id, service_type, name, cycle_days, days_per_week,
		cutoff_hours, price_minor, delivery_weekdays, meal_slots)
		VALUES ($1, $2, $3, 'T plan', 7, 5, 12, 70000, '{0,1,2,3,4}', '{lunch}')`, planID, branchID, planServiceType)
	exec(`INSERT INTO menu_items (id, branch_id, name) VALUES ($1, $3, 'Week one biryani'), ($2, $3, 'Week two kabsa')`,
		week1, week2, branchID)
	exec(`INSERT INTO users (id, phone) VALUES ($1, $2)`, userID, fmt.Sprintf("+97157%07d", n))
	exec(`INSERT INTO addresses (id, user_id, label, city, point) VALUES ($1, $2, 'Home', 'Dubai',
		ST_SetSRID(ST_MakePoint(55.23, 25.14), 4326)::geography)`, addressID, userID)
	exec(`INSERT INTO subscriptions (id, subscription_no, user_id, branch_id, vendor_id, plan_id, service_type,
		status, start_date, address_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', $8, $9)`,
		subID, fmt.Sprintf("SHR-%d", n), userID, branchID, vendorID, planID, planServiceType, thisSunday, addressID)
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM outbox_events WHERE aggregate_id = $1`, subID)
		_, _ = pool.Exec(bg, `DELETE FROM notifications WHERE user_id = $1`, userID)
		_, _ = pool.Exec(bg, `DELETE FROM subscriptions WHERE id = $1`, subID)
		_, _ = pool.Exec(bg, `DELETE FROM addresses WHERE id = $1`, addressID)
		_, _ = pool.Exec(bg, `DELETE FROM users WHERE id = $1`, userID)
		_, _ = pool.Exec(bg, `DELETE FROM menu_cycles WHERE branch_id = $1`, branchID)
		_, _ = pool.Exec(bg, `DELETE FROM menu_items WHERE branch_id = $1`, branchID)
		_, _ = pool.Exec(bg, `DELETE FROM subscription_plans WHERE branch_id = $1`, branchID)
		_, _ = pool.Exec(bg, `DELETE FROM vendor_branches WHERE id = $1`, branchID)
		_, _ = pool.Exec(bg, `DELETE FROM vendors WHERE id = $1`, vendorID)
	})
	var cycleID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO menu_cycles (branch_id, name, weeks, anchor_date) VALUES ($1, 'T', 2, $2) RETURNING id`,
		branchID, anchor).Scan(&cycleID); err != nil {
		t.Fatal(err)
	}
	for week, dish := range map[int]uuid.UUID{1: week1, 2: week2} {
		for d := 0; d <= 4; d++ {
			exec(`WITH day AS (INSERT INTO menu_cycle_days (cycle_id, week_no, weekday) VALUES ($1, $2, $3) RETURNING id)
				INSERT INTO menu_day_items (cycle_day_id, menu_item_id, meal_slot, is_default)
				SELECT id, $4, 'lunch', true FROM day`, cycleID, week, d, dish)
		}
	}
	// Next Monday (week 2): the customer chose the kabsa. Next Tuesday: they
	// chose the biryani, which week 1 still serves after the shrink. A week-2
	// pick far in the past was already served and is history.
	exec(`INSERT INTO subscription_meal_selections (subscription_id, service_date, meal_slot, menu_item_id, chosen_by)
		VALUES ($1, $2, 'lunch', $3, 'customer'), ($1, $4, 'lunch', $5, 'customer'), ($1, $6, 'lunch', $3, 'customer')`,
		subID, nextMonday, week2, nextMonday.AddDate(0, 0, 1), week1, thisSunday.AddDate(0, 0, -13))

	s := NewService(pool).WithNotifier(notify.NewService(pool, nil, nil))
	m, err := s.SetMenuWeeks(ctx, branchID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if m.Weeks != 1 {
		t.Fatalf("weeks = %d", m.Weeks)
	}
	var kept []string
	rows, err := pool.Query(ctx, `SELECT service_date::text FROM subscription_meal_selections
		WHERE subscription_id = $1 ORDER BY service_date`, subID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatal(err)
		}
		kept = append(kept, d)
	}
	rows.Close()
	want := []string{thisSunday.AddDate(0, 0, -13).Format("2006-01-02"), nextMonday.AddDate(0, 0, 1).Format("2006-01-02")}
	if len(kept) != 2 || kept[0] != want[0] || kept[1] != want[1] {
		t.Fatalf("selections left = %v, want %v (the week-2 pick for next Monday must go)", kept, want)
	}
	var events int
	var payload []byte
	if err := pool.QueryRow(ctx, `SELECT count(*), min(payload::text)::bytea FROM outbox_events
		WHERE aggregate_id = $1 AND event_type = 'menu.selections_reset'`, subID).Scan(&events, &payload); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("reset events = %d, want 1", events)
	}
	var notices int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM notifications
		WHERE user_id = $1 AND template_key = 'menu.selections_reset'`, userID).Scan(&notices); err != nil {
		t.Fatal(err)
	}
	if notices != 1 {
		t.Errorf("customer notices = %d, want 1", notices)
	}
	if got := string(payload); !contains(got, `"count": 1`) || !contains(got, nextMonday.Format("2006-01-02")) {
		t.Errorf("reset payload = %s", got)
	}
	// The prep sheet for next Monday now defaults to the one-week dish.
	if _, err := s.PrepSheet(ctx, branchID, nextMonday); err != nil {
		t.Fatal(err)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
