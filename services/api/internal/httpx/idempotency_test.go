package httpx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// memStore is the contract of the Postgres store without the database:
// in-flight claims, completed replays, and releases.
type memStore struct {
	mu   sync.Mutex
	rows map[string]*memRow
}

type memRow struct {
	hash     string
	inFlight bool
	resp     *StoredResponse
}

func newMemStore() *memStore { return &memStore{rows: map[string]*memRow{}} }

func (m *memStore) Begin(_ context.Context, key, _ uuid.UUID, endpoint, hash string) (*StoredResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := key.String() + endpoint
	row, ok := m.rows[id]
	if !ok {
		m.rows[id] = &memRow{hash: hash, inFlight: true}
		return nil, nil
	}
	if row.hash != hash {
		return nil, ErrIdempotencyMismatch
	}
	if row.inFlight {
		return nil, Coded(ErrConflict, "idempotency_in_flight", "")
	}
	return row.resp, nil
}

func (m *memStore) Complete(_ context.Context, key uuid.UUID, endpoint string, status int, body []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row := m.rows[key.String()+endpoint]
	row.inFlight = false
	row.resp = &StoredResponse{Status: status, Body: append([]byte(nil), body...)}
	return nil
}

func (m *memStore) Release(_ context.Context, key uuid.UUID, endpoint string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if row, ok := m.rows[key.String()+endpoint]; ok && row.inFlight {
		delete(m.rows, key.String()+endpoint)
	}
	return nil
}

func (m *memStore) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.rows)
}

func send(h http.Handler, key uuid.UUID, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/v1/companies/c/lunch", strings.NewReader(body))
	r.Header.Set("Idempotency-Key", key.String())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestIdempotencyRetriesAfterBusinessErrorReExecute(t *testing.T) {
	store := newMemStore()
	calls, status := 0, http.StatusConflict
	h := Idempotency(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if status != http.StatusCreated {
			WriteProblem(w, r, Coded(ErrConflict, "over_credit_limit", ""))
			return
		}
		WriteJSON(w, status, "", map[string]int{"call": calls})
	}))
	key := uuid.New()

	if w := send(h, key, `{"a":1}`); w.Code != http.StatusConflict {
		t.Fatalf("first: status %d", w.Code)
	}
	if store.count() != 0 {
		t.Fatalf("a refused request must release its key")
	}
	if w := send(h, key, `{"a":1}`); w.Code != http.StatusConflict || calls != 2 {
		t.Fatalf("retry after 409 should re-execute: status %d calls %d body %s", w.Code, calls, w.Body)
	}

	status = http.StatusCreated
	first := send(h, key, `{"a":1}`)
	if first.Code != http.StatusCreated || calls != 3 {
		t.Fatalf("success: status %d calls %d", first.Code, calls)
	}
	replay := send(h, key, `{"a":1}`)
	if replay.Code != http.StatusCreated || calls != 3 || replay.Header().Get("Idempotent-Replay") != "true" {
		t.Fatalf("success must replay: status %d calls %d", replay.Code, calls)
	}
	if replay.Body.String() != first.Body.String() {
		t.Fatalf("replay body %q, want %q", replay.Body, first.Body)
	}
}

func TestIdempotencyReleasesOnServerErrorAndPanic(t *testing.T) {
	store := newMemStore()
	h := Idempotency(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Panic") != "" {
			panic("boom")
		}
		WriteProblem(w, r, context.DeadlineExceeded)
	}))
	if w := send(h, uuid.New(), `{}`); w.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", w.Code)
	}
	if store.count() != 0 {
		t.Fatalf("a 5xx must release its key")
	}

	r := httptest.NewRequest(http.MethodPost, "/v1/x", strings.NewReader(`{}`))
	r.Header.Set("Idempotency-Key", uuid.NewString())
	r.Header.Set("X-Panic", "1")
	func() {
		defer func() { _ = recover() }()
		h.ServeHTTP(httptest.NewRecorder(), r)
	}()
	if store.count() != 0 {
		t.Fatalf("a panicking handler must release its key")
	}
}

func TestIdempotencyInFlightIsCoded(t *testing.T) {
	store := newMemStore()
	key := uuid.New()
	store.rows[key.String()+"POST /v1/companies/c/lunch"] = &memRow{hash: hashOf(`{}`), inFlight: true}
	h := Idempotency(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("must not execute while a twin is in flight")
	}))
	w := send(h, key, `{}`)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"code":"idempotency_in_flight"`) {
		t.Fatalf("status %d body %s", w.Code, w.Body)
	}
}

func TestIdempotencyMismatchIsCoded(t *testing.T) {
	store := newMemStore()
	h := Idempotency(store)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusCreated, "", map[string]bool{"ok": true})
	}))
	key := uuid.New()
	send(h, key, `{"a":1}`)
	w := send(h, key, `{"a":2}`)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), `"code":"idempotency_key_reused"`) {
		t.Fatalf("status %d body %s", w.Code, w.Body)
	}
}

func hashOf(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}
