// Package postgres owns the connection pool and the transaction helper.
//
// Modules take a *pgxpool.Pool or a pgx.Tx; nothing else in the codebase opens
// a connection.
package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	URL             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

func Connect(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	if cfg.MaxConns > 0 {
		poolCfg.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		poolCfg.MinConns = cfg.MinConns
	}
	if cfg.MaxConnLifetime > 0 {
		poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	}
	if cfg.MaxConnIdleTime > 0 {
		poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime
	}

	// A statement timeout at the pool level is a backstop against one pathological
	// query holding a connection until the pool starves. Individual long jobs
	// override it per-transaction.
	poolCfg.ConnConfig.RuntimeParams["statement_timeout"] = "15000"
	poolCfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "30000"
	poolCfg.ConnConfig.RuntimeParams["application_name"] = "marketplace-api"

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}

	slog.Info("postgres connected", "max_conns", poolCfg.MaxConns)
	return pool, nil
}

// InTx runs fn inside a transaction, rolling back on error or panic.
//
// Every write path that touches more than one table goes through this — most
// importantly the ones that write to `outbox_events`. An event published outside
// the transaction that produced it is the classic dual-write bug: the order
// exists and the notification never fires, or worse, the notification fires for
// an order that rolled back.
func InTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		// Use WithoutCancel so a rollback still runs when the request context
		// has already expired — otherwise a timed-out request leaks an open
		// transaction until the idle timeout reaps it.
		if rbErr := tx.Rollback(context.WithoutCancel(ctx)); rbErr != nil && rbErr != pgx.ErrTxClosed {
			slog.ErrorContext(ctx, "rollback failed", "error", rbErr)
		}
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
