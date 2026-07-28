package metrics

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The metrics this system actually carries.
//
// Twelve of them, and the list is short on purpose: every one exists because
// someone would page on it or would look at it first during an incident. A
// metric nobody would act on is a metric nobody maintains, and the dashboard it
// sits on becomes noise the third time it is opened.
//
// The route label is the chi *pattern*, never the path. `/v1/orders/{orderId}`
// is one series; `/v1/orders/9f3c…` is one series per order, and a metrics
// backend does not survive that.

var (
	// RED: rate, errors, duration. The three numbers that answer "is the API
	// working" without knowing anything about the product.
	HTTPRequests = Default.Counter(
		"http_requests_total",
		"HTTP requests by route, method and status class.",
		"route", "method", "status")

	HTTPDuration = Default.Histogram(
		"http_request_duration_seconds",
		"Request latency by route.",
		DefaultBuckets, "route")

	// Capacity contention. PLAN.md §8 names this as a top risk and
	// PLAN-VIDEO-FIRST §8 Phase 10 calls for a load test of it specifically.
	// A rejection is not an error — it is the bucket model working — so it is
	// counted separately from HTTP 5xx and watched as a *rate*: a step change
	// means either a launch spike or a branch whose capacity is set wrong, and
	// those need opposite responses.
	CapacityRejections = Default.Counter(
		"capacity_rejections_total",
		"Reservation attempts refused because a bucket was full.",
		"reason")

	// The feed read path, PLAN-VIDEO-FIRST §3's separate service boundary.
	// Separated from the general HTTP histogram because it is the one endpoint
	// whose latency is the product rather than a quality of it.
	FeedLatency = Default.Histogram(
		"feed_page_duration_seconds",
		"Time to build one feed page, excluding transport.",
		[]float64{0.01, 0.025, 0.05, 0.1, 0.15, 0.25, 0.5, 1}, "source")

	// An empty page is not an error and is the single most important product
	// signal in this system: it means a viewer at that location had nothing to
	// watch. §4 calls content supply the existential risk, and this is the
	// number that measures it in production rather than in a plan.
	FeedEmptyPages = Default.Counter(
		"feed_empty_pages_total",
		"Feed requests that returned no items.",
		"authenticated")

	// Moderation queue depth and age. The SLA is two hours (PHASE-2); a queue
	// that is deep and *young* is a busy day, one that is shallow and *old* is
	// a queue nobody is working. Only the second is an incident, which is why
	// both are exported.
	ModerationQueueDepth = Default.Counter(
		"moderation_queue_items_total",
		"Items entering a moderation queue.",
		"queue")

	// Money. Posting failures are the one class of error where retrying without
	// understanding is dangerous, so they are counted separately from every
	// other 500 in the process.
	LedgerPostings = Default.Counter(
		"ledger_postings_total",
		"Journals posted, by event type and outcome.",
		"event", "outcome")

	// Notifications. `suppressed` is deliberately a first-class outcome rather
	// than folded into failures: a rising suppression rate is a consent or
	// template problem and a rising failure rate is a provider problem, and
	// they are fixed by different people.
	Notifications = Default.Counter(
		"notifications_total",
		"Notification dispatch outcomes.",
		"channel", "outcome")

	// Boost delivery. `expired` rising means clients are not confirming
	// impressions, which silently starves campaigns — see PHASE-9 §3.3.
	BoostDeliveries = Default.Counter(
		"boost_deliveries_total",
		"Sponsored impressions by state.",
		"state")

	// Background work. A job loop that stops is the failure mode nothing else
	// here would show: the API stays green, the dashboards stay flat, and
	// routes go unplanned until a customer calls.
	JobRuns = Default.Counter(
		"job_runs_total",
		"Background job executions.",
		"job", "outcome")
)

// Middleware records RED metrics for every request.
//
// Placed after chi's routing so `chi.RouteContext(r).RoutePattern()` is
// populated — before it, every request labels as the empty pattern and the
// metric is one enormous series.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			// An unmatched path. Bucketed under a literal rather than the URL,
			// because a 404 scan is exactly the traffic that would blow up
			// cardinality.
			route = "unmatched"
		}

		// Status *class*, not code. The difference between 401 and 403 does not
		// change what an on-call engineer does, and it triples the series count
		// to record it.
		HTTPRequests.With(route, r.Method, statusClass(rec.status)).Inc()
		HTTPDuration.With(route).Observe(time.Since(start))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status  int
	written bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.written {
		r.status = code
		r.written = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.written = true
	return r.ResponseWriter.Write(b)
}

func statusClass(code int) string {
	switch {
	case code < 200:
		return "1xx"
	case code < 300:
		return "2xx"
	case code < 400:
		return "3xx"
	case code < 500:
		return "4xx"
	default:
		return "5xx"
	}
}

// Handler serves the exposition endpoint.
//
// Not mounted under /v1. It is scraped by infrastructure rather than called by
// clients, it has no versioning contract, and putting it behind the API's auth
// and CORS would mean a scraper needs a token — which is how monitoring stops
// working after a credential rotation nobody connected to it.
func Handler(r *Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(r.Render()))
	}
}

// BindPool exports connection-pool gauges.
//
// Pool saturation is the failure mode that presents as "the API is slow" and is
// actually "every request is waiting for a connection". Without these the
// investigation starts at the database, which will look idle — because it is.
func BindPool(reg *Registry, pool *pgxpool.Pool, name string) {
	reg.Gauge("db_pool_acquired_connections",
		"Connections currently checked out of the pool.",
		func() float64 { return float64(pool.Stat().AcquiredConns()) })

	reg.Gauge("db_pool_idle_connections",
		"Connections currently idle.",
		func() float64 { return float64(pool.Stat().IdleConns()) })

	reg.Gauge("db_pool_max_connections",
		"Configured pool ceiling.",
		func() float64 { return float64(pool.Stat().MaxConns()) })

	// The one that actually pages. A non-zero and rising count means requests
	// are queueing for connections, which is invisible in every other metric
	// here — the database is fine, the handlers are fine, and everything is
	// slow.
	reg.Gauge("db_pool_acquire_wait_seconds_total",
		"Cumulative time spent waiting for a connection.",
		func() float64 { return pool.Stat().AcquireDuration().Seconds() })

	reg.Gauge("db_pool_canceled_acquires_total",
		"Acquires abandoned because the caller's context expired.",
		func() float64 { return float64(pool.Stat().CanceledAcquireCount()) })
}

// TimeJob records a background job's outcome and duration.
//
// Wraps rather than being called at each return point, because a job that
// returns early on an error is exactly the one whose metric gets forgotten.
func TimeJob(ctx context.Context, name string, fn func(context.Context) error) error {
	err := fn(ctx)
	outcome := "ok"
	if err != nil {
		outcome = "error"
	}
	JobRuns.With(name, outcome).Inc()
	return err
}

// ObserveFeed records one page build.
func ObserveFeed(source string, d time.Duration, items int, authenticated bool) {
	FeedLatency.With(source).Observe(d)
	if items == 0 {
		FeedEmptyPages.With(strconv.FormatBool(authenticated)).Inc()
	}
}
