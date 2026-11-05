package company

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/modules/addresses"
	"github.com/marketplace/api/internal/modules/company/credits"
	"github.com/marketplace/api/internal/platform/postgres"
)

// billedLunch is an office lunch whose one paid cycle was charged to the
// company, the way settle records it.
type billedLunch struct{ sub, cycle uuid.UUID }

func newBilledLunch(t *testing.T, pool *pgxpool.Pool, companyID, user uuid.UUID, amount int64) billedLunch {
	t.Helper()
	ctx := context.Background()
	var plan, branch, vendor uuid.UUID
	var serviceType string
	if err := pool.QueryRow(ctx, `
		SELECT sp.id, sp.branch_id, b.vendor_id, sp.service_type
		  FROM subscription_plans sp JOIN vendor_branches b ON b.id = sp.branch_id
		 LIMIT 1`).Scan(&plan, &branch, &vendor, &serviceType); err != nil {
		t.Skipf("no subscription plan seeded: %v", err)
	}
	addr, err := addresses.NewService(pool).Create(ctx, user, addresses.Input{
		Label: "Office", City: "Dubai", Point: &addresses.GeoPoint{Lat: 25.18, Lng: 55.27}})
	if err != nil {
		t.Fatalf("address: %v", err)
	}
	var l billedLunch
	var order uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO orders (order_no, user_id, branch_id, vendor_id, service_type)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		fmt.Sprintf("T-%d", rand.Int64()), user, branch, vendor, serviceType).Scan(&order); err != nil {
		t.Fatalf("order: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO subscriptions (subscription_no, user_id, branch_id, vendor_id, plan_id, service_type,
		                           status, start_date, address_id, company_id)
		VALUES ($1, $2, $3, $4, $5, $6, 'cancelled', CURRENT_DATE, $7, $8) RETURNING id`,
		fmt.Sprintf("S-%d", rand.Int64()), user, branch, vendor, plan, serviceType, addr.ID, companyID).
		Scan(&l.sub); err != nil {
		t.Fatalf("subscription: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO subscription_cycles (subscription_id, cycle_no, starts_on, ends_on, order_id, status, amount_minor)
		VALUES ($1, 1, CURRENT_DATE, CURRENT_DATE + 27, $2, 'cancelled', $3) RETURNING id`,
		l.sub, order, amount).Scan(&l.cycle); err != nil {
		t.Fatalf("cycle: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO company_charges (company_id, kind, subscription_id, order_id, user_id, amount_minor, currency)
		VALUES ($1, 'charge', $2, $3, $4, $5, 'AED')`, companyID, l.sub, order, user, amount); err != nil {
		t.Fatalf("charge: %v", err)
	}
	return l
}

// refund writes the cancel credit the subscriptions module writes, then
// applies it as that module does, in the same transaction.
func refund(t *testing.T, pool *pgxpool.Pool, companyID, user uuid.UUID, l billedLunch, amount int64) {
	t.Helper()
	ctx := context.Background()
	if err := postgres.InTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO company_charges (company_id, kind, subscription_id, cycle_id, user_id, amount_minor, currency)
			VALUES ($1, 'credit', $2, $3, $4, $5, 'AED')`, companyID, l.sub, l.cycle, user, -amount); err != nil {
			return err
		}
		return credits.Apply(ctx, tx, companyID)
	}); err != nil {
		t.Fatalf("refund: %v", err)
	}
}

// A refund for a lunch already on an issued statement reduces what that
// statement asks for, as an adjustment line, instead of sitting as negative
// unbilled credit. Only the part beyond the open balance carries forward.
func TestRefundCreditAppliesToOpenStatement(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	svc := NewService(pool, nil, nil)
	admin, _ := newUser(t, pool)
	c, err := svc.Register(ctx, admin, registration(fmt.Sprintf("Refund Co %d LLC", rand.IntN(1_000_000))))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	limit := int64(1_000_000)
	if err := svc.Review(ctx, admin, c.ID, ReviewInput{Status: "active", CreditLimitMinor: &limit}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	employee, _ := newUser(t, pool)
	a := newBilledLunch(t, pool, c.ID, employee, 100_000)
	b := newBilledLunch(t, pool, c.ID, employee, 50_000)
	x := newBilledLunch(t, pool, c.ID, employee, 10_000)
	stID, err := svc.IssueStatement(ctx, admin, c.ID, time.Now())
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	statement := func() Statement {
		t.Helper()
		st, err := svc.AdminStatement(ctx, c.ID, stID)
		if err != nil {
			t.Fatalf("statement: %v", err)
		}
		return st
	}
	if st := statement(); st.TotalMinor != 160_000 || st.AmountDueMinor != 160_000 || st.AdjustmentMinor != 0 {
		t.Fatalf("issued statement = %+v", st)
	}

	// Part refund of lunch A: comes straight off the open statement.
	refund(t, pool, c.ID, employee, a, 40_000)
	st := statement()
	if st.Status != "issued" || st.TotalMinor != 160_000 || st.AdjustmentMinor != -40_000 || st.AmountDueMinor != 120_000 {
		t.Fatalf("after part refund: %+v", st)
	}
	last := st.Lines[len(st.Lines)-1]
	if !last.Adjustment || last.Kind != "credit" || last.AmountMinor != -40_000 || len(st.Lines) != 4 {
		t.Fatalf("adjustment line = %+v (of %d)", last, len(st.Lines))
	}
	d, err := svc.AdminDetail(ctx, c.ID)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if d.Credit.UnbilledMinor != 0 || d.Credit.OutstandingMinor != 120_000 || len(d.Unbilled) != 0 {
		t.Fatalf("credit after part refund = %+v, unbilled %d", d.Credit, len(d.Unbilled))
	}

	// A second adjustment on the same statement must not count its total twice.
	refund(t, pool, c.ID, employee, x, 10_000)
	d, err = svc.AdminDetail(ctx, c.ID)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if d.Credit.OutstandingMinor != 110_000 {
		t.Fatalf("credit after two refunds = %+v", d.Credit)
	}

	// A refund larger than what is left: the statement is settled, and only
	// the excess carries forward as unbilled credit.
	refund(t, pool, c.ID, employee, b, 150_000)
	st = statement()
	if st.Status != "paid" || st.AmountDueMinor != 0 || st.AdjustmentMinor != -160_000 {
		t.Fatalf("after big refund: %+v", st)
	}
	d, err = svc.AdminDetail(ctx, c.ID)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if d.Credit.UnbilledMinor != -40_000 || d.Credit.OutstandingMinor != 0 || len(d.Unbilled) != 1 {
		t.Fatalf("credit after big refund = %+v, unbilled %+v", d.Credit, d.Unbilled)
	}
	if err := svc.MarkStatementPaid(ctx, admin, c.ID, stID, "TT-1"); err == nil {
		t.Fatal("a statement settled by credit should not be payable again")
	}

	// A refund for a lunch not billed yet nets against its own charge on the
	// next statement and is not applied to an open one.
	st2 := newBilledLunch(t, pool, c.ID, employee, 30_000)
	id2, err := svc.IssueStatement(ctx, admin, c.ID, time.Now())
	if err == nil {
		t.Fatalf("net-negative unbilled should not issue a statement, got %v", id2)
	}
	refund(t, pool, c.ID, employee, st2, 30_000)
	d, err = svc.AdminDetail(ctx, c.ID)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if d.Credit.UnbilledMinor != -40_000 {
		t.Fatalf("unbilled after refunding an unbilled lunch = %d, want -40000", d.Credit.UnbilledMinor)
	}
}
