// Package metrics is the Phase 10 observability surface.
//
// PLAN-VIDEO-FIRST §8 Phase 10 asks for "observability, on-call runbooks, launch
// readiness". This is the first half, and it is deliberately built without a
// metrics library.
//
// **Why no Prometheus client.** The dependency is small, well-maintained and
// entirely reasonable — and it is also the thing that makes it easy to add a
// hundred metrics. What this system needs on call is roughly a dozen numbers,
// and every one of them should be there because someone will page on it. A
// hand-rolled registry that speaks the Prometheus text format keeps the question
// "is this metric worth carrying" in front of whoever adds the next one.
// Swapping in the real client later is an implementation change behind these
// types, not a rewrite.
//
// **What is deliberately absent.** No per-vendor labels, no per-reel anything,
// no per-endpoint histogram with ten buckets each. Cardinality is how a metrics
// bill overtakes a database bill, and a label whose values come from user data
// is unbounded by construction. Every label in this package has a closed value
// set that a reader can enumerate.
package metrics

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Registry holds every metric a process exposes.
type Registry struct {
	mu    sync.RWMutex
	order []string
	help  map[string]string

	counters   map[string]*CounterVec
	gauges     map[string]*Gauge
	histograms map[string]*HistogramVec
}

func NewRegistry() *Registry {
	return &Registry{
		help:       map[string]string{},
		counters:   map[string]*CounterVec{},
		gauges:     map[string]*Gauge{},
		histograms: map[string]*HistogramVec{},
	}
}

// Default is the process-wide registry.
//
// A package-level default rather than injection through every constructor,
// because a metric that is awkward to record does not get recorded — and an
// unrecorded metric is a worse outcome than a slightly impure design.
var Default = NewRegistry()

func (r *Registry) register(name, help string) {
	if _, seen := r.help[name]; !seen {
		r.order = append(r.order, name)
	}
	r.help[name] = help
}

// --- counters ---------------------------------------------------------------

type Counter struct{ v atomic.Int64 }

func (c *Counter) Inc()        { c.v.Add(1) }
func (c *Counter) Add(n int64) { c.v.Add(n) }
func (c *Counter) Value() int64 {
	return c.v.Load()
}

// CounterVec is a counter split by labels.
//
// Label values are passed as an ordered slice rather than a map, so the
// cardinality of a metric is visible at the call site. A map invites
// `labels["vendor_id"] = id`, which is how a time-series database falls over.
type CounterVec struct {
	mu       sync.RWMutex
	name     string
	labels   []string
	children map[string]*Counter
}

func (r *Registry) Counter(name, help string, labels ...string) *CounterVec {
	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.counters[name]; ok {
		return existing
	}
	r.register(name, help)
	v := &CounterVec{name: name, labels: labels, children: map[string]*Counter{}}
	r.counters[name] = v
	return v
}

// With returns the child counter for a label set.
func (v *CounterVec) With(values ...string) *Counter {
	key := strings.Join(values, "\x00")

	v.mu.RLock()
	c, ok := v.children[key]
	v.mu.RUnlock()
	if ok {
		return c
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	if c, ok := v.children[key]; ok {
		return c
	}
	c = &Counter{}
	v.children[key] = c
	return c
}

// --- gauges -----------------------------------------------------------------

// Gauge is sampled at scrape time rather than written to.
//
// A callback rather than a settable value, because the alternative is a
// background goroutine keeping a number fresh — and a stale gauge on an incident
// dashboard is worse than no gauge. It is a number people act on.
type Gauge struct {
	name string
	fn   func() float64
}

func (r *Registry) Gauge(name, help string, fn func() float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.register(name, help)
	r.gauges[name] = &Gauge{name: name, fn: fn}
}

// --- histograms -------------------------------------------------------------

// DefaultBuckets, in seconds.
//
// Chosen against the SLOs in docs/PHASE-10 rather than as a generic spread.
// There is a boundary at 0.25 because that is the feed's target, and one at 2
// because past it a request has failed whatever the status code says. A bucket
// that does not straddle a threshold someone cares about is storage.
var DefaultBuckets = []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5}

type Histogram struct {
	mu     sync.Mutex
	bounds []float64
	counts []int64
	sum    float64
	total  int64
	labels []string
}

type HistogramVec struct {
	mu       sync.RWMutex
	name     string
	labels   []string
	bounds   []float64
	children map[string]*Histogram
}

func (r *Registry) Histogram(name, help string, bounds []float64, labels ...string) *HistogramVec {
	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.histograms[name]; ok {
		return existing
	}
	r.register(name, help)
	if bounds == nil {
		bounds = DefaultBuckets
	}
	v := &HistogramVec{name: name, labels: labels, bounds: bounds, children: map[string]*Histogram{}}
	r.histograms[name] = v
	return v
}

func (v *HistogramVec) With(values ...string) *Histogram {
	key := strings.Join(values, "\x00")

	v.mu.RLock()
	h, ok := v.children[key]
	v.mu.RUnlock()
	if ok {
		return h
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	if h, ok := v.children[key]; ok {
		return h
	}
	h = &Histogram{
		bounds: v.bounds,
		counts: make([]int64, len(v.bounds)+1),
		labels: append([]string{}, values...),
	}
	v.children[key] = h
	return h
}

func (h *Histogram) Observe(d time.Duration) {
	seconds := d.Seconds()

	h.mu.Lock()
	defer h.mu.Unlock()
	h.sum += seconds
	h.total++
	for i, b := range h.bounds {
		if seconds <= b {
			h.counts[i]++
			return
		}
	}
	h.counts[len(h.bounds)]++
}

func (h *Histogram) Count() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.total
}

// Quantile estimates a percentile from the bucket counts.
//
// Approximate by construction — it can only ever report a bucket boundary — and
// it returns the *upper* bound of the bucket the quantile falls in, which is an
// honest over-estimate. Stated rather than hidden, because a p99 that looks
// precise and is actually interpolated between two boundaries 1.5 seconds apart
// is a number people quote in meetings.
//
// Returns -1 when the quantile falls above the highest bucket, which is a
// different fact from "5 seconds" and must not be reported as one.
func (h *Histogram) Quantile(q float64) float64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.total == 0 {
		return 0
	}

	target := float64(h.total) * q
	var cumulative float64
	for i, c := range h.counts {
		cumulative += float64(c)
		if cumulative >= target {
			if i >= len(h.bounds) {
				return -1
			}
			return h.bounds[i]
		}
	}
	return -1
}

// --- rendering --------------------------------------------------------------

// Render writes the Prometheus text exposition format.
func (r *Registry) Render() string {
	r.mu.RLock()
	names := append([]string{}, r.order...)
	help := make(map[string]string, len(r.help))
	for k, v := range r.help {
		help[k] = v
	}
	counters := make(map[string]*CounterVec, len(r.counters))
	for k, v := range r.counters {
		counters[k] = v
	}
	gauges := make(map[string]*Gauge, len(r.gauges))
	for k, v := range r.gauges {
		gauges[k] = v
	}
	histograms := make(map[string]*HistogramVec, len(r.histograms))
	for k, v := range r.histograms {
		histograms[k] = v
	}
	r.mu.RUnlock()

	var b strings.Builder
	for _, name := range names {
		if h := help[name]; h != "" {
			fmt.Fprintf(&b, "# HELP %s %s\n", name, h)
		}
		switch {
		case counters[name] != nil:
			fmt.Fprintf(&b, "# TYPE %s counter\n", name)
			renderCounter(&b, counters[name])
		case gauges[name] != nil:
			fmt.Fprintf(&b, "# TYPE %s gauge\n", name)
			fmt.Fprintf(&b, "%s %s\n", name, formatFloat(gauges[name].fn()))
		case histograms[name] != nil:
			fmt.Fprintf(&b, "# TYPE %s histogram\n", name)
			renderHistogram(&b, histograms[name])
		}
	}
	return b.String()
}

func renderCounter(b *strings.Builder, v *CounterVec) {
	v.mu.RLock()
	keys := make([]string, 0, len(v.children))
	for k := range v.children {
		keys = append(keys, k)
	}
	children := make(map[string]*Counter, len(v.children))
	for k, c := range v.children {
		children[k] = c
	}
	v.mu.RUnlock()
	sort.Strings(keys)

	for _, k := range keys {
		fmt.Fprintf(b, "%s%s %d\n", v.name,
			labelSet(v.labels, strings.Split(k, "\x00")), children[k].Value())
	}
}

func renderHistogram(b *strings.Builder, v *HistogramVec) {
	v.mu.RLock()
	keys := make([]string, 0, len(v.children))
	for k := range v.children {
		keys = append(keys, k)
	}
	children := make(map[string]*Histogram, len(v.children))
	for k, h := range v.children {
		children[k] = h
	}
	v.mu.RUnlock()
	sort.Strings(keys)

	bucketNames := append(append([]string{}, v.labels...), "le")

	for _, k := range keys {
		h := children[k]

		h.mu.Lock()
		var cumulative int64
		for i, bound := range h.bounds {
			cumulative += h.counts[i]
			values := append(append([]string{}, h.labels...), formatFloat(bound))
			fmt.Fprintf(b, "%s_bucket%s %d\n", v.name, labelSet(bucketNames, values), cumulative)
		}
		cumulative += h.counts[len(h.bounds)]
		values := append(append([]string{}, h.labels...), "+Inf")
		fmt.Fprintf(b, "%s_bucket%s %d\n", v.name, labelSet(bucketNames, values), cumulative)
		fmt.Fprintf(b, "%s_sum%s %s\n", v.name, labelSet(v.labels, h.labels), formatFloat(h.sum))
		fmt.Fprintf(b, "%s_count%s %d\n", v.name, labelSet(v.labels, h.labels), h.total)
		h.mu.Unlock()
	}
}

func labelSet(names, values []string) string {
	if len(names) == 0 {
		return ""
	}
	parts := make([]string, 0, len(names))
	for i, n := range names {
		v := ""
		if i < len(values) {
			v = values[i]
		}
		parts = append(parts, fmt.Sprintf("%s=%q", n, v))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}
