// Package idempotency backs the httpx.Idempotency middleware with the
// idempotency_keys table.
package idempotency

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// ErrInFlight means an identical request is currently executing.
//
// This is a 409, not a replay: returning the eventual response would require
// blocking on another request's transaction, and returning success before the
// work is done would be a lie. The client retries in a moment.
//
// It wraps httpx.ErrConflict so the boundary maps it without httpx needing to
// import this package — the dependency only runs one way.
var ErrInFlight = fmt.Errorf("%w: idempotent request already in flight", httpx.ErrConflict)

// Begin claims the key, or reports what has already happened with it.
//
// The INSERT ... ON CONFLICT DO NOTHING is the whole concurrency control: two
// simultaneous requests with the same key mean exactly one insert succeeds. The
// loser reads the existing row and learns whether it is a replay, a body
// mismatch, or a race against an in-flight twin.
func (s *Store) Begin(
	ctx context.Context,
	key uuid.UUID,
	userID uuid.UUID,
	endpoint string,
	requestHash string,
) (*httpx.StoredResponse, error) {
	var ownerID *uuid.UUID
	if userID != uuid.Nil {
		ownerID = &userID
	}

	tag, err := s.pool.Exec(ctx, `
		INSERT INTO idempotency_keys (key, user_id, endpoint, request_hash, state)
		VALUES ($1, $2, $3, $4, 'in_flight')
		ON CONFLICT (key, endpoint) DO NOTHING`,
		key, ownerID, endpoint, requestHash)
	if err != nil {
		return nil, fmt.Errorf("claim idempotency key: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return nil, nil // first time through
	}

	var (
		storedHash string
		storedUser *uuid.UUID
		state      string
		status     *int32
		body       []byte
	)
	err = s.pool.QueryRow(ctx, `
		SELECT request_hash, user_id, state, response_status, response_body
		  FROM idempotency_keys
		 WHERE key = $1 AND endpoint = $2`,
		key, endpoint).Scan(&storedHash, &storedUser, &state, &status, &body)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The row was reaped between the INSERT and this SELECT. Treating
			// it as a fresh request is correct: the TTL has passed, so nothing
			// is being replayed.
			return nil, nil
		}
		return nil, fmt.Errorf("read idempotency key: %w", err)
	}

	// A key belonging to someone else must never return their response. In
	// practice this means a guessed or leaked key leaks nothing.
	if !sameOwner(storedUser, ownerID) {
		return nil, httpx.ErrIdempotencyMismatch
	}

	if storedHash != requestHash {
		return nil, httpx.ErrIdempotencyMismatch
	}

	if state == "in_flight" {
		return nil, ErrInFlight
	}

	if status == nil {
		return nil, nil
	}
	return &httpx.StoredResponse{Status: int(*status), Body: body}, nil
}

func (s *Store) Complete(ctx context.Context, key uuid.UUID, endpoint string, status int, body []byte) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE idempotency_keys
		   SET state = 'completed',
		       response_status = $3,
		       response_body = $4,
		       completed_at = now()
		 WHERE key = $1 AND endpoint = $2`,
		key, endpoint, status, body)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			return fmt.Errorf("complete idempotency key (%s): %w", pgErr.Code, err)
		}
		return fmt.Errorf("complete idempotency key: %w", err)
	}
	return nil
}

func sameOwner(a, b *uuid.UUID) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
