//go:build loadtest

package loadtest

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The feed read path.
//
// PLAN-VIDEO-FIRST §8 Phase 10:
//
//	Load-test capacity contention **and the feed read path**. […] the
//	video-first plan adds a second contention point the services plan never
//	had: the feed read path under a Ramadan-scale signup spike.
//
// The two are different kinds of test and it is worth saying why. Capacity is a
// *correctness* test — the answer must be exactly 100 — and it needs
// concurrency to produce the failure. The feed is a *scaling* test: it is
// correct with ten reels and with a hundred thousand, and what changes is
// whether the query plan is still an index scan.
//
// So the assertion here is on the plan, not on the clock. A latency threshold
// on a laptop fails under a background build, gets marked flaky, and is deleted
// within a month. "The planner chose a sequential scan over reels" is a fact
// that means the same thing on every machine.

const seedReels = 20_000

// seedFeed creates a branch with `seedReels` published reels covering Dubai.
//
// Twenty thousand rather than a few hundred, because the whole question is what
// the planner does when the table stops fitting in a page or two. At three
// hundred rows Postgres sequentially scans everything regardless of the indexes,
// and a test at that size measures nothing and asserts nothing.
func seedFeed(t testing.TB, p *pgxpool.Pool) (cleanup func()) {
	t.Helper()
	ctx := context.Background()

	suffix := uuid.New().String()[:8]
	var vendorID, branchID uuid.UUID

	if err := p.QueryRow(ctx, `
		INSERT INTO vendors (slug, legal_name, trade_name, status)
		VALUES ($1, 'Feed Load Test LLC', 'Feed Load Test', 'active') RETURNING id`,
		"feedload-"+suffix).Scan(&vendorID); err != nil {
		t.Fatalf("create vendor: %v", err)
	}

	if err := p.QueryRow(ctx, `
		INSERT INTO vendor_branches (vendor_id, name, point, timezone)
		VALUES ($1, $2, ST_SetSRID(ST_MakePoint(55.14, 25.08), 4326)::geography, 'Asia/Dubai')
		RETURNING id`, vendorID, "Feed Load "+suffix).Scan(&branchID); err != nil {
		t.Fatalf("create branch: %v", err)
	}

	// One statement rather than 20,000 round trips. generate_series scatters
	// the reels across a ~20 km box around Dubai Marina and across the last 30
	// days, so the recency window in the real query actually excludes some of
	// them — a seed where every row qualifies tests a filter that never fires.
	started := time.Now()
	if _, err := p.Exec(ctx, `
		INSERT INTO reels (
		    author_type, author_id, state, visibility, caption, duration_ms,
		    poster_key, published_at, coverage, coverage_kind, coverage_resolved_at,
		    origin_point)
		SELECT 'vendor_branch', $1, 'published', 'public',
		       'load test reel ' || i, 15000,
		       'posters/load-' || i || '.jpg',
		       now() - (random() * interval '30 days'),
		       ST_Buffer(
		           ST_SetSRID(ST_MakePoint(55.14 + (random() - 0.5) * 0.2,
		                                   25.08 + (random() - 0.5) * 0.2), 4326)::geography,
		           5000),
		       'radius', now(),
		       ST_SetSRID(ST_MakePoint(55.14 + (random() - 0.5) * 0.2,
		                               25.08 + (random() - 0.5) * 0.2), 4326)::geography
		  FROM generate_series(1, $2) AS i`, branchID, seedReels); err != nil {
		t.Fatalf("seed reels: %v", err)
	}

	// Without this the planner is working from statistics that predate the
	// insert, and the plan being asserted is not the plan production would get.
	// Forgetting it is the most common way a load test produces a reassuring
	// and meaningless result.
	if _, err := p.Exec(ctx, `ANALYZE reels`); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	t.Logf("seeded %d reels in %v", seedReels, time.Since(started).Round(time.Millisecond))

	return func() {
		if _, err := p.Exec(context.Background(),
			`DELETE FROM vendors WHERE id = $1`, vendorID); err != nil {
			t.Logf("cleanup: %v", err)
		}
	}
}

// eligibleQuery is the hard-filter half of feed.Service.retrieve.
//
// The geo predicate, the moderation state and the recency window — the part
// that scans. Ranking is arithmetic over a bounded candidate set and does not
// change shape with the table size; this does.
const eligibleQuery = `
	SELECT r.id
	  FROM reels r
	 WHERE r.state = 'published'
	   AND r.visibility = 'public'
	   AND r.removed_at IS NULL
	   AND r.published_at > now() - interval '30 days'
	   AND ST_Intersects(r.coverage, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography)
	 ORDER BY r.published_at DESC
	 LIMIT 200`

// TestFeedUsesAnIndexAtScale is the assertion that survives being run anywhere.
//
// A sequential scan over `reels` is the regression this exists to catch: it is
// invisible in development, it is invisible in staging with a thousand rows, and
// it is the difference between a feed that serves in 40 ms and one that serves
// in four seconds on the day the content operator's work lands.
func TestFeedUsesAnIndexAtScale(t *testing.T) {
	p := pool(t)
	cleanup := seedFeed(t, p)
	defer cleanup()

	rows, err := p.Query(context.Background(),
		"EXPLAIN (ANALYZE, BUFFERS) "+eligibleQuery, 55.14, 25.08)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()

	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteString("\n")
	}
	t.Logf("plan:\n%s", plan.String())

	if strings.Contains(plan.String(), "Seq Scan on reels") {
		t.Errorf("the planner chose a sequential scan over reels at %d rows.\n\n"+
			"This is invisible in development and is the difference between a feed "+
			"that serves in 40ms and one that serves in four seconds on the day the "+
			"content operator's work lands. Check that the GiST index on "+
			"(coverage, published_at) still exists and that its predicate still "+
			"matches this query's filters.", seedReels)
	}
}

// TestFeedReadPathUnderConcurrency reports the distribution.
//
// Prints rather than asserts, for the reason in the file header: a latency
// threshold measured on a developer laptop is a flaky test with a deletion date.
// What it produces is the number that goes in a capacity plan, measured against
// a table large enough for the answer to mean something.
func TestFeedReadPathUnderConcurrency(t *testing.T) {
	p := pool(t)
	cleanup := seedFeed(t, p)
	defer cleanup()

	for _, readers := range []int{1, 8, 32} {
		const perReader = 20

		latencies := make([]time.Duration, 0, readers*perReader)
		var mu sync.Mutex
		var wg sync.WaitGroup
		start := make(chan struct{})

		for i := 0; i < readers; i++ {
			wg.Add(1)
			go func(seed int) {
				defer wg.Done()
				<-start

				for j := 0; j < perReader; j++ {
					// A different point per request. One repeated point would
					// be answered from cache after the first, and the number
					// would describe a cache rather than the feed.
					lng := 55.14 + float64(seed%10)*0.005
					lat := 25.08 + float64(j%10)*0.005

					began := time.Now()
					rows, err := p.Query(context.Background(), eligibleQuery, lng, lat)
					if err != nil {
						t.Errorf("query: %v", err)
						return
					}
					for rows.Next() {
						var id uuid.UUID
						_ = rows.Scan(&id)
					}
					rows.Close()

					mu.Lock()
					latencies = append(latencies, time.Since(began))
					mu.Unlock()
				}
			}(i)
		}

		began := time.Now()
		close(start)
		wg.Wait()
		elapsed := time.Since(began)

		sort.Slice(latencies, func(a, b int) bool { return latencies[a] < latencies[b] })
		fmt.Printf("  %2d readers × %d queries over %d reels: "+
			"p50 %v · p95 %v · p99 %v · %.0f queries/s\n",
			readers, perReader, seedReels,
			pct(latencies, 0.50).Round(time.Microsecond*100),
			pct(latencies, 0.95).Round(time.Microsecond*100),
			pct(latencies, 0.99).Round(time.Microsecond*100),
			float64(len(latencies))/elapsed.Seconds())
	}
}

func pct(sorted []time.Duration, q float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	i := int(float64(len(sorted)-1) * q)
	return sorted[i]
}
