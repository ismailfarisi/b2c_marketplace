package subscriptions

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The price cap puts a previous kitchen back when a fresh suggestion is too
// dear. A kitchen the customer marked "not again" must never be that kitchen.
func TestCappedProposalKeepsNotAgainKitchensOut(t *testing.T) {
	// Either previous kitchen may come first in the fallback order, so try
	// avoiding each of them.
	t.Run("third avoided", func(t *testing.T) { testCappedProposalAvoids(t, 3, 2) })
	t.Run("second avoided", func(t *testing.T) { testCappedProposalAvoids(t, 2, 3) })
}

func testCappedProposalAvoids(t *testing.T, avoided, plain int) {
	f := newSwapFixture(t, 10)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		if _, err := f.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%v\n%s", err, sql)
		}
	}
	// Other tests leave kitchens around this address; a spot of our own keeps
	// only the fixture's six eligible.
	lon := 52.0 + float64(uuid.New().ID()%100000)/100.0
	exec(`UPDATE addresses SET point = ST_SetSRID(ST_MakePoint($2, 23.5), 4326)::geography WHERE id = $1`, f.addressID, lon)
	exec(`UPDATE vendor_branches SET point = ST_SetSRID(ST_MakePoint($2, 23.5), 4326)::geography,
	        service_area = ST_Multi(ST_Buffer(ST_SetSRID(ST_MakePoint($2, 23.5), 4326)::geography, 15000)::geometry)::geography
	      WHERE id = ANY($1)`, f.branches, lon)

	// Kitchens 0 and 1 are loved and kept; one previous kitchen is "not
	// again", the other just a kitchen the customer ate from. Kitchen 4 becomes far too dear so the
	// fresh proposal blows the cap and something has to be substituted.
	for _, i := range []int{0, 1} {
		exec(`INSERT INTO meal_tastes (user_id, subject_kind, subject_id, sentiment) VALUES ($1, 'kitchen', $2, 'favourite')`,
			f.userID, f.branches[i])
	}
	exec(`INSERT INTO meal_tastes (user_id, subject_kind, subject_id, sentiment) VALUES ($1, 'kitchen', $2, 'avoid')`,
		f.userID, f.branches[avoided])
	exec(`UPDATE subscription_plans SET price_minor = 40000, rotation_week_price_minor = 40000 WHERE id = $1`, f.plans[4])

	start, _ := time.Parse("2006-01-02", f.rotation.EndDate)
	start = start.AddDate(0, 0, 1)
	ids, err := f.svc.cappedProposalPlans(ctx, f.rotation, f.userID, f.addressID, start)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != RotationKitchens {
		t.Fatalf("plans = %v, want %d", ids, RotationKitchens)
	}
	has := func(id uuid.UUID) bool {
		for _, got := range ids {
			if got == id {
				return true
			}
		}
		return false
	}
	if has(f.plans[avoided]) {
		t.Fatalf("the capped renewal put the 'not again' kitchen back: %v", ids)
	}
	if has(f.plans[4]) {
		t.Fatalf("the 40000 kitchen must have been capped out: %v", ids)
	}
	if !has(f.plans[plain]) {
		t.Fatalf("the ordinary previous kitchen is the one to fall back on: %v", ids)
	}
	var total int64
	for _, id := range ids {
		var p int64
		if err := f.pool.QueryRow(ctx, `SELECT rotation_week_price_minor FROM subscription_plans WHERE id = $1`, id).Scan(&p); err != nil {
			t.Fatal(err)
		}
		total += p
	}
	if cap := f.rotation.AmountMinor * 110 / 100; total > cap {
		t.Fatalf("total %d over the cap %d", total, cap)
	}
}
