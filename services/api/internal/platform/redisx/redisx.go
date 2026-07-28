// Package redisx owns the Redis client.
//
// Redis holds only data the product can survive losing: the per-viewer seen-set
// and the running engagement counters. Nothing that has to be durable lives
// here — jobs are Postgres-backed for exactly that reason (see cmd/worker), and
// counters are flushed to Postgres, which remains the truth.
//
// That constraint is what lets every caller treat a Redis failure as a
// degradation rather than an outage.
package redisx

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Connect returns nil, nil when no URL is configured.
//
// A nil client is a supported state, not an error: the API runs without Redis
// with a repeating feed and no live counters, and refusing to boot would make
// an optional dependency mandatory.
func Connect(ctx context.Context, url string) (*redis.Client, error) {
	if url == "" {
		return nil, nil
	}

	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("redisx: parse url: %w", err)
	}
	// Short timeouts. Every call from here sits on a request path that has a
	// fallback, so waiting is strictly worse than failing over to it.
	opts.DialTimeout = 2 * time.Second
	opts.ReadTimeout = time.Second
	opts.WriteTimeout = time.Second

	client := redis.NewClient(opts)

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("redisx: ping: %w", err)
	}
	return client, nil
}
