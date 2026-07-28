package engagement

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
)

// Event types the client may report. An allow-list rather than free text: an
// analytics store fed arbitrary strings accumulates typos that look like real
// event types forever, and nobody can tell `view_start` from `viewStart` six
// months later.
const (
	EventViewStart    = "view_start"
	EventViewProgress = "view_progress"
	EventLike         = "like"
	EventUnlike       = "unlike"
	EventComment      = "comment"
	EventShare        = "share"
	EventSave         = "save"
	EventUnsave       = "unsave"
	EventSkip         = "skip"
	// Phase 3 adds action_click here. It is listed now because the ingest
	// endpoint should not need a deploy to start accepting it.
	EventActionClick = "action_click"
)

var eventTypes = map[string]bool{
	EventViewStart: true, EventViewProgress: true, EventLike: true,
	EventUnlike: true, EventComment: true, EventShare: true,
	EventSave: true, EventUnsave: true, EventSkip: true, EventActionClick: true,
}

// Event is one client-reported interaction.
type Event struct {
	Type       string
	ReelID     *uuid.UUID
	SessionID  string
	OccurredAt time.Time
	// WatchedMS on a view_progress event. The view counter increments only when
	// this crosses ViewMinimumMS, which is why the client sends progress rather
	// than a pre-judged "view".
	WatchedMS int
	// Completed is reported separately from WatchedMS, and deliberately: "did
	// they watch it" and "did they finish it" answer different questions, and
	// Phase 4's ranker weights them differently.
	Completed  bool
	Properties map[string]any
}

// Viewer identifies who reported a batch.
//
// Key is present for anonymous viewers and UserID is not, which is why both are
// here. Phase 2 carried only the user id, and the consequence was invisible
// until Phase 4 went looking for it: an interest profile keyed on user id
// personalises nothing for anonymous viewers, and anonymous viewers are exactly
// the population whose first session decides whether they ever return.
type Viewer struct {
	UserID uuid.NullUUID
	Key    string
}

// Sink is where events go after ingest.
//
// An interface because the production destination is Kinesis → ClickHouse and
// the development destination is a Postgres table, and because the ingest
// endpoint must not fail a client batch when the stream is unavailable. A
// dropped analytics event is a rounding error; a 500 to a mobile client makes
// it retry the whole batch and amplify the outage.
type Sink interface {
	Publish(ctx context.Context, events []Event, viewer Viewer) error
}

// Ingest handles batched client events.
type Ingest struct {
	sink     Sink
	counters *Counters
}

func NewIngest(sink Sink, counters *Counters) *Ingest {
	return &Ingest{sink: sink, counters: counters}
}

// MaxBatch bounds one request. A client that has been offline for a day and
// batches 50,000 events must not be able to hold a connection open while they
// are written; it splits or it is rejected.
const MaxBatch = 200

// Submit validates a batch, applies counter side effects, and publishes.
//
// Note the ordering: counters first, sink second. If the sink is down, the
// counts that drive the product still move; if counters are down, the analytics
// record is still complete. Neither failure takes the other with it.
func (i *Ingest) Submit(ctx context.Context, events []Event, viewer Viewer) error {
	if len(events) == 0 {
		return nil
	}
	if len(events) > MaxBatch {
		return &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "events", Code: "too_many",
			Message: fmt.Sprintf("maximum %d events per batch", MaxBatch),
		}}}
	}

	var fields []httpx.FieldError
	for idx, e := range events {
		if !eventTypes[e.Type] {
			fields = append(fields, httpx.FieldError{
				Field:   fmt.Sprintf("events[%d].type", idx),
				Code:    "unknown_event_type",
				Message: e.Type,
			})
		}
		if e.OccurredAt.IsZero() {
			fields = append(fields, httpx.FieldError{
				Field: fmt.Sprintf("events[%d].occurred_at", idx),
				Code:  "required",
			})
		}
	}
	if len(fields) > 0 {
		return &httpx.ValidationError{Fields: fields}
	}

	if i.counters != nil {
		i.applyCounters(ctx, events)
	}
	return i.sink.Publish(ctx, events, viewer)
}

// applyCounters translates events into counter movements.
//
// Errors are swallowed on purpose. A counter that failed to increment is a
// slightly-wrong number on a card; failing the batch would make the client
// retry every event in it, which double-counts everything that did succeed.
func (i *Ingest) applyCounters(ctx context.Context, events []Event) {
	for _, e := range events {
		if e.ReelID == nil {
			continue
		}
		switch e.Type {
		case EventViewProgress:
			// The definition, applied in exactly one place.
			if e.WatchedMS >= ViewMinimumMS {
				_ = i.counters.Incr(ctx, *e.ReelID, "views", 1)
			}
		case EventLike:
			_ = i.counters.Incr(ctx, *e.ReelID, "likes", 1)
		case EventUnlike:
			_ = i.counters.Incr(ctx, *e.ReelID, "likes", -1)
		case EventComment:
			_ = i.counters.Incr(ctx, *e.ReelID, "comments", 1)
		case EventShare:
			_ = i.counters.Incr(ctx, *e.ReelID, "shares", 1)
		case EventSave:
			_ = i.counters.Incr(ctx, *e.ReelID, "saves", 1)
		case EventUnsave:
			_ = i.counters.Incr(ctx, *e.ReelID, "saves", -1)
		}
	}
}

// PostgresSink is the development and fallback destination.
type PostgresSink struct {
	pool *pgxpool.Pool
}

func NewPostgresSink(pool *pgxpool.Pool) *PostgresSink { return &PostgresSink{pool: pool} }

func (s *PostgresSink) Publish(ctx context.Context, events []Event, viewer Viewer) error {
	for _, e := range events {
		props, err := json.Marshal(e.Properties)
		if err != nil {
			props = []byte("{}")
		}
		var uid any
		if viewer.UserID.Valid {
			uid = viewer.UserID.UUID
		}
		// ON CONFLICT DO NOTHING against the dedup key. Client batches are
		// at-least-once by design — a mobile client that loses its connection
		// mid-POST resends the whole batch — so ingest must be idempotent or
		// every flaky network doubles the view count.
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO client_events
			    (user_id, viewer_key, session_id, reel_id, event_type,
			     occurred_at, watched_ms, completed, properties, dedup_key)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (dedup_key) DO NOTHING`,
			uid, nullableText(viewer.Key), nullableText(e.SessionID), e.ReelID, e.Type,
			e.OccurredAt, e.WatchedMS, e.Completed, props, DedupKey(e, viewer),
		); err != nil {
			return fmt.Errorf("engagement: publish event: %w", err)
		}
	}
	return nil
}

// DedupKey identifies one logical event across retries.
//
// Hashed rather than concatenated because the parts include a client-supplied
// session ID of unbounded length, and a unique index on unbounded text is a
// footgun. The watched-ms bucket is included so successive progress events on
// the same reel are distinct, and rounded to the nearest 25% so a client
// reporting 5001 ms and 5003 ms on a retry does not create two rows.
func DedupKey(e Event, viewer Viewer) string {
	reel := ""
	if e.ReelID != nil {
		reel = e.ReelID.String()
	}
	// The viewer key, falling back to the user id and then to a constant.
	// Keying anonymous batches on the literal "anon" would make two different
	// devices reporting the same event on the same reel in the same session
	// collide — which does not happen for signed-in users and is exactly the
	// population this key now covers.
	owner := viewer.Key
	if owner == "" {
		owner = "anon"
		if viewer.UserID.Valid {
			owner = viewer.UserID.UUID.String()
		}
	}
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%s|%d",
		owner, e.SessionID, reel, e.Type, progressBucket(e.WatchedMS))))
	return hex.EncodeToString(h[:16])
}

// progressBucket collapses watch progress to the quartiles §5 actually asks the
// client to report.
func progressBucket(ms int) int {
	switch {
	case ms <= 0:
		return 0
	case ms < 2_000:
		return 1
	case ms < 5_000:
		return 2
	case ms < 15_000:
		return 3
	default:
		return 4
	}
}

func nullableText(s string) any {
	if s == "" {
		return nil
	}
	return s
}
