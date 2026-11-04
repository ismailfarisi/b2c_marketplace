package admin

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
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

func newUser(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	phone := fmt.Sprintf("+97155%07d", rand.IntN(10_000_000))
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO users (phone) VALUES ($1) RETURNING id`, phone).Scan(&id); err != nil {
		t.Fatalf("user: %v", err)
	}
	return id
}

// A support grant must not unlock ops-only endpoints, and a revoke must take
// the legacy platform_admins row with it so nothing keyed on it survives.
func TestGrantRevokeKeepsLegacyRowHonest(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	svc := NewService(pool)

	actor := newUser(t, pool)
	if _, err := pool.Exec(ctx, `INSERT INTO admin_grants (user_id, role_key, reason) VALUES ($1, 'superadmin', 'test')`, actor); err != nil {
		t.Fatalf("seed superadmin: %v", err)
	}
	staff := newUser(t, pool)

	if err := svc.GrantRole(ctx, actor, staff, "support", "test", nil); err != nil {
		t.Fatalf("grant: %v", err)
	}
	var role string
	if err := pool.QueryRow(ctx, `SELECT role FROM platform_admins WHERE user_id = $1`, staff).Scan(&role); err != nil {
		t.Fatalf("legacy row after grant: %v", err)
	}
	if role != "moderator" {
		t.Fatalf("legacy role for support grant = %q, want moderator (never ops)", role)
	}
	if err := svc.RequireOps(ctx, staff); !errors.Is(err, httpx.ErrForbidden) {
		t.Fatalf("RequireOps for support: want 403, got %v", err)
	}
	if err := svc.RequireStaff(ctx, staff); err != nil {
		t.Fatalf("RequireStaff for support: %v", err)
	}

	if err := svc.RevokeRole(ctx, actor, staff, "support", "test"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM platform_admins WHERE user_id = $1`, staff).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("legacy row survived revoke")
	}
	if err := svc.RequireStaff(ctx, staff); !errors.Is(err, httpx.ErrForbidden) {
		t.Fatalf("RequireStaff after revoke: want 403, got %v", err)
	}
}
