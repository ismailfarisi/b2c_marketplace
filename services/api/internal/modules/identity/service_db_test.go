package identity

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/auth"
	"github.com/marketplace/api/internal/httpx"
)

// These run against a migrated database (CI sets DATABASE_URL) and skip
// otherwise. They cover writes that must survive the error they come with:
// postgres.InTx rolls back on any returned error.

func testService(t *testing.T) (*Service, *pgxpool.Pool) {
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
	issuer := auth.NewIssuer("test-signing-key-0123456789abcdef", 15*time.Minute, 24*time.Hour)
	return NewService(pool, issuer, 5*time.Minute, false), pool
}

func testPhone() string { return fmt.Sprintf("+97158%07d", rand.IntN(10_000_000)) }

func newChallenge(t *testing.T, pool *pgxpool.Pool, phone, code string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO otp_codes (phone, purpose, code_hash, expires_at)
		VALUES ($1, 'login', $2, now() + interval '5 minutes') RETURNING id`,
		phone, auth.HashToken(code)).Scan(&id); err != nil {
		t.Fatalf("insert otp: %v", err)
	}
	return id
}

func problemCode(err error) string {
	var c *httpx.CodedError
	if errors.As(err, &c) {
		return c.Code
	}
	return ""
}

func TestVerifyOTPCountsWrongAttempts(t *testing.T) {
	svc, pool := testService(t)
	ctx := context.Background()
	challenge := newChallenge(t, pool, testPhone(), "123456")

	for i := 1; i <= 5; i++ {
		_, err := svc.VerifyOTP(ctx, challenge, "000000", DeviceInfo{Platform: "web"})
		if !errors.Is(err, httpx.ErrUnauthorized) {
			t.Fatalf("attempt %d: want 401, got %v", i, err)
		}
		want := CodeWrongCode
		if i == 5 {
			want = CodeTooManyAttempts
		}
		if got := problemCode(err); got != want {
			t.Fatalf("attempt %d: want code %s, got %q (%v)", i, want, got, err)
		}
	}

	var attempts int
	if err := pool.QueryRow(ctx, `SELECT attempts FROM otp_codes WHERE id = $1`, challenge).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 5 {
		t.Fatalf("attempts = %d, want 5", attempts)
	}

	// The right code no longer works on an exhausted challenge.
	_, err := svc.VerifyOTP(ctx, challenge, "123456", DeviceInfo{Platform: "web"})
	if got := problemCode(err); got != CodeTooManyAttempts {
		t.Fatalf("right code after lockout: want %s, got %q (%v)", CodeTooManyAttempts, got, err)
	}
}

func TestRefreshReuseRevokesFamily(t *testing.T) {
	svc, pool := testService(t)
	ctx := context.Background()
	challenge := newChallenge(t, pool, testPhone(), "654321")

	first, err := svc.VerifyOTP(ctx, challenge, "654321", DeviceInfo{Platform: "web"})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	second, err := svc.Refresh(ctx, first.RefreshToken, DeviceInfo{Platform: "web"})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}

	if _, err := svc.Refresh(ctx, first.RefreshToken, DeviceInfo{Platform: "web"}); !errors.Is(err, httpx.ErrUnauthorized) {
		t.Fatalf("replay: want 401, got %v", err)
	}
	if _, err := svc.Refresh(ctx, second.RefreshToken, DeviceInfo{Platform: "web"}); !errors.Is(err, httpx.ErrUnauthorized) {
		t.Fatalf("newest token after replay: want 401, got %v", err)
	}
}

func TestRequestOTPRateLimitIs429WithRetryAfter(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()
	phone := testPhone()

	for i := 0; i < otpPerPhone; i++ {
		if _, err := svc.RequestOTP(ctx, phone, "login", ""); err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
	}
	_, err := svc.RequestOTP(ctx, phone, "login", "")
	if !errors.Is(err, httpx.ErrRateLimited) {
		t.Fatalf("6th request: want ErrRateLimited, got %v", err)
	}
	if got := problemCode(err); got != CodeRateLimited {
		t.Fatalf("code = %q, want %s", got, CodeRateLimited)
	}
	var ra *httpx.RetryAfter
	if !errors.As(err, &ra) || ra.After <= 0 || ra.After > otpWindow {
		t.Fatalf("want a Retry-After within the window, got %+v", ra)
	}
}

func TestRequestOTPAcceptsUAENationalFormat(t *testing.T) {
	svc, pool := testService(t)
	ctx := context.Background()
	national := fmt.Sprintf("058%07d", rand.IntN(10_000_000))
	ch, err := svc.RequestOTP(ctx, national, "login", "")
	if err != nil {
		t.Fatalf("RequestOTP(%s): %v", national, err)
	}
	var stored string
	if err := pool.QueryRow(ctx, `SELECT phone FROM otp_codes WHERE id = $1`, ch.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "+971"+national[1:] {
		t.Fatalf("stored phone = %q", stored)
	}
}
