package idempotency

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
)

func testStore(t *testing.T) (*Store, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return New(pool), pool
}

const ep = "POST /v1/test/idempotency"

func TestReleasedKeyRunsAgain(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	key := uuid.New()

	if got, err := s.Begin(ctx, key, uuid.Nil, ep, "h"); err != nil || got != nil {
		t.Fatalf("claim: %v %v", got, err)
	}
	_, err := s.Begin(ctx, key, uuid.Nil, ep, "h")
	var coded *httpx.CodedError
	if !errors.Is(err, httpx.ErrConflict) || !errors.As(err, &coded) || coded.Code != "idempotency_in_flight" {
		t.Fatalf("twin while in flight: want coded 409, got %v", err)
	}

	if err := s.Release(ctx, key, ep); err != nil {
		t.Fatalf("release: %v", err)
	}
	if got, err := s.Begin(ctx, key, uuid.Nil, ep, "h"); err != nil || got != nil {
		t.Fatalf("after release the key must execute afresh: %v %v", got, err)
	}

	if err := s.Complete(ctx, key, ep, 201, []byte(`{"ok":true}`)); err != nil {
		t.Fatalf("complete: %v", err)
	}
	// Release never discards a completed outcome.
	if err := s.Release(ctx, key, ep); err != nil {
		t.Fatalf("release completed: %v", err)
	}
	got, err := s.Begin(ctx, key, uuid.Nil, ep, "h")
	if err != nil || got == nil || got.Status != 201 || string(got.Body) != `{"ok":true}` {
		t.Fatalf("replay: %+v %v", got, err)
	}
}

func TestStaleInFlightIsReclaimed(t *testing.T) {
	s, pool := testStore(t)
	ctx := context.Background()
	key, user, other := uuid.New(), uuid.New(), uuid.New()

	if _, err := s.Begin(ctx, key, user, ep, "h"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE idempotency_keys SET created_at = now() - interval '10 minutes'
		WHERE key = $1 AND endpoint = $2`, key, ep); err != nil {
		t.Fatalf("age: %v", err)
	}

	// A stale claim still belongs to its owner and body.
	if _, err := s.Begin(ctx, key, other, ep, "h"); !errors.Is(err, httpx.ErrIdempotencyMismatch) {
		t.Fatalf("other user: want mismatch, got %v", err)
	}
	if got, err := s.Begin(ctx, key, user, ep, "h"); err != nil || got != nil {
		t.Fatalf("stale claim should be taken over: %v %v", got, err)
	}
	if _, err := s.Begin(ctx, key, user, ep, "h"); !errors.Is(err, httpx.ErrConflict) {
		t.Fatalf("reclaimed row is in flight again: got %v", err)
	}
}

func TestExpiredKeyIsFresh(t *testing.T) {
	s, pool := testStore(t)
	ctx := context.Background()
	key := uuid.New()

	if _, err := s.Begin(ctx, key, uuid.Nil, ep, "h"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := s.Complete(ctx, key, ep, 200, []byte(`{}`)); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE idempotency_keys SET expires_at = now() - interval '1 second'
		WHERE key = $1 AND endpoint = $2`, key, ep); err != nil {
		t.Fatalf("expire: %v", err)
	}
	if got, err := s.Begin(ctx, key, uuid.Nil, ep, "different body"); err != nil || got != nil {
		t.Fatalf("an expired key is a new request: %v %v", got, err)
	}
}
