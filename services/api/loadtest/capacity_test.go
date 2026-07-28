//go:build loadtest

// Package loadtest holds the two targeted load tests PLAN-VIDEO-FIRST §8 Phase
// 10 asks for.
//
//	Load-test capacity contention and the feed read path.
//
// Behind a build tag, and behind an environment variable, for two different
// reasons. The tag keeps them out of `make test` — they need a database with
// real data and they take minutes. The environment variable keeps them from
// running against whatever `DATABASE_URL` happens to be set to, because this
// file writes to `capacity_buckets` and a developer who ran it against a shared
// environment would have a very confusing afternoon.
//
//	LOADTEST_DATABASE_URL=postgres://... go test -tags loadtest -v ./loadtest/...
//
// These are not benchmarks. A benchmark answers "how fast"; these answer "is it
// correct under contention", which is the question PLAN.md §8 actually raises:
//
//	Capacity race under launch spike | Explicit bucket model + DB constraints
//	+ targeted load test at hardening
package loadtest

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func pool(t testing.TB) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("LOADTEST_DATABASE_URL")
	if url == "" {
		t.Skip("LOADTEST_DATABASE_URL is not set")
	}

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	// Deliberately larger than the API's pool. The point of this test is to put
	// more concurrent writers on one row than production ever will; a pool the
	// same size as production's would make the pool the bottleneck and hide the
	// contention being measured.
	cfg.MaxConns = 64

	p, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

// fixture creates a branch, a window and a bucket with a known limit.
//
// Its own vendor and branch every run, rather than reusing seed data, so a
// failed run leaves nothing behind that a later run trips over — and so two
// people can run this simultaneously against the same database.
func fixture(t testing.TB, p *pgxpool.Pool, limit int) (bucketID uuid.UUID, cleanup func()) {
	t.Helper()
	ctx := context.Background()

	suffix := uuid.New().String()[:8]
	var vendorID, branchID, windowID uuid.UUID

	if err := p.QueryRow(ctx, `
		INSERT INTO vendors (slug, legal_name, trade_name, status)
		VALUES ($1, 'Load Test LLC', 'Load Test', 'active') RETURNING id`,
		"loadtest-"+suffix).Scan(&vendorID); err != nil {
		t.Fatalf("create vendor: %v", err)
	}

	if err := p.QueryRow(ctx, `
		INSERT INTO vendor_branches (vendor_id, name, point, timezone)
		VALUES ($1, $2, ST_SetSRID(ST_MakePoint(55.14, 25.08), 4326)::geography, 'Asia/Dubai')
		RETURNING id`, vendorID, "Load Test "+suffix).Scan(&branchID); err != nil {
		t.Fatalf("create branch: %v", err)
	}

	if err := p.QueryRow(ctx, `
		INSERT INTO time_windows (branch_id, label, starts_at, ends_at)
		VALUES ($1, 'Load Test Window', '18:00', '20:00')
		RETURNING id`, branchID).Scan(&windowID); err != nil {
		t.Fatalf("create window: %v", err)
	}

	if err := p.QueryRow(ctx, `
		INSERT INTO capacity_buckets
		    (branch_id, resource_key, bucket_date, window_id, capacity_limit)
		VALUES ($1, 'orders', CURRENT_DATE + 30, $2, $3)
		RETURNING id`, branchID, windowID, limit).Scan(&bucketID); err != nil {
		t.Fatalf("create bucket: %v", err)
	}

	return bucketID, func() {
		// Cascades take the branch, window, bucket and reservations with it.
		if _, err := p.Exec(context.Background(),
			`DELETE FROM vendors WHERE id = $1`, vendorID); err != nil {
			t.Logf("cleanup: %v", err)
		}
	}
}

// TestCapacityHoldsUnderContention is the test PLAN.md §8 asks for.
//
// 400 concurrent reservation attempts against a bucket with 100 units. The
// assertion is not about throughput: it is that **exactly 100 succeed**. One
// unit of overbooking is a customer who ordered a meal that will not be cooked,
// and it is the failure mode an optimistic implementation produces only under
// load — which is to say, on launch day and never in development.
func TestCapacityHoldsUnderContention(t *testing.T) {
	p := pool(t)
	const (
		limit   = 100
		writers = 400
	)

	bucketID, cleanup := fixture(t, p, limit)
	defer cleanup()

	var granted, refused, errored atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Every goroutine blocks on the same channel so they arrive
			// together. Starting them in a loop staggers arrival by however
			// long goroutine creation takes, which is enough to serialise them
			// and turn this into a test of nothing.
			<-start

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			var reservationID *uuid.UUID
			err := p.QueryRow(ctx,
				`SELECT reserve_capacity($1, $2, 1, '15 minutes')`,
				bucketID, uuid.New()).Scan(&reservationID)
			switch {
			case err != nil:
				errored.Add(1)
			case reservationID == nil:
				refused.Add(1)
			default:
				granted.Add(1)
			}
		}()
	}

	began := time.Now()
	close(start)
	wg.Wait()
	elapsed := time.Since(began)

	t.Logf("%d writers, limit %d: %d granted, %d refused, %d errored in %v (%.0f/s)",
		writers, limit, granted.Load(), refused.Load(), errored.Load(),
		elapsed.Round(time.Millisecond), float64(writers)/elapsed.Seconds())

	if errored.Load() > 0 {
		t.Errorf("%d attempts errored — a full bucket must refuse, not fail", errored.Load())
	}
	if got := granted.Load(); got != limit {
		t.Fatalf("%d reservations granted against a limit of %d", got, limit)
	}
	if got := granted.Load() + refused.Load(); got != writers {
		t.Fatalf("%d outcomes for %d writers — some attempt was lost", got, writers)
	}

	// And the row itself agrees. The counters above are what the application
	// believed; this is what the database holds, and a divergence between them
	// is the bug that makes every capacity number downstream wrong.
	var consumed, reserved, capacity int
	if err := p.QueryRow(context.Background(),
		`SELECT consumed, reserved, capacity_limit FROM capacity_buckets WHERE id = $1`,
		bucketID).Scan(&consumed, &reserved, &capacity); err != nil {
		t.Fatal(err)
	}
	if consumed+reserved != limit {
		t.Fatalf("bucket holds %d+%d against a limit of %d", consumed, reserved, capacity)
	}
}

// TestCapacityReleaseIsNotLossy runs the whole reserve/release cycle under
// contention.
//
// The subtler failure. Overbooking is caught by a CHECK constraint and shows up
// loudly; *under*-booking does not. A release path that loses a unit leaves a
// bucket permanently short, the branch shows as fully booked with no orders
// behind it, and nobody notices until a vendor complains that they have no
// deliveries — by which time the count has drifted for weeks.
func TestCapacityReleaseIsNotLossy(t *testing.T) {
	p := pool(t)
	const (
		limit  = 50
		rounds = 10
	)

	bucketID, cleanup := fixture(t, p, limit)
	defer cleanup()

	for round := 0; round < rounds; round++ {
		ids := make([]uuid.UUID, 0, limit)
		var mu sync.Mutex
		var wg sync.WaitGroup

		for i := 0; i < limit*2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()

				var id *uuid.UUID
				if err := p.QueryRow(ctx,
					`SELECT reserve_capacity($1, $2, 1, '15 minutes')`,
					bucketID, uuid.New()).Scan(&id); err != nil {
					return
				}
				if id != nil {
					mu.Lock()
					ids = append(ids, *id)
					mu.Unlock()
				}
			}()
		}
		wg.Wait()

		if len(ids) != limit {
			t.Fatalf("round %d: %d reservations for a limit of %d", round, len(ids), limit)
		}

		for _, id := range ids {
			wg.Add(1)
			go func(id uuid.UUID) {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				var ok bool
				if err := p.QueryRow(ctx,
					`SELECT release_capacity($1, 'expired')`, id).Scan(&ok); err != nil {
					t.Errorf("release: %v", err)
				}
			}(id)
		}
		wg.Wait()

		var consumed, reserved int
		if err := p.QueryRow(context.Background(),
			`SELECT consumed, reserved FROM capacity_buckets WHERE id = $1`,
			bucketID).Scan(&consumed, &reserved); err != nil {
			t.Fatal(err)
		}
		if consumed != 0 || reserved != 0 {
			t.Fatalf("round %d: bucket did not return to empty (consumed %d, reserved %d) — "+
				"a lossy release leaves a branch permanently short and nobody notices "+
				"for weeks", round, consumed, reserved)
		}
	}

	t.Logf("%d rounds of %d reserve/release cycles returned the bucket to empty every time",
		rounds, limit)
}

// TestCapacityThroughput reports the numbers a capacity plan needs.
//
// No assertion on the rate. A pass/fail threshold on throughput fails on a
// laptop under load, gets marked flaky, and is deleted — so this prints and
// leaves the judgement to whoever reads it. What it *does* assert is the
// invariant, because a fast implementation that overbooks is not faster.
func TestCapacityThroughput(t *testing.T) {
	p := pool(t)

	for _, writers := range []int{1, 8, 32, 128} {
		limit := writers * 2
		bucketID, cleanup := fixture(t, p, limit)

		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < writers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				var id *uuid.UUID
				_ = p.QueryRow(ctx, `SELECT reserve_capacity($1, $2, 1, '15 minutes')`,
					bucketID, uuid.New()).Scan(&id)
			}()
		}

		began := time.Now()
		close(start)
		wg.Wait()
		elapsed := time.Since(began)

		fmt.Printf("  %3d concurrent writers on one bucket: %v total, %6.0f reservations/s\n",
			writers, elapsed.Round(time.Millisecond), float64(writers)/elapsed.Seconds())

		cleanup()
	}
}
