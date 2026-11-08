package subscriptions

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/seam/servicetype"
	"github.com/marketplace/api/internal/verticals/meals"
)

type suggestFixture struct {
	svc       *Service
	pool      *pgxpool.Pool
	userID    uuid.UUID
	addressID uuid.UUID
	lng, lat  float64
}

type suggestKitchen struct {
	cuisine     string
	rating      *float64
	ratings     int
	rotation    bool
	far         bool
	noWindow    bool
	weekPrice   int64
	lunchDish   string
	lunchDishAr string
}

func newSuggestFixture(t *testing.T) suggestFixture {
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
	// A random spot in the empty quarter keeps other tests' kitchens out of range.
	f := suggestFixture{svc: NewService(pool, reg, Config{}), pool: pool, userID: uuid.New(), addressID: uuid.New(),
		lng: 50 + rand.Float64()*4, lat: 19 + rand.Float64()*2}
	suggestExec(t, pool, `INSERT INTO users (id, phone) VALUES ($1, $2)`, f.userID, fmt.Sprintf("+97158%07d", rand.IntN(10_000_000)))
	suggestExec(t, pool, `INSERT INTO addresses (id, user_id, label, city, point) VALUES ($1, $2, 'Home', 'Dubai',
		ST_SetSRID(ST_MakePoint($3, $4), 4326)::geography)`, f.addressID, f.userID, f.lng, f.lat)
	return f
}

func suggestExec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("fixture: %v\n%s", err, sql)
	}
}

func (f suggestFixture) addKitchen(t *testing.T, k suggestKitchen) (branchID, planID uuid.UUID) {
	t.Helper()
	vendorID := uuid.New()
	branchID, planID = uuid.New(), uuid.New()
	lng, lat := f.lng, f.lat
	if k.far {
		lng += 1
	}
	suggestExec(t, f.pool, `INSERT INTO vendors (id, slug, legal_name, trade_name, status) VALUES ($1, $2, 'S LLC', 'S', 'active')`,
		vendorID, "suggest-"+vendorID.String())
	suggestExec(t, f.pool, `INSERT INTO vendor_branches (id, vendor_id, name, point, service_area, cuisines, rating, rating_count)
		VALUES ($1, $2, $3, ST_SetSRID(ST_MakePoint($4, $5), 4326)::geography,
		ST_Multi(ST_Buffer(ST_SetSRID(ST_MakePoint($4, $5), 4326)::geography, 5000)::geometry)::geography,
		$6, $7, $8)`, branchID, vendorID, k.cuisine+" kitchen", lng, lat, []string{k.cuisine}, k.rating, k.ratings)
	if !k.noWindow {
		suggestExec(t, f.pool, `INSERT INTO time_windows (branch_id, label, starts_at, ends_at) VALUES ($1, 'Lunch', '12:00', '14:00')`, branchID)
	}
	var weekPrice any
	if k.rotation {
		weekPrice = k.weekPrice
	}
	suggestExec(t, f.pool, `INSERT INTO subscription_plans (id, branch_id, service_type, name, cycle_days, days_per_week,
		cutoff_hours, price_minor, delivery_weekdays, meal_slots, rotation_enabled, rotation_week_price_minor)
		VALUES ($1, $2, 'meals_subscription', 'Weekday lunch', 7, 5, 20, $3, '{0,1,2,3,4}', '{lunch}', $4, $5)`,
		planID, branchID, k.weekPrice, k.rotation, weekPrice)
	if k.lunchDish != "" {
		cycleID, dayID, itemID := uuid.New(), uuid.New(), uuid.New()
		suggestExec(t, f.pool, `INSERT INTO menu_cycles (id, branch_id, name, weeks, anchor_date) VALUES ($1, $2, 'Menu', 1, '2024-01-07')`,
			cycleID, branchID)
		suggestExec(t, f.pool, `INSERT INTO menu_items (id, branch_id, name, name_ar) VALUES ($1, $2, $3, NULLIF($4, ''))`,
			itemID, branchID, k.lunchDish, k.lunchDishAr)
		suggestExec(t, f.pool, `INSERT INTO menu_cycle_days (id, cycle_id, week_no, weekday) VALUES ($1, $2, 1, 1)`, dayID, cycleID)
		suggestExec(t, f.pool, `INSERT INTO menu_day_items (cycle_day_id, menu_item_id, meal_slot, is_default) VALUES ($1, $2, 'lunch', true)`,
			dayID, itemID)
	}
	return branchID, planID
}

func rating(v float64) *float64 { return &v }

func TestSuggestRotationEligibility(t *testing.T) {
	f := newSuggestFixture(t)
	ctx := context.Background()

	if _, err := f.svc.SuggestRotation(ctx, SuggestInput{UserID: f.userID, AddressID: f.addressID}); fieldCode(err, "address_id") != "not_enough_kitchens" {
		t.Fatalf("empty area: %v", err)
	}
	if _, err := f.svc.SuggestRotation(ctx, SuggestInput{UserID: f.userID, AddressID: f.addressID, Weekdays: []int{7}}); fieldCode(err, "delivery_weekdays") != "invalid_weekday" {
		t.Fatalf("weekday 7: %v", err)
	}
	if _, err := f.svc.SuggestRotation(ctx, SuggestInput{UserID: f.userID, AddressID: f.addressID, Slots: []string{"lunch", "lunch"}}); fieldCode(err, "meal_slots") != "duplicate" {
		t.Fatalf("duplicate slot: %v", err)
	}

	ineligible := map[uuid.UUID]string{}
	add := func(name string, k suggestKitchen) {
		b, _ := f.addKitchen(t, k)
		ineligible[b] = name
	}
	add("out of area", suggestKitchen{cuisine: "thai", rotation: true, far: true, weekPrice: 20000, rating: rating(4.9), ratings: 40})
	add("not in rotation", suggestKitchen{cuisine: "chinese", weekPrice: 20000, rating: rating(4.9), ratings: 40})
	add("poorly rated", suggestKitchen{cuisine: "persian", rotation: true, weekPrice: 20000, rating: rating(2.1), ratings: 30})
	add("no delivery window", suggestKitchen{cuisine: "turkish", rotation: true, noWindow: true, weekPrice: 20000})

	indian1, _ := f.addKitchen(t, suggestKitchen{cuisine: "indian", rotation: true, weekPrice: 20000, rating: rating(4.8), ratings: 50,
		lunchDish: "Chicken Biryani", lunchDishAr: "برياني الدجاج"})
	indian2, _ := f.addKitchen(t, suggestKitchen{cuisine: "indian", rotation: true, weekPrice: 20000, rating: rating(4.8), ratings: 50,
		lunchDish: "Biryani chicken"})
	for _, c := range []string{"kerala", "arab", "filipino"} {
		f.addKitchen(t, suggestKitchen{cuisine: c, rotation: true, weekPrice: 21000, rating: rating(4.3), ratings: 8})
	}
	f.addKitchen(t, suggestKitchen{cuisine: "lebanese", rotation: true, weekPrice: 95000, rating: rating(5), ratings: 80})

	got, err := f.svc.SuggestRotation(ctx, SuggestInput{UserID: f.userID, AddressID: f.addressID})
	if err != nil {
		t.Fatalf("suggest: %v", err)
	}
	if len(got.PlanIDs) != RotationKitchens || len(got.Quote.Weeks) != RotationKitchens {
		t.Fatalf("plans %d weeks %d", len(got.PlanIDs), len(got.Quote.Weeks))
	}
	indians := 0
	for _, b := range got.BranchIDs {
		if name, bad := ineligible[b]; bad {
			t.Errorf("suggested an ineligible kitchen: %s", name)
		}
		if b == indian1 || b == indian2 {
			indians++
		}
	}
	if indians != 1 {
		t.Errorf("want one of the two Indian kitchens, got %d", indians)
	}
	for _, w := range got.Quote.Weeks {
		if w.PriceMinor > 30000 {
			t.Errorf("out of price band: %s %d", w.KitchenName, w.PriceMinor)
		}
	}
	if got.Quote.Variety == nil || got.Quote.Variety.DistinctCuisines != 4 || got.Quote.Variety.Level != VarietyGreat {
		t.Fatalf("variety %+v", got.Quote.Variety)
	}
	if len(got.Weekdays) != 5 || len(got.Slots) != 1 || got.Slots[0] != "lunch" {
		t.Fatalf("schedule %v %v", got.Weekdays, got.Slots)
	}

	// Preview scores whatever the customer picks and still quotes it.
	plans := []uuid.UUID{}
	rows, err := f.pool.Query(ctx, `SELECT id FROM subscription_plans WHERE branch_id = ANY($1)`, []uuid.UUID{indian1, indian2})
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id uuid.UUID
		_ = rows.Scan(&id)
		plans = append(plans, id)
	}
	rows.Close()
	for i, b := range got.BranchIDs {
		if len(plans) < RotationKitchens && b != indian1 && b != indian2 {
			plans = append(plans, got.PlanIDs[i])
		}
	}
	q, err := f.svc.PreviewRotation(ctx, RotationInput{UserID: f.userID, AddressID: f.addressID, PlanIDs: plans,
		StartDate: calendarDay(EarliestStart(20, time.UTC, time.Now())), Weekdays: got.Weekdays, Slots: got.Slots})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if q.Variety == nil || q.Variety.DistinctCuisines > 3 {
		t.Fatalf("preview variety %+v", q.Variety)
	}
	repeated := false
	for _, w := range q.Variety.Warnings {
		repeated = repeated || (w.Code == WarnRepeatedDish && w.Kitchens == 2 && w.Weekday != nil && *w.Weekday == 1)
	}
	if !repeated {
		t.Errorf("want the Monday biryani warning, got %+v", q.Variety.Warnings)
	}
}
