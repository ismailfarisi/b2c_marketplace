package kitchen

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The storefront search promises "kitchens or dishes": a dish on a kitchen's
// live menu finds that kitchen, by its English or Arabic name.
func TestDiscoverMatchesMenuDishes(t *testing.T) {
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

	// The test owns its kitchen: a shared demo branch must not be left with
	// a second active menu (the schema now forbids one anyway).
	tag := fmt.Sprintf("%08d", rand.IntN(100_000_000))
	vendorID, branchID := uuid.New(), uuid.New()
	for _, st := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO vendors (id, slug, legal_name, trade_name, status) VALUES ($1, $2, 'T LLC', 'T', 'active')`,
			[]any{vendorID, "discover-" + tag}},
		{`INSERT INTO vendor_branches (id, vendor_id, name, timezone, point) VALUES ($1, $2, $3, 'Asia/Dubai',
			ST_SetSRID(ST_MakePoint(55.23, 25.14), 4326)::geography)`,
			[]any{branchID, vendorID, "Discover " + tag}},
		{`INSERT INTO subscription_plans (branch_id, service_type, name, cycle_days, days_per_week,
			cutoff_hours, price_minor, delivery_weekdays, meal_slots)
			VALUES ($1, $2, 'T plan', 7, 5, 12, 70000, '{0,1,2,3,4}', '{lunch}')`,
			[]any{branchID, planServiceType}},
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

	name, nameAr := "Zzdish"+tag, "طبق"+tag
	var itemID, cycleID, dayID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO menu_items (branch_id, name, name_ar) VALUES ($1, $2, $3) RETURNING id`,
		branchID, name, nameAr).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO menu_cycles (branch_id, name, anchor_date, is_active) VALUES ($1, $2, CURRENT_DATE, false) RETURNING id`,
		branchID, "test "+tag).Scan(&cycleID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO menu_cycle_days (cycle_id, week_no, weekday) VALUES ($1, 1, 0) RETURNING id`,
		cycleID).Scan(&dayID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO menu_day_items (cycle_day_id, menu_item_id, meal_slot) VALUES ($1, $2, 'lunch')`,
		dayID, itemID); err != nil {
		t.Fatal(err)
	}

	s := NewService(pool)
	found := func(q string) bool {
		t.Helper()
		ks, err := s.Discover(ctx, DiscoverFilter{Query: q})
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range ks {
			if k.BranchID == branchID {
				return true
			}
		}
		return false
	}

	// An inactive menu is not on sale, so its dishes don't match.
	if found(name) {
		t.Fatal("dish on an inactive menu matched")
	}
	if _, err := pool.Exec(ctx, `UPDATE menu_cycles SET is_active = true WHERE id = $1`, cycleID); err != nil {
		t.Fatal(err)
	}
	if !found(name) {
		t.Fatal("English dish name did not find its kitchen")
	}
	if !found("zzDISH" + tag) {
		t.Fatal("dish search is case-sensitive")
	}
	if !found(nameAr) {
		t.Fatal("Arabic dish name did not find its kitchen")
	}

	// Arabic is typed with or without tashkeel and tatweel, and with any of
	// the alef / yaa / taa marbuta spellings; all of them are the same dish.
	voweled := "مَنْسَفُ إِبْراهيمَ بالدجاجة على الأرز" + tag
	if _, err := pool.Exec(ctx, `UPDATE menu_items SET name_ar = $2 WHERE id = $1`, itemID, voweled); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		voweled,
		"منسف ابراهيم بالدجاجه علي الارز" + tag,
		"إبراهيم بالدجاجة" + " على الأرز" + tag,
		"بالدجـــاجة علي آلارز" + tag,
		"مَنْسَف",
	} {
		if !found(q) {
			t.Errorf("Arabic query %q did not find the voweled dish", q)
		}
	}
	plain := "منسف ابراهيم بالدجاجه" + tag
	if _, err := pool.Exec(ctx, `UPDATE menu_items SET name_ar = $2 WHERE id = $1`, itemID, plain); err != nil {
		t.Fatal(err)
	}
	if !found("مَنْسَفُ إِبْراهيمَ بالدجاجة" + tag) {
		t.Error("voweled query did not find the plain dish")
	}
	if found("منسف لحم" + tag) {
		t.Error("a different Arabic dish matched")
	}
	if found("Nodish" + tag) {
		t.Fatal("unrelated query matched")
	}
}
