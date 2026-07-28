package admin

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Queue depth, for the one screen an ops shift starts on.
//
// This exists because the admin panel exposed a gap the API had been hiding:
// there were nine `decide` endpoints and only five `queue` endpoints, so four
// review surfaces could be acted on and not found. A decide endpoint with no
// queue behind it is a feature nobody can use — the work reaches a reviewer
// only if someone already knew its id.
//
// One query rather than nine round trips, because the overview is polled every
// thirty seconds by every open tab in the building.

// QueueDepth is one review surface.
type QueueDepth struct {
	Queue string
	// Waiting is the number of undecided items.
	Waiting int
	// OldestSeconds is how long the oldest has waited. The number that matters
	// more than depth: a deep and *young* queue is a busy afternoon, a shallow
	// and *old* one is a queue nobody is working, and only the second is an
	// incident. A dashboard that shows depth alone reports them identically.
	OldestSeconds int
	// BreachedSLA counts items past this queue's promise. Zero for queues with
	// no promise, which is honest rather than inventing one.
	BreachedSLA int
}

// SLAs per queue. Sourced from the phase that created each one rather than
// picked here, so a change to a promise happens in one place.
var queueSLA = map[string]time.Duration{
	"reel":           2 * time.Hour,      // PHASE-2
	"comment":        30 * time.Minute,   // PHASE-4 — comments are judged in seconds
	"message":        15 * time.Minute,   // PHASE-6 — a held message is a stalled conversation
	"listing":        4 * time.Hour,      // PHASE-6 — needs the VIN and price history in front of it
	"fraud":          24 * time.Hour,     // PHASE-6 — investigation, not triage
	"appeal":         7 * 24 * time.Hour, // PHASE-8
	"kyc":            2 * 24 * time.Hour, // PHASE-8 — blocks a vendor's first payout
	"dispute":        3 * 24 * time.Hour, // PHASE-7
	"boost":          24 * time.Hour,     // PHASE-9 — an unreviewed campaign is money not spent
	"payout_account": 24 * time.Hour,
}

// SLAFor exposes the table so the client can render a deadline rather than
// invent one.
func SLAFor(queue string) time.Duration { return queueSLA[queue] }

// Queues returns every review surface's depth in one round trip.
func (s *Service) Queues(ctx context.Context, actorID uuid.UUID) ([]QueueDepth, error) {
	if err := s.RequireOps(ctx, actorID); err != nil {
		return nil, err
	}

	// A UNION ALL of nine counts. Each branch is an index lookup on a partial
	// index that already exists for the queue endpoint itself; the whole thing
	// is cheaper than the nine HTTP round trips it replaces.
	rows, err := s.pool.Query(ctx, `
		WITH q AS (
		    SELECT 'reel' AS queue, coalesce(metadata_ready_at, created_at) AS entered
		      FROM reels WHERE state = 'review_pending'
		    UNION ALL
		    SELECT 'comment', created_at FROM reel_comments
		     WHERE state = 'pending' AND deleted_at IS NULL
		    UNION ALL
		    SELECT 'message', created_at FROM messages WHERE state = 'held'
		    UNION ALL
		    SELECT 'listing', updated_at FROM vehicle_listings
		     WHERE status = 'pending_review'
		    UNION ALL
		    SELECT 'fraud', created_at FROM fraud_signals WHERE state = 'open'
		    UNION ALL
		    SELECT 'appeal', created_at FROM content_appeals
		     WHERE status IN ('open', 'in_review')
		    UNION ALL
		    SELECT 'kyc', created_at FROM kyc_cases
		     WHERE status IN ('submitted', 'in_review')
		    UNION ALL
		    SELECT 'payout_account', created_at FROM payout_accounts WHERE status = 'pending'
		    UNION ALL
		    SELECT 'dispute', created_at FROM disputes
		     WHERE state IN ('open', 'investigating', 'awaiting_response')
		    UNION ALL
		    SELECT 'boost', created_at FROM boost_campaigns WHERE status = 'pending_review'
		)
		SELECT queue, count(*), coalesce(extract(epoch FROM (now() - min(entered))), 0)::int
		  FROM q GROUP BY queue`)
	if err != nil {
		return nil, fmt.Errorf("admin: queue depths: %w", err)
	}
	defer rows.Close()

	depths := map[string]QueueDepth{}
	for rows.Next() {
		var d QueueDepth
		if err := rows.Scan(&d.Queue, &d.Waiting, &d.OldestSeconds); err != nil {
			return nil, err
		}
		depths[d.Queue] = d
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Every known queue is returned, including the empty ones. A queue that
	// disappears from the dashboard when it empties is a queue whose sudden
	// reappearance nobody notices — and an empty moderation queue is a fact
	// worth showing rather than an absence.
	out := make([]QueueDepth, 0, len(queueSLA))
	for _, name := range []string{
		"reel", "comment", "message", "listing", "fraud",
		"appeal", "kyc", "payout_account", "dispute", "boost",
	} {
		d := depths[name]
		d.Queue = name
		if sla := queueSLA[name]; sla > 0 && d.OldestSeconds > int(sla.Seconds()) {
			// Only the oldest is known to have breached without a second query.
			// Counting every breached item would mean carrying every entry
			// timestamp back; the flag is what the dashboard acts on, and the
			// queue screen itself shows the per-item detail.
			d.BreachedSLA = 1
		}
		out = append(out, d)
	}
	return out, nil
}
