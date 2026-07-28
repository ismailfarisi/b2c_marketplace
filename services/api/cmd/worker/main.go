// Command worker runs background jobs.
//
// Jobs are Postgres-backed (River) rather than Redis-backed, so a job can be
// enqueued in the same transaction as the state change that requires it. That
// is the same guarantee the outbox gives events, and it removes the entire
// class of "the order committed but the job never ran" bug.
//
// Phase 1 jobs:
//   - outbox relay          publish committed events
//   - capacity sweeper      release expired reservations
//   - idempotency reaper    drop keys past their TTL
//
// Phase 2 adds the media pipeline: probe, transcode, moderate, publish.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/app"
	"github.com/marketplace/api/internal/config"
	"github.com/marketplace/api/internal/platform/postgres"
	"github.com/marketplace/api/internal/platform/redisx"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)).
		With("service", "worker", "version", version))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.Connect(ctx, postgres.Config{
		URL:      cfg.DatabaseURL,
		MaxConns: 10,
		MinConns: 1,
	})
	if err != nil {
		return err
	}
	defer pool.Close()

	rdb, err := redisx.Connect(ctx, cfg.RedisURL)
	if err != nil {
		return err
	}
	if rdb != nil {
		defer rdb.Close()
	}

	media, err := app.BuildMedia(ctx, cfg, pool, rdb, app.BuildRegistries(pool).Actions)
	if err != nil {
		return err
	}

	// The media pipeline runs on its own faster loop, in its own goroutine: it
	// has a five-minute end-to-end budget (exit criterion #1) and must not
	// queue behind a 30-second capacity sweep.
	go runMediaLoop(ctx, pool, media)

	// River is wired here once the first job type exists. Until then the loop
	// below runs the two sweepers that Phase 1 genuinely needs, on a ticker.
	// This is not a permanent design — it is the smallest thing that keeps
	// capacity from leaking while the job framework is still being introduced.
	slog.Info("worker started")

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("worker shutting down")
			return nil
		case <-ticker.C:
			sweep(ctx, pool)
		}
	}
}

func sweep(ctx context.Context, pool *pgxpool.Pool) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	// Expired capacity reservations. Every abandoned checkout holds inventory
	// until this runs; skipping it means vendors show as fully booked with no
	// orders behind it, and nobody notices until they complain.
	released, err := releaseExpiredReservations(ctx, pool)
	if err != nil && !errors.Is(err, context.Canceled) {
		slog.ErrorContext(ctx, "capacity sweep failed", "error", err)
	} else if released > 0 {
		slog.InfoContext(ctx, "released expired reservations", "count", released)
	}

	if _, err := pool.Exec(ctx,
		`DELETE FROM idempotency_keys WHERE expires_at < now()`); err != nil {
		slog.ErrorContext(ctx, "idempotency reap failed", "error", err)
	}
}

func releaseExpiredReservations(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	rows, err := pool.Query(ctx, `
		SELECT id
		  FROM capacity_reservations
		 WHERE state = 'held' AND expires_at < now()
		 ORDER BY expires_at
		 LIMIT 500`)
	if err != nil {
		return 0, err
	}

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	released := 0
	for _, id := range ids {
		// release_capacity is transactional per reservation. Doing them one at a
		// time rather than in one big transaction keeps lock hold times short on
		// the hottest table in the system.
		var ok bool
		if err := pool.QueryRow(ctx,
			`SELECT release_capacity($1, 'expired')`, id).Scan(&ok); err != nil {
			slog.WarnContext(ctx, "release failed", "reservation_id", id, "error", err)
			continue
		}
		if ok {
			released++
		}
	}
	return released, nil
}
