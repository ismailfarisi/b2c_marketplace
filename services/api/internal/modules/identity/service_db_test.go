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
		if _, err := svc.RequestOTP(ctx, phone, "", "login", ""); err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
	}
	_, err := svc.RequestOTP(ctx, phone, "", "login", "")
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

func TestRequestOTPNationalNumberFollowsTheCountry(t *testing.T) {
	svc, pool := testService(t)
	ctx := context.Background()
	national := fmt.Sprintf("055%07d", rand.IntN(10_000_000))
	stored := func(id uuid.UUID) string {
		var p string
		if err := pool.QueryRow(ctx, `SELECT phone FROM otp_codes WHERE id = $1`, id).Scan(&p); err != nil {
			t.Fatal(err)
		}
		return p
	}

	if _, err := svc.RequestOTP(ctx, national, "", "login", ""); err == nil {
		t.Fatalf("a national number with no country must be refused, not guessed")
	} else {
		var ve *httpx.ValidationError
		if !errors.As(err, &ve) || ve.Fields[0].Code != PhoneCountryRequired {
			t.Fatalf("want 422 %s, got %v", PhoneCountryRequired, err)
		}
	}
	sa, err := svc.RequestOTP(ctx, national, "SA", "login", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := stored(sa.ID); got != "+966"+national[1:] {
		t.Fatalf("SA stored phone = %q", got)
	}
	ae, err := svc.RequestOTP(ctx, national, "AE", "login", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := stored(ae.ID); got != "+971"+national[1:] {
		t.Fatalf("AE stored phone = %q", got)
	}
}

func TestResendRetiresEarlierCodes(t *testing.T) {
	svc, pool := testService(t)
	ctx := context.Background()
	phone := testPhone()
	first := newChallenge(t, pool, phone, "111111")

	second, err := svc.RequestOTP(ctx, phone, "", "login", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.VerifyOTP(ctx, first, "111111", DeviceInfo{})
	if got := problemCode(err); got != CodeChallengeSuperseded {
		t.Fatalf("earlier code after a resend: want %s, got %v", CodeChallengeSuperseded, err)
	}
	var superseded *time.Time
	if err := pool.QueryRow(ctx, `SELECT superseded_at FROM otp_codes WHERE id = $1`, second.ID).Scan(&superseded); err != nil {
		t.Fatal(err)
	}
	if superseded != nil {
		t.Fatal("the newest challenge must stay live")
	}
}

func TestRefreshKeepsTheDevice(t *testing.T) {
	svc, pool := testService(t)
	ctx := context.Background()
	phone := testPhone()
	ch := newChallenge(t, pool, phone, "222222")
	first, err := svc.VerifyOTP(ctx, ch, "222222", DeviceInfo{Platform: "ios", Name: "Layla's iPhone"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Refresh(ctx, first.RefreshToken, DeviceInfo{IP: "192.0.2.7"})
	if err != nil {
		t.Fatal(err)
	}
	var platform, name *string
	if err := pool.QueryRow(ctx, `SELECT device_platform, device_name FROM sessions WHERE id = $1`,
		second.sessionID).Scan(&platform, &name); err != nil {
		t.Fatal(err)
	}
	if platform == nil || *platform != "ios" || name == nil || *name != "Layla's iPhone" {
		t.Fatalf("rotated session lost its device: platform=%v name=%v", platform, name)
	}
}
