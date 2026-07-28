package engagement

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

// ClickHouseSink is the production destination for view and engagement events.
//
// PLAN-VIDEO-FIRST §3 puts this data in ClickHouse and is explicit about why:
// "Never Postgres — this is your highest-volume data by an order of magnitude."
// The number that makes it true is in §9 — every view is paid for whether it
// converts or not — and a product whose front door is a video feed generates
// one to two orders of magnitude more view events than orders.
//
// Written over the HTTP interface rather than the native protocol. That is a
// deliberate choice to avoid a driver dependency for what is, in this direction,
// one endpoint: `POST /?query=INSERT ... FORMAT JSONEachRow`. When analytics
// starts *reading* from ClickHouse in anger — Phase 9's ad delivery is the first
// consumer that will — a real driver earns its place. It does not earn it for
// an append-only writer.
//
// § The two rules this implementation is built around:
//
//  1. **Ingest never fails because analytics is unwell.** A 500 to a mobile
//     client makes it retry the whole batch, which amplifies an outage into a
//     traffic spike aimed at the thing that is already down.
//  2. **Dropped events are counted, never silent.** A sink that quietly loses
//     10% of views produces a conversion rate that is 11% too high, and nothing
//     anywhere says so.
type ClickHouseSink struct {
	endpoint string
	table    string
	http     *http.Client

	mu      sync.Mutex
	buf     []chRow
	dropped atomic.Int64
	written atomic.Int64

	// maxBuffer bounds memory when ClickHouse is unreachable. Past it, the
	// oldest events are dropped rather than the newest: a backlog that evicts
	// the newest is a sink that stops reflecting what is happening right now,
	// which is the one thing anyone looks at during an incident.
	maxBuffer int
	batchSize int
}

// chRow is the wire shape. Flat, with no nesting, because ClickHouse columns
// are flat and a nested JSON field becomes a String that nobody can aggregate.
type chRow struct {
	EventID    string `json:"event_id"`
	OccurredAt int64  `json:"occurred_at"`
	ReceivedAt int64  `json:"received_at"`
	EventType  string `json:"event_type"`
	ReelID     string `json:"reel_id"`
	UserID     string `json:"user_id"`
	ViewerKey  string `json:"viewer_key"`
	SessionID  string `json:"session_id"`
	WatchedMS  int32  `json:"watched_ms"`
	Completed  uint8  `json:"completed"`
	Properties string `json:"properties"`
}

type ClickHouseConfig struct {
	// URL of the HTTP interface, e.g. http://clickhouse:8123
	URL      string
	Database string
	Table    string
	Username string
	Password string
}

func NewClickHouseSink(cfg ClickHouseConfig) (*ClickHouseSink, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("engagement: clickhouse url is required")
	}
	if cfg.Database == "" {
		cfg.Database = "analytics"
	}
	if cfg.Table == "" {
		cfg.Table = "reel_events"
	}

	base, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("engagement: clickhouse url: %w", err)
	}
	q := base.Query()
	if cfg.Username != "" {
		q.Set("user", cfg.Username)
		q.Set("password", cfg.Password)
	}
	// async_insert is what makes many small batches cheap on the ClickHouse
	// side: the server accumulates them into a single part rather than creating
	// one part per request, which is the classic way to melt a MergeTree.
	q.Set("async_insert", "1")
	q.Set("wait_for_async_insert", "0")
	base.RawQuery = q.Encode()

	return &ClickHouseSink{
		endpoint: base.String(),
		table:    cfg.Database + "." + cfg.Table,
		// Short. A hung insert must fail and be retried on the next flush, not
		// hold the flusher while every subsequent batch queues behind it.
		http:      &http.Client{Timeout: 5 * time.Second},
		maxBuffer: 50_000,
		batchSize: 2_000,
	}, nil
}

// Publish buffers a batch. It does not perform I/O and it cannot fail.
//
// The request path's only job here is to hand the events over. Doing the insert
// inline would put ClickHouse's latency on every mobile client's event POST —
// and those POSTs happen every few seconds per active viewer.
func (s *ClickHouseSink) Publish(_ context.Context, events []Event, viewer Viewer) error {
	now := time.Now().UTC().UnixMilli()

	rows := make([]chRow, 0, len(events))
	for _, e := range events {
		props := "{}"
		if len(e.Properties) > 0 {
			if raw, err := json.Marshal(e.Properties); err == nil {
				props = string(raw)
			}
		}

		var reelID string
		if e.ReelID != nil {
			reelID = e.ReelID.String()
		}
		var userID string
		if viewer.UserID.Valid {
			userID = viewer.UserID.UUID.String()
		}
		var completed uint8
		if e.Completed {
			completed = 1
		}

		rows = append(rows, chRow{
			// The same dedup key the Postgres sink uses. ClickHouse does not
			// enforce uniqueness, so this is what a ReplacingMergeTree — or a
			// query written by someone who read the schema — deduplicates on.
			// At-least-once client batches make it necessary either way.
			EventID:    DedupKey(e, viewer),
			OccurredAt: e.OccurredAt.UTC().UnixMilli(),
			// Kept alongside the client clock because the difference between
			// them is the only way to spot a client batching hours of offline
			// events, and a view "at" 3am that arrived at 9am is not a 3am view.
			ReceivedAt: now,
			EventType:  e.Type,
			ReelID:     reelID,
			UserID:     userID,
			ViewerKey:  viewer.Key,
			SessionID:  e.SessionID,
			WatchedMS:  int32(e.WatchedMS),
			Completed:  completed,
			Properties: props,
		})
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if over := len(s.buf) + len(rows) - s.maxBuffer; over > 0 {
		if over >= len(s.buf) {
			s.dropped.Add(int64(len(s.buf)))
			s.buf = s.buf[:0]
		} else {
			s.dropped.Add(int64(over))
			s.buf = s.buf[over:]
		}
	}
	s.buf = append(s.buf, rows...)
	return nil
}

// Flush writes buffered events. Called by the worker on a ticker.
//
// On failure the batch goes back on the front of the buffer and is retried next
// tick. That is bounded by maxBuffer — a ClickHouse outage long enough to fill
// 50,000 events starts dropping the oldest, which is the honest behaviour: the
// alternative is unbounded memory in the process that also serves the feed.
func (s *ClickHouseSink) Flush(ctx context.Context) (int, error) {
	s.mu.Lock()
	if len(s.buf) == 0 {
		s.mu.Unlock()
		return 0, nil
	}
	n := min(len(s.buf), s.batchSize)
	batch := make([]chRow, n)
	copy(batch, s.buf[:n])
	s.buf = s.buf[n:]
	s.mu.Unlock()

	if err := s.insert(ctx, batch); err != nil {
		s.mu.Lock()
		s.buf = append(batch, s.buf...)
		s.mu.Unlock()
		return 0, err
	}

	s.written.Add(int64(n))
	return n, nil
}

func (s *ClickHouseSink) insert(ctx context.Context, rows []chRow) error {
	var body bytes.Buffer
	enc := json.NewEncoder(&body)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			return fmt.Errorf("engagement: encode clickhouse row: %w", err)
		}
	}

	query := "INSERT INTO " + s.table + " FORMAT JSONEachRow"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.endpoint+"&query="+url.QueryEscape(query), &body)
	if err != nil {
		return fmt.Errorf("engagement: build clickhouse request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-ndjson")

	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("engagement: clickhouse insert: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		detail := make([]byte, 512)
		n, _ := resp.Body.Read(detail)
		return fmt.Errorf("engagement: clickhouse returned %s: %s",
			resp.Status, bytes.TrimSpace(detail[:n]))
	}
	return nil
}

// Stats reports what has been written and what has been lost.
//
// Dropped is the number that matters and the reason this method exists. A sink
// that quietly loses 10% of views produces a conversion rate 11% too high, and
// Phase 3 spent an entire design on not letting that number be wrong.
func (s *ClickHouseSink) Stats() (written, dropped, buffered int64) {
	s.mu.Lock()
	buffered = int64(len(s.buf))
	s.mu.Unlock()
	return s.written.Load(), s.dropped.Load(), buffered
}

// LogStats emits the counters, loudly when anything has been dropped.
func (s *ClickHouseSink) LogStats(ctx context.Context) {
	written, dropped, buffered := s.Stats()
	if dropped > 0 {
		slog.ErrorContext(ctx, "analytics events dropped — conversion denominators are understated",
			"dropped", dropped, "written", written, "buffered", buffered)
		return
	}
	slog.DebugContext(ctx, "analytics sink", "written", written, "buffered", buffered)
}

// TeeSink writes to two sinks, and reports the failure of neither.
//
// Used during the ClickHouse cutover: events land in both stores so that the
// numbers can be compared before anything starts depending on the new one. A
// migration where the old store is switched off first is a migration whose
// first bad number has nothing to check against.
type TeeSink struct {
	Primary   Sink
	Secondary Sink
}

func (t TeeSink) Publish(ctx context.Context, events []Event, viewer Viewer) error {
	err := t.Primary.Publish(ctx, events, viewer)
	if t.Secondary != nil {
		if serr := t.Secondary.Publish(ctx, events, viewer); serr != nil {
			slog.WarnContext(ctx, "secondary analytics sink failed", "error", serr)
		}
	}
	return err
}
