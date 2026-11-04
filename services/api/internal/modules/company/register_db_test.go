package company

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

func newUser(t *testing.T, pool *pgxpool.Pool) (uuid.UUID, string) {
	t.Helper()
	var id uuid.UUID
	phone := fmt.Sprintf("+97155%07d", rand.IntN(10_000_000))
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO users (phone) VALUES ($1) RETURNING id`, phone).Scan(&id); err != nil {
		t.Fatalf("user: %v", err)
	}
	return id, phone
}

func registration(legal string) RegisterInput {
	return RegisterInput{
		Name: "Acme", LegalName: legal, BillingEmail: "ap@acme.test",
		Office: OfficeInput{Label: "HQ", Building: "Tower 1", Area: "Business Bay", City: "Dubai", Lat: 25.18, Lng: 55.27},
	}
}

// A second "Send for approval" for the same legal name must not open a twin
// pending company; a different company is still allowed.
func TestRegisterRefusesDuplicate(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	svc := NewService(pool, nil, nil)
	user, _ := newUser(t, pool)
	legal := fmt.Sprintf("Acme Trading %d LLC", rand.IntN(1_000_000))

	if _, err := svc.Register(ctx, user, registration(legal)); err != nil {
		t.Fatalf("first register: %v", err)
	}
	_, err := svc.Register(ctx, user, registration("  "+legal+" "))
	var coded *httpx.CodedError
	if !errors.As(err, &coded) || coded.Code != "company_exists" {
		t.Fatalf("second register = %v, want company_exists", err)
	}
	if _, err := svc.Register(ctx, user, registration(legal+" Two")); err != nil {
		t.Fatalf("other company: %v", err)
	}
}

// An invite typed in UAE local format links to the account signed in with the
// E.164 number.
func TestLocalInviteLinksAccount(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	svc := NewService(pool, nil, nil)
	admin, _ := newUser(t, pool)
	c, err := svc.Register(ctx, admin, registration(fmt.Sprintf("Invite Co %d", rand.IntN(1_000_000))))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE companies SET status = 'active', credit_limit_minor = 100000 WHERE id = $1`, c.ID); err != nil {
		t.Fatalf("approve: %v", err)
	}
	employee, phone := newUser(t, pool)
	local := "0" + phone[4:]
	if err := svc.AddMember(ctx, admin, c.ID, local, "Layla", "employee"); err != nil {
		t.Fatalf("add member %s: %v", local, err)
	}
	mine, err := svc.Benefits(ctx, employee)
	if err != nil {
		t.Fatalf("mine: %v", err)
	}
	if len(mine) != 1 || mine[0].CompanyID != c.ID {
		t.Fatalf("employee companies = %+v, want the inviting company", mine)
	}
}
