package console

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testPool(t *testing.T) *pgxpool.Pool {
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

// The portal picks "today" in the branch's zone, so /me/branches must carry it.
func TestBranchesCarryTimezone(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	n := rand.IntN(10_000_000)

	var user, vendor, branch uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users (phone) VALUES ($1) RETURNING id`,
		fmt.Sprintf("+97155%07d", n)).Scan(&user); err != nil {
		t.Fatalf("user: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO vendors (slug, legal_name, trade_name, status) VALUES ($1, 'TZ Kitchen LLC', 'TZ Kitchen', 'active') RETURNING id`,
		fmt.Sprintf("tz-kitchen-%d", n)).Scan(&vendor); err != nil {
		t.Fatalf("vendor: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM vendors WHERE id = $1`, vendor)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, user)
	})
	if err := pool.QueryRow(ctx,
		`INSERT INTO vendor_branches (vendor_id, name, timezone, point)
		 VALUES ($1, 'Riyadh', 'Asia/Riyadh', ST_GeogFromText('POINT(46.67 24.71)')) RETURNING id`,
		vendor).Scan(&branch); err != nil {
		t.Fatalf("branch: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO vendor_users (vendor_id, user_id, role) VALUES ($1, $2, 'staff')`, vendor, user); err != nil {
		t.Fatalf("membership: %v", err)
	}

	got, err := NewService(pool).Branches(ctx, user)
	if err != nil {
		t.Fatalf("branches: %v", err)
	}
	if len(got) != 1 || got[0].BranchID != branch {
		t.Fatalf("branches = %+v, want the one branch", got)
	}
	if got[0].Timezone != "Asia/Riyadh" || got[0].Role != "staff" {
		t.Fatalf("timezone/role = %q/%q, want Asia/Riyadh/staff", got[0].Timezone, got[0].Role)
	}
}
