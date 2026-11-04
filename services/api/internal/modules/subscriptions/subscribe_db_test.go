package subscriptions

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

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/seam/servicetype"
	"github.com/marketplace/api/internal/verticals/meals"
)

// These run against a migrated database (CI sets DATABASE_URL) and skip
// otherwise.

type subFixture struct {
	svc       *Service
	pool      *pgxpool.Pool
	userID    uuid.UUID
	planID    uuid.UUID
	addressID uuid.UUID
	windowID  uuid.UUID
}

func newSubFixture(t *testing.T, cutoffHours int) subFixture {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	reg := servicetype.NewRegistry()
	meals.Register(reg)

	f := subFixture{svc: NewService(pool, reg, Config{}), pool: pool,
		userID: uuid.New(), planID: uuid.New(), addressID: uuid.New(), windowID: uuid.New()}
	vendorID, branchID := uuid.New(), uuid.New()
	n := rand.IntN(10_000_000)
	stmts := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users (id, phone) VALUES ($1, $2)`, []any{f.userID, fmt.Sprintf("+97159%07d", n)}},
		{`INSERT INTO vendors (id, slug, legal_name, trade_name) VALUES ($1, $2, 'T LLC', 'T')`,
			[]any{vendorID, fmt.Sprintf("subtest-%d", n)}},
		{`INSERT INTO vendor_branches (id, vendor_id, name, point, service_area) VALUES ($1, $2, 'T',
			ST_SetSRID(ST_MakePoint(55.23, 25.14), 4326)::geography,
			ST_Multi(ST_Buffer(ST_SetSRID(ST_MakePoint(55.23, 25.14), 4326)::geography, 15000)::geometry)::geography)`,
			[]any{branchID, vendorID}},
		{`INSERT INTO time_windows (id, branch_id, label, starts_at, ends_at) VALUES ($1, $2, 'Lunch', '12:00', '14:00')`,
			[]any{f.windowID, branchID}},
		{`INSERT INTO subscription_plans (id, branch_id, service_type, name, cycle_days, days_per_week,
			cutoff_hours, price_minor, delivery_weekdays)
			VALUES ($1, $2, 'meals_subscription', 'T plan', 7, 7, $3, 70000, '{0,1,2,3,4,5,6}')`,
			[]any{f.planID, branchID, cutoffHours}},
		{`INSERT INTO addresses (id, user_id, label, city, point) VALUES ($1, $2, 'Home', 'Dubai',
			ST_SetSRID(ST_MakePoint(55.23, 25.14), 4326)::geography)`, []any{f.addressID, f.userID}},
	}
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("fixture: %v\n%s", err, s.sql)
		}
	}
	return f
}

func (f subFixture) input(start time.Time) SubscribeInput {
	rules := make([]Rule, 0, 7)
	for d := 0; d < 7; d++ {
		rules = append(rules, Rule{Weekday: Weekday(d), Slot: "lunch", WindowID: f.windowID})
	}
	return SubscribeInput{UserID: f.userID, PlanID: f.planID, AddressID: f.addressID, StartDate: start, Rules: rules}
}

func fieldCode(err error, field string) string {
	var v *httpx.ValidationError
	if errors.As(err, &v) {
		for _, fe := range v.Fields {
			if fe.Field == field {
				return fe.Code
			}
		}
	}
	return ""
}

func TestSubscribeRetryReusesPendingSubscription(t *testing.T) {
	f := newSubFixture(t, 20)
	ctx := context.Background()
	dubai, _ := time.LoadLocation("Asia/Dubai")
	start := EarliestStart(20, dubai, time.Now()).AddDate(0, 0, 1)

	first, err := f.svc.Subscribe(ctx, f.input(start))
	if err != nil {
		t.Fatalf("first subscribe: %v", err)
	}
	second, err := f.svc.Subscribe(ctx, f.input(start))
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if second.ID != first.ID || second.OrderID != first.OrderID {
		t.Fatalf("retry opened a second subscription: %s/%s vs %s/%s", second.ID, second.OrderID, first.ID, first.OrderID)
	}
	var n int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM subscriptions WHERE user_id = $1`, f.userID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want 1 subscription, got %d", n)
	}

	// A different start date is a different checkout.
	third, err := f.svc.Subscribe(ctx, f.input(start.AddDate(0, 0, 1)))
	if err != nil {
		t.Fatalf("other start: %v", err)
	}
	if third.ID == first.ID {
		t.Fatal("a different start date must not reuse the pending subscription")
	}
}

func TestSubscribeRefusesStartPastCutoff(t *testing.T) {
	f := newSubFixture(t, 48)
	ctx := context.Background()
	dubai, _ := time.LoadLocation("Asia/Dubai")
	earliest := EarliestStart(48, dubai, time.Now())

	_, err := f.svc.Subscribe(ctx, f.input(earliest.AddDate(0, 0, -1)))
	if got := fieldCode(err, "start_date"); got != "too_soon" {
		t.Fatalf("day before earliest: want too_soon, got %q (%v)", got, err)
	}
	_, err = f.svc.PreviewSubscription(ctx, f.input(earliest.AddDate(0, 0, -1)))
	if got := fieldCode(err, "start_date"); got != "too_soon" {
		t.Fatalf("preview: want too_soon, got %q (%v)", got, err)
	}
	if _, err := f.svc.Subscribe(ctx, f.input(earliest)); err != nil {
		t.Fatalf("earliest start refused: %v", err)
	}
}
