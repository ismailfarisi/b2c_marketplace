package subscriptions

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type sentNotice struct {
	user    uuid.UUID
	key, ch string
	params  map[string]string
}

type recordingNotifier struct {
	mu   sync.Mutex
	sent []sentNotice
}

func (n *recordingNotifier) Notify(_ context.Context, _ pgx.Tx, userID uuid.UUID, key string, _ bool,
	params map[string]string, _ string) error {
	return n.NotifyOn(context.Background(), nil, userID, key, "", false, params, "")
}

func (n *recordingNotifier) NotifyOn(_ context.Context, _ pgx.Tx, userID uuid.UUID, key, ch string, _ bool,
	params map[string]string, _ string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sent = append(n.sent, sentNotice{user: userID, key: key, ch: ch, params: params})
	return nil
}

func (n *recordingNotifier) forUser(u uuid.UUID, key string) []sentNotice {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []sentNotice
	for _, s := range n.sent {
		if s.user == u && s.key == key {
			out = append(out, s)
		}
	}
	return out
}

func (f subFixture) branchID(t *testing.T) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := f.pool.QueryRow(context.Background(),
		`SELECT branch_id FROM subscription_plans WHERE id = $1`, f.planID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (f subFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%v\n%s", err, sql)
	}
}

func TestNextWeekPreviewAndNoticeDedupe(t *testing.T) {
	f := newSubFixture(t, 20)
	ctx := context.Background()
	dubai, _ := time.LoadLocation("Asia/Dubai")
	branch := f.branchID(t)

	today := dayIn(time.Now(), dubai)
	thursday := today.AddDate(0, 0, (int(time.Thursday)-int(today.Weekday())+7)%7)
	now := time.Date(thursday.Year(), thursday.Month(), thursday.Day(), 19, 0, 0, 0, dubai)
	start := NextWeekStart(now, dubai)

	sub, err := f.svc.Subscribe(ctx, f.input(start))
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	f.exec(t, `UPDATE subscriptions SET status = 'active' WHERE id = $1`, sub.ID)
	f.exec(t, `UPDATE vendor_branches SET name_ar = 'مطبخ', cuisines = '{lebanese}' WHERE id = $1`, branch)

	// A two-week menu whose week 2 is the week being previewed.
	cycle := uuid.New()
	f.exec(t, `INSERT INTO menu_cycles (id, branch_id, name, weeks, anchor_date) VALUES ($1, $2, 'C', 2, $3)`,
		cycle, branch, start.AddDate(0, 0, -7))
	for wk := 1; wk <= 2; wk++ {
		item := uuid.New()
		f.exec(t, `INSERT INTO menu_items (id, branch_id, name) VALUES ($1, $2, $3)`, item, branch, fmt.Sprintf("Week %d dish", wk))
		for d := 0; d < 7; d++ {
			day := uuid.New()
			f.exec(t, `INSERT INTO menu_cycle_days (id, cycle_id, week_no, weekday) VALUES ($1, $2, $3, $4)`, day, cycle, wk, d)
			f.exec(t, `INSERT INTO menu_day_items (cycle_day_id, menu_item_id, meal_slot, is_default) VALUES ($1, $2, 'lunch', true)`, day, item)
		}
	}

	previews, err := f.svc.NextWeek(ctx, f.userID, now)
	if err != nil {
		t.Fatalf("next week: %v", err)
	}
	if len(previews) != 1 {
		t.Fatalf("want 1 preview, got %d", len(previews))
	}
	p := previews[0]
	if p.StartsOn != start.Format("2006-01-02") || p.MenuWeek != 2 || p.KitchenName != "T" || len(p.Cuisines) != 1 {
		t.Fatalf("preview header: %+v", p)
	}
	if len(p.Meals) == 0 {
		t.Fatal("preview has no meals")
	}
	for _, m := range p.Meals {
		if len(m.Dishes) != 1 || m.Dishes[0].Name != "Week 2 dish" {
			t.Fatalf("%s: dishes %+v, want week 2's", m.Date, m.Dishes)
		}
	}

	rec := &recordingNotifier{}
	f.svc.notifier = rec
	early := time.Date(thursday.Year(), thursday.Month(), thursday.Day(), 17, 0, 0, 0, dubai)
	if _, err := f.svc.SendNextWeekNotices(ctx, early, 500); err != nil {
		t.Fatal(err)
	}
	if got := rec.forUser(f.userID, "meals.next_week"); len(got) != 0 {
		t.Fatalf("sent before Thursday 18:00: %+v", got)
	}
	for i := 0; i < 2; i++ {
		if _, err := f.svc.SendNextWeekNotices(ctx, now, 500); err != nil {
			t.Fatal(err)
		}
	}
	got := rec.forUser(f.userID, "meals.next_week")
	if len(got) != 2 || got[0].ch != "push" || got[1].ch != "whatsapp" {
		t.Fatalf("want one push + one WhatsApp, got %+v", got)
	}
	if got[0].params["dishes"] != "Week 2 dish" || got[0].params["kitchen_ar"] != "مطبخ" {
		t.Fatalf("params: %+v", got[0].params)
	}
	var markers int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM meal_variety_notices WHERE kind = 'next_week' AND user_id = $1`,
		f.userID).Scan(&markers); err != nil {
		t.Fatal(err)
	}
	if markers != 1 {
		t.Fatalf("want 1 sent marker, got %d", markers)
	}
}

func TestRotationRecapCountsAndNotice(t *testing.T) {
	f := newSubFixture(t, 20)
	ctx := context.Background()
	dubai, _ := time.LoadLocation("Asia/Dubai")
	branch := f.branchID(t)
	f.exec(t, `UPDATE vendor_branches SET cuisines = '{lebanese,kerala}' WHERE id = $1`, branch)

	today := dayIn(time.Now(), dubai)
	rotStart := today.AddDate(0, 0, -21)
	rot := uuid.New()
	f.exec(t, `INSERT INTO meal_rotations (id, rotation_no, user_id, address_id, status, start_date, end_date,
		meal_slots, delivery_weekdays, amount_minor)
		VALUES ($1, $2, $3, $4, 'active', $5, $6, '{lunch}', '{0,1,2,3,4}', 0)`,
		rot, "RT-"+rot.String()[:8], f.userID, f.addressID, rotStart, rotStart.AddDate(0, 0, 27))

	future := EarliestStart(20, dubai, time.Now()).AddDate(0, 0, 1)
	subs := make([]uuid.UUID, 4)
	for i := range subs {
		s, err := f.svc.Subscribe(ctx, f.input(future.AddDate(0, 0, i)))
		if err != nil {
			t.Fatalf("subscribe: %v", err)
		}
		subs[i] = s.ID
		ws := rotStart.AddDate(0, 0, 7*i)
		f.exec(t, `UPDATE subscriptions SET status = 'active', rotation_id = $2, rotation_week = $3,
			start_date = $4, end_date = $5 WHERE id = $1`, s.ID, rot, i+1, ws, ws.AddDate(0, 0, 6))
	}
	for i, st := range []string{"completed", "completed", "completed", "failed"} {
		f.exec(t, `INSERT INTO fulfillments (subscription_id, branch_id, kind, status, scheduled_date, window_id,
			address_line, point) VALUES ($1, $2, 'delivery', $3, $4, $5, 'x', ST_SetSRID(ST_MakePoint(55.23, 25.14), 4326)::geography)`,
			subs[0], branch, st, rotStart.AddDate(0, 0, i), f.windowID)
	}
	skip := rotStart.AddDate(0, 0, 8)
	f.exec(t, `INSERT INTO subscription_skips (subscription_id, skip_date, meal_slot) VALUES ($1, $2, 'lunch'), ($1, $2, 'dinner'), ($1, $3, NULL)`,
		subs[1], skip, skip.AddDate(0, 0, 1))
	f.exec(t, `INSERT INTO kitchen_reviews (branch_id, user_id, subscription_id, rating) VALUES ($1, $2, $3, 3), ($1, $2, $4, 5)`,
		branch, f.userID, subs[0], subs[2])

	recaps, err := f.svc.MyRecaps(ctx, f.userID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(recaps) != 1 {
		t.Fatalf("want 1 recap, got %d", len(recaps))
	}
	rc := recaps[0]
	if rc.KitchenCount != 4 || rc.CuisineCount != 2 || rc.DishesDelivered != 3 || rc.DaysSkipped != 2 {
		t.Fatalf("counts: kitchens %d cuisines %d dishes %d skipped %d", rc.KitchenCount, rc.CuisineCount, rc.DishesDelivered, rc.DaysSkipped)
	}
	if rc.Favourite == nil || rc.Favourite.Rating != 5 || rc.Ended {
		t.Fatalf("favourite/ended: %+v ended=%v", rc.Favourite, rc.Ended)
	}
	if rc.RenewPath != "/rotations/"+rot.String()+"/renew" {
		t.Fatalf("renew path %q", rc.RenewPath)
	}

	// Not shown before week 4.
	early, err := f.svc.MyRecaps(ctx, f.userID, time.Now().AddDate(0, 0, -2))
	if err != nil {
		t.Fatal(err)
	}
	if len(early) != 0 {
		t.Fatalf("recap shown in week 3: %+v", early)
	}

	rec := &recordingNotifier{}
	f.svc.notifier = rec
	for i := 0; i < 2; i++ {
		if _, err := f.svc.SendRecapNotices(ctx, time.Now(), 500); err != nil {
			t.Fatal(err)
		}
	}
	got := rec.forUser(f.userID, "meals.cycle_recap")
	if len(got) != 1 || got[0].ch != "push" || !strings.HasSuffix(got[0].params["link"], rc.RenewPath) {
		t.Fatalf("want one recap push linking to renew, got %+v", got)
	}
}

// A default dish the customer marked "not again" is not what the preview or
// the Thursday notice name: the day's other dish stands in, as on the prep
// sheet. A day with only the avoided dish keeps it.
func TestNextWeekPreviewHonoursAvoidedDishes(t *testing.T) {
	f := newSubFixture(t, 20)
	ctx := context.Background()
	dubai, _ := time.LoadLocation("Asia/Dubai")
	branch := f.branchID(t)

	today := dayIn(time.Now(), dubai)
	thursday := today.AddDate(0, 0, (int(time.Thursday)-int(today.Weekday())+7)%7)
	now := time.Date(thursday.Year(), thursday.Month(), thursday.Day(), 19, 0, 0, 0, dubai)
	start := NextWeekStart(now, dubai)

	sub, err := f.svc.Subscribe(ctx, f.input(start))
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	f.exec(t, `UPDATE subscriptions SET status = 'active' WHERE id = $1`, sub.ID)

	cycle := uuid.New()
	f.exec(t, `INSERT INTO menu_cycles (id, branch_id, name, weeks, anchor_date) VALUES ($1, $2, 'C', 1, $3)`, cycle, branch, start)
	avoided, other := uuid.New(), uuid.New()
	f.exec(t, `INSERT INTO menu_items (id, branch_id, name) VALUES ($1, $2, 'Okra stew'), ($3, $2, 'Grilled chicken')`, avoided, branch, other)
	// Sunday has only the avoided dish; the other days have a stand-in.
	for d := 0; d < 7; d++ {
		day := uuid.New()
		f.exec(t, `INSERT INTO menu_cycle_days (id, cycle_id, week_no, weekday) VALUES ($1, $2, 1, $3)`, day, cycle, d)
		f.exec(t, `INSERT INTO menu_day_items (cycle_day_id, menu_item_id, meal_slot, is_default, sort_order) VALUES ($1, $2, 'lunch', true, 0)`, day, avoided)
		if d != 0 {
			f.exec(t, `INSERT INTO menu_day_items (cycle_day_id, menu_item_id, meal_slot, is_default, sort_order) VALUES ($1, $2, 'lunch', false, 1)`, day, other)
		}
	}
	f.exec(t, `INSERT INTO meal_tastes (user_id, subject_kind, subject_id, sentiment) VALUES ($1, 'dish', $2, 'avoid')`, f.userID, avoided)

	previews, err := f.svc.NextWeek(ctx, f.userID, now)
	if err != nil {
		t.Fatalf("next week: %v", err)
	}
	if len(previews) != 1 {
		t.Fatalf("want 1 preview, got %d", len(previews))
	}
	p := previews[0]
	if len(p.Meals) == 0 {
		t.Fatal("preview has no meals")
	}
	for _, m := range p.Meals {
		d, ok := previewServedDish(m)
		if !ok {
			t.Fatalf("%s: no dish", m.Date)
		}
		defaults := 0
		for _, c := range m.Dishes {
			if c.IsDefault {
				defaults++
			}
		}
		if defaults != 1 {
			t.Fatalf("%s: %d defaults in %+v", m.Date, defaults, m.Dishes)
		}
		if m.Weekday == 0 {
			if d.ID != avoided {
				t.Fatalf("%s: a lone avoided dish should stay, got %+v", m.Date, d)
			}
			continue
		}
		if d.ID != other {
			t.Fatalf("%s: served %+v, want the stand-in", m.Date, d)
		}
	}
	en, _ := noticeDishes(p)
	if !strings.Contains(en, "Grilled chicken") {
		t.Fatalf("notice names %q, want the stand-in", en)
	}
}
