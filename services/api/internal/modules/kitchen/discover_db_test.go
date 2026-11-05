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

	var branchID uuid.UUID
	if err := pool.QueryRow(ctx, `
		SELECT b.id FROM vendor_branches b
		  JOIN vendors v ON v.id = b.vendor_id
		 WHERE b.deleted_at IS NULL AND b.is_active AND v.deleted_at IS NULL AND v.status = 'active'
		   AND EXISTS (SELECT 1 FROM subscription_plans sp
		                WHERE sp.branch_id = b.id AND sp.service_type = $1 AND sp.is_active)
		 LIMIT 1`, planServiceType).Scan(&branchID); err != nil {
		t.Skipf("no kitchen with a live plan seeded: %v", err)
	}

	tag := fmt.Sprintf("%08d", rand.IntN(100_000_000))
	name, nameAr := "Zzdish"+tag, "طبق"+tag
	var itemID, cycleID, dayID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO menu_items (branch_id, name, name_ar) VALUES ($1, $2, $3) RETURNING id`,
		branchID, name, nameAr).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM menu_items WHERE id = $1`, itemID) })
	if err := pool.QueryRow(ctx, `INSERT INTO menu_cycles (branch_id, name, anchor_date, is_active) VALUES ($1, $2, CURRENT_DATE, false) RETURNING id`,
		branchID, "test "+tag).Scan(&cycleID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM menu_cycles WHERE id = $1`, cycleID) })
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
