package invoicing

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/platform/postgres"
)

func dbPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// issueTestInvoice writes an invoice for a fresh order through the real
// insert path, so the test covers what gets snapshotted.
func issueTestInvoice(t *testing.T, pool *pgxpool.Pool, svc *Service, buyer uuid.UUID, supplier Party) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var branch, vendor uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id, vendor_id FROM vendor_branches LIMIT 1`).Scan(&branch, &vendor); err != nil {
		t.Skipf("no branch seeded: %v", err)
	}
	var order uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO orders (order_no, user_id, branch_id, vendor_id, service_type)
		VALUES ($1, $2, $3, $4, 'meals_one_off') RETURNING id`,
		fmt.Sprintf("T-%d", rand.Int64()), buyer, branch, vendor).Scan(&order); err != nil {
		t.Fatalf("order: %v", err)
	}
	doc := Document{
		Kind: Simplified, Country: "AE", Currency: "AED",
		Supplier: supplier, Buyer: Party{Name: "Layla"}, SupplyDate: time.Now(),
		Lines: []Line{{Description: "Lunch", Quantity: 1, UnitNetMinor: 1000, NetMinor: 1000,
			TaxCategory: "S", TaxRateBP: 500, TaxMinor: 50, GrossMinor: 1050}},
		NetMinor: 1000, TaxMinor: 50, GrossMinor: 1050,
	}
	var id uuid.UUID
	if err := postgres.InTx(ctx, pool, func(tx pgx.Tx) error {
		var err error
		id, err = svc.insert(ctx, tx, doc, insertRefs{OrderID: &order, BuyerUserID: &buyer})
		return err
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	return id
}

// The printed invoice carries the supplier's address as it was at issue, and
// a credit note keeps it.
func TestPrintableCarriesSupplierAddress(t *testing.T) {
	pool := dbPool(t)
	ctx := context.Background()
	svc := NewService(pool, nil, false)
	var buyer uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users (phone) VALUES ($1) RETURNING id`,
		fmt.Sprintf("+97158%07d", rand.IntN(10_000_000))).Scan(&buyer); err != nil {
		t.Fatal(err)
	}
	supplier := Party{Name: "Switeaz FZ-LLC", TRN: "100000000000003", Address: "Office 12, Dubai Internet City, Dubai"}
	id := issueTestInvoice(t, pool, svc, buyer, supplier)

	p, err := svc.Get(ctx, id, &buyer)
	if err != nil {
		t.Fatal(err)
	}
	if p.Supplier.Address != supplier.Address || p.Supplier.Name != supplier.Name {
		t.Fatalf("supplier %+v", p.Supplier)
	}
	cn, err := svc.CreditNote(ctx, id, "refund")
	if err != nil {
		t.Fatalf("credit note: %v", err)
	}
	c, err := svc.Get(ctx, cn, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Supplier.Address != supplier.Address {
		t.Fatalf("credit note supplier address %q", c.Supplier.Address)
	}
}

// Every invoice is reachable through the pages, newest first.
func TestListPageReachesOlderInvoices(t *testing.T) {
	pool := dbPool(t)
	ctx := context.Background()
	svc := NewService(pool, nil, false)
	var buyer uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users (phone) VALUES ($1) RETURNING id`,
		fmt.Sprintf("+97158%07d", rand.IntN(10_000_000))).Scan(&buyer); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		issueTestInvoice(t, pool, svc, buyer, Party{Name: "Kitchen", TRN: "100000000000003"})
	}
	first, next, err := svc.ListPage(ctx, buyer, nil, 2)
	if err != nil || len(first) != 2 || next == nil {
		t.Fatalf("first page: %d items, next %v, %v", len(first), next, err)
	}
	rest, next, err := svc.ListPage(ctx, buyer, next, 2)
	if err != nil || len(rest) != 1 || next != nil {
		t.Fatalf("second page: %d items, next %v, %v", len(rest), next, err)
	}
	if rest[0].ID == first[0].ID || rest[0].ID == first[1].ID {
		t.Fatal("pages overlap")
	}
}
