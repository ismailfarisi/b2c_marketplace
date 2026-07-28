package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
)

type ctxKey string

const (
	ctxKeyRequestID ctxKey = "request_id"
	ctxKeyUserID    ctxKey = "user_id"
	ctxKeyLocale    ctxKey = "locale"
)

// RequestID assigns every request a stable identifier, honouring an inbound
// X-Request-Id so a trace survives the hop from the edge proxy.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" || len(id) > 64 {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-Id", id)
		ctx := context.WithValue(r.Context(), ctxKeyRequestID, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(ctxKeyRequestID).(string)
	return id
}

func UserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(ctxKeyUserID).(uuid.UUID)
	return id, ok
}

func WithUserID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, ctxKeyUserID, id)
}

// Locale resolves the response language from Accept-Language, defaulting to
// English. Arabic is a first-class locale from Phase 0, not a later
// localisation pass — retrofitting RTL is materially more expensive than
// carrying it from the start.
func Locale(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		loc := "en"
		if h := r.Header.Get("Accept-Language"); len(h) >= 2 && h[:2] == "ar" {
			loc = "ar"
		}
		w.Header().Set("Content-Language", loc)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyLocale, loc)))
	})
}

func LocaleFromContext(ctx context.Context) string {
	if loc, ok := ctx.Value(ctxKeyLocale).(string); ok {
		return loc
	}
	return "en"
}

// statusRecorder captures the status code so the access log can report it.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

func AccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}

		next.ServeHTTP(rec, r)

		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		slog.InfoContext(r.Context(), "http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes", rec.bytes,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", RequestIDFromContext(r.Context()),
		)
	})
}

// Recover turns a panic into a 500 with a request ID rather than a dropped
// connection, and logs the stack once.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.ErrorContext(r.Context(), "panic recovered",
					"panic", rec,
					"stack", string(debug.Stack()),
					"request_id", RequestIDFromContext(r.Context()),
				)
				WriteJSON(w, http.StatusInternalServerError, "application/problem+json", Problem{
					Type:      "about:blank",
					Title:     "Internal server error",
					Status:    http.StatusInternalServerError,
					RequestID: RequestIDFromContext(r.Context()),
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Timeout bounds every request. Without it a slow dependency converts into
// exhausted connection slots, which reads as a total outage rather than a slow
// endpoint.
func Timeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequestHeaders is the CORS allow-list, shared by every HTTP binary.
//
// One list, because there were two and they drifted. `cmd/feed` permitted
// `X-Device-Id` and `cmd/api` did not — so the same `GET /feed` succeeded from
// the read-path process and failed the preflight from the transactional one,
// and with it every anonymous call a browser makes: the feed, event ingest, and
// `POST /actions/{id}/click`.
//
// That last one is the expensive part. The click is Phase 3's conversion event
// — the measurement the whole video bet is judged on — and `actions.go` requires
// `X-Device-Id` on it for anonymous viewers. It could not be sent from a
// browser at all, so anonymous conversions were unrecorded rather than
// under-recorded, and the number would have read as "reels do not convert".
//
// Found in QA against a real browser. Nothing in the Go tests exercises a
// preflight, and curl does not send one.
var RequestHeaders = []string{
	"Authorization",
	"Content-Type",
	"Accept-Language",
	"Idempotency-Key",
	"X-Request-Id",
	// Identifies an anonymous viewer across requests: it namespaces the
	// seen-set, the interest profile and the click attribution. Without it
	// every visit is a first visit.
	"X-Device-Id",
}

// ResponseHeaders is what browsers are allowed to read back.
var ResponseHeaders = []string{"X-Request-Id", "Idempotent-Replay"}
