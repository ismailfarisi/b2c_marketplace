// Package timesignal keeps `reel_time_signals` in step with the verticals that
// own its rows.
//
// The seam (internal/seam/subject) defines what a time signal is and who can
// compute one. This package is the only thing that writes them, and it exists as
// a module rather than as part of the seam for the reason the rest of the
// codebase separates the two: a seam is a contract, and a contract that also
// owns a table starts accreting storage decisions until nobody can implement it
// without a database.
package timesignal

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/seam/subject"
)

// Projector writes the projection.
type Projector struct {
	pool     *pgxpool.Pool
	registry *subject.Registry
}

func New(pool *pgxpool.Pool, registry *subject.Registry) *Projector {
	return &Projector{pool: pool, registry: registry}
}

// Refresh recomputes one subject's signal and stores it.
//
// Called from a vertical's write path — a listing published, an auction
// rescheduled, a plan's cutoff changed — and by Reconcile. Idempotent, so a
// vertical that calls it twice or a worker that overlaps with a write costs a
// round-trip and nothing else.
//
// A provider that reports the subject as timeless deletes the row rather than
// leaving it. That direction is the one that matters: a sold car whose signal
// row survived would keep drawing a freshness bar for a listing nobody can buy,
// and the failure is invisible because the bar looks exactly like a working one.
func (p *Projector) Refresh(ctx context.Context, t subject.Type, id uuid.UUID) error {
	sig, ok, err := p.registry.Project(ctx, t, id)
	if err != nil {
		return fmt.Errorf("timesignal: project %s/%s: %w", t, id, err)
	}
	if !ok {
		_, err := p.pool.Exec(ctx, `
			DELETE FROM reel_time_signals
			 WHERE subject_type = $1 AND subject_id = $2`, string(t), id)
		if err != nil {
			return fmt.Errorf("timesignal: clear %s/%s: %w", t, id, err)
		}
		return nil
	}

	var horizon *int32
	if sig.Horizon > 0 {
		s := int32(sig.Horizon.Seconds())
		horizon = &s
	}

	_, err = p.pool.Exec(ctx, `
		INSERT INTO reel_time_signals
		       (subject_type, subject_id, mode, starts_at, ends_at,
		        horizon_seconds, timezone, recurring, projected_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
		ON CONFLICT (subject_type, subject_id) DO UPDATE
		   SET mode            = EXCLUDED.mode,
		       starts_at       = EXCLUDED.starts_at,
		       ends_at         = EXCLUDED.ends_at,
		       horizon_seconds = EXCLUDED.horizon_seconds,
		       timezone        = EXCLUDED.timezone,
		       recurring       = EXCLUDED.recurring,
		       projected_at    = now()`,
		string(t), id, string(sig.Mode),
		nullTime(sig.StartsAt), nullTime(sig.EndsAt),
		horizon, nullString(sig.Timezone), sig.Recurring)
	if err != nil {
		return fmt.Errorf("timesignal: store %s/%s: %w", t, id, err)
	}
	return nil
}

// StaleAfter is how long a projection is trusted without being recomputed.
//
// A day. The failure this bounds is a vertical that changed a source row without
// calling Refresh, which is silent by construction — nothing errors, the bar is
// just wrong. A day is short enough that a freshness bar is never visibly stale
// and long enough that the sweep is cheap.
const StaleAfter = 24 * time.Hour

// Reconcile brings the projection back in line with its sources.
//
// Three jobs, in one pass, because they share a work-list:
//
//  1. **Backfill.** Subjects that published reels have but the projection does
//     not. This is what populates the table for content that existed before a
//     provider did, and it is driven from `reels` rather than from the verticals'
//     own tables — the projector must not know that a vehicle_listing lives in
//     vehicle_listings, or the seam has leaked into the thing enforcing it.
//
//  2. **Recurrence.** A cutoff whose instant has passed rolls to the next cycle.
//     Done by re-calling the provider rather than by adding an interval here: the
//     recurrence rule is the vertical's, and interval arithmetic across a DST
//     boundary is exactly the sort of thing this codebase has already been bitten
//     by once.
//
//  3. **Endings.** A *non*-recurring signal whose instant has passed. Same
//     trigger as recurrence and the opposite outcome: when this clock reaches
//     zero the subject is usually finished — the lot sold, the offer closed —
//     and re-calling the provider is what removes the row.
//
//     This clause was missing, and its absence disabled the deletion path that
//     [Projector.Refresh] documents as "the direction that matters". Nothing
//     else selects an ended countdown: the recurrence clause requires
//     `recurring`, and staleness does not fire for a day. A settled plate
//     auction was found on For you under a live "1h 49m left" pill, next to
//     its own Place bid CTA correctly disabled with the reason "Sold" — the
//     two facts on screen at once, and the clock is the louder of them.
//
//     Only the surfaces that filter on the signal were spared, because
//     `candidatePredicate` requires `ts.ends_at > now()`. For you joins it as
//     decoration, so For you showed it.
//
//  4. **Staleness.** Anything not recomputed within StaleAfter.
//
// Returns how many subjects were refreshed. Errors on individual subjects are
// logged and skipped — one vertical's broken provider must not stop the sweep,
// because the rows it would have blocked belong to other verticals.
func (p *Projector) Reconcile(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 500
	}

	rows, err := p.pool.Query(ctx, `
		-- Subjects that reels reference and the projection is missing or
		-- doubts. DISTINCT because a subject with four reels is one subject.
		SELECT DISTINCT r.subject_type, r.subject_id
		  FROM reels r
		  LEFT JOIN reel_time_signals ts
		         ON ts.subject_type = r.subject_type
		        AND ts.subject_id = r.subject_id
		 WHERE r.state = 'published'
		   AND r.subject_type IS NOT NULL
		   AND r.subject_id IS NOT NULL
		   AND (ts.subject_id IS NULL
		        OR (ts.recurring AND ts.ends_at < now())
		        -- An ended non-recurring signal, asked about exactly once.
		        --
		        -- The projected_at <= ends_at test is what stops this from
		        -- selecting the same row on every sweep. Recurring self-limits,
		        -- because rolling it writes an ends_at in the future; a
		        -- non-recurring one does not, so a provider that keeps
		        -- returning the same past instant would refill the work-list
		        -- forever and crowd out the backfill that shares its LIMIT.
		        -- Once re-projected after its own end, the answer is taken as
		        -- considered and not asked again until staleness.
		        OR (NOT ts.recurring
		            AND ts.ends_at IS NOT NULL
		            AND ts.ends_at < now()
		            AND ts.projected_at <= ts.ends_at)
		        OR ts.projected_at < now() - make_interval(secs => $1))
		 LIMIT $2`, StaleAfter.Seconds(), limit)
	if err != nil {
		return 0, fmt.Errorf("timesignal: reconcile scan: %w", err)
	}

	type ref struct {
		t  subject.Type
		id uuid.UUID
	}
	// Drained before any Refresh runs. Holding a cursor open while issuing
	// writes on the same pool is how a reconcile job deadlocks itself against
	// its own connection limit.
	var work []ref
	for rows.Next() {
		var t string
		var id uuid.UUID
		if err := rows.Scan(&t, &id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("timesignal: reconcile scan row: %w", err)
		}
		work = append(work, ref{subject.Type(t), id})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("timesignal: reconcile scan: %w", err)
	}

	// Subjects whose type has no provider are skipped without a write and
	// without a log line. There is nothing wrong with them — a reel about a
	// meal plan is fine, meals just has no provider yet — and logging one line
	// per such subject per sweep would bury the errors that matter.
	done := 0
	for _, w := range work {
		if !p.registry.Has(w.t) {
			continue
		}
		if err := p.Refresh(ctx, w.t, w.id); err != nil {
			slog.Warn("time signal refresh failed",
				"subject_type", w.t, "subject_id", w.id, "error", err)
			continue
		}
		done++
	}
	return done, nil
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
