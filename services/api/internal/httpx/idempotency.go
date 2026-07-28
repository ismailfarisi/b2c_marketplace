package httpx

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
)

// IdempotencyStore persists the outcome of a keyed request.
//
// Implemented against the `idempotency_keys` table. It is a table and not a
// Redis key because the guarantee has to survive a cache eviction: a mobile
// client on a flaky network retries aggressively, and a double-charge is not
// recoverable by apology.
type IdempotencyStore interface {
	// Begin claims the key. It returns the stored response when this key has
	// already completed, or ErrIdempotencyMismatch when the same key arrives
	// with a different body.
	Begin(ctx context.Context, key uuid.UUID, userID uuid.UUID, endpoint, requestHash string) (*StoredResponse, error)
	// Complete records the response so a later replay can return it verbatim.
	Complete(ctx context.Context, key uuid.UUID, status int, body []byte) error
}

type StoredResponse struct {
	Status int
	Body   []byte
}

const maxIdempotentBody = 1 << 20 // 1 MiB

// Idempotency short-circuits replays of a mutating request.
//
// Scope note: the key is namespaced by (user, endpoint). The same client-side
// UUID sent to two different endpoints is two different operations, and a key
// must never let one user read another user's stored response.
func Idempotency(store IdempotencyStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			raw := r.Header.Get("Idempotency-Key")
			if raw == "" {
				next.ServeHTTP(w, r)
				return
			}

			key, err := uuid.Parse(raw)
			if err != nil {
				WriteProblem(w, r, &ValidationError{Fields: []FieldError{{
					Field: "Idempotency-Key", Code: "invalid", Message: "must be a UUID",
				}}})
				return
			}

			userID, _ := UserIDFromContext(r.Context())

			body, err := io.ReadAll(io.LimitReader(r.Body, maxIdempotentBody))
			if err != nil {
				WriteProblem(w, r, err)
				return
			}
			_ = r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(body))

			sum := sha256.Sum256(body)
			hash := hex.EncodeToString(sum[:])

			stored, err := store.Begin(r.Context(), key, userID, r.Method+" "+r.URL.Path, hash)
			if err != nil {
				WriteProblem(w, r, err)
				return
			}
			if stored != nil {
				w.Header().Set("Idempotent-Replay", "true")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(stored.Status)
				_, _ = w.Write(stored.Body)
				return
			}

			buf := &bytes.Buffer{}
			rec := &capturingWriter{ResponseWriter: w, buf: buf}
			next.ServeHTTP(rec, r)

			// Only successful outcomes are worth replaying. Storing a 500 would
			// pin a transient failure to the key forever — the client should be
			// allowed to retry into a healthy process.
			if rec.status >= 200 && rec.status < 400 {
				if err := store.Complete(r.Context(), key, rec.status, buf.Bytes()); err != nil {
					// The response is already on the wire; surfacing this now
					// would corrupt it. Log and move on — the cost is that one
					// replay re-executes, which the handlers tolerate.
					slog.ErrorContext(r.Context(), "failed to store idempotent response",
						"error", err,
						"key", key,
						"request_id", RequestIDFromContext(r.Context()),
					)
				}
			}
		})
	}
}

type capturingWriter struct {
	http.ResponseWriter
	buf    *bytes.Buffer
	status int
}

func (c *capturingWriter) WriteHeader(code int) {
	c.status = code
	c.ResponseWriter.WriteHeader(code)
}

func (c *capturingWriter) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	c.buf.Write(b)
	return c.ResponseWriter.Write(b)
}

// DecodeJSON reads and validates a JSON request body, rejecting unknown fields
// so a client typo becomes a 422 instead of a silently ignored parameter.
func DecodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxIdempotentBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return &ValidationError{Fields: []FieldError{{
			Field: "body", Code: "malformed", Message: err.Error(),
		}}}
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return &ValidationError{Fields: []FieldError{{
			Field: "body", Code: "trailing_data", Message: "body must contain a single JSON object",
		}}}
	}
	return nil
}
