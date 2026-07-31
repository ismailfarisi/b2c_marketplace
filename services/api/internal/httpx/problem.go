// Package httpx holds the HTTP boundary concerns shared by every module:
// error rendering, request context, and middleware. It is the only package that
// knows about status codes — modules return typed errors and let this layer map
// them.
package httpx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
)

// Problem is RFC 9457 problem details. One error shape for the whole API, so
// clients write one error handler instead of one per endpoint.
type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
	// Code is a stable machine-readable reason, for the cases where the status
	// alone does not say enough to act on.
	//
	// Added for OTP verification, which is the clearest example of the problem:
	// a wrong digit, a challenge that aged out, and a challenge whose attempts
	// are exhausted are all 401, and a client that cannot tell them apart shows
	// "wrong code" to someone who will never be allowed another guess. They are
	// three different screens.
	//
	// Deliberately not a general-purpose enum. Most errors in this API are
	// adequately described by their status plus, for a 422, their field errors —
	// and a `code` on every response invites clients to branch on strings where
	// a status would do. It is set only where a caller must genuinely behave
	// differently, and every value is documented at the site that produces it.
	Code      string       `json:"code,omitempty"`
	Instance  string       `json:"instance,omitempty"`
	RequestID string       `json:"request_id,omitempty"`
	Errors    []FieldError `json:"errors,omitempty"`
}

type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

// Sentinel errors modules return. The mapping to HTTP lives here and nowhere
// else, so a module never imports net/http to express "not found".
var (
	ErrNotFound            = errors.New("not found")
	ErrUnauthorized        = errors.New("unauthorized")
	ErrForbidden           = errors.New("forbidden")
	ErrConflict            = errors.New("conflict")
	ErrCapacityTaken       = errors.New("capacity exhausted")
	ErrIdempotencyMismatch = errors.New("idempotency key reused with a different body")
)

// ValidationError carries field-level detail. Returned by request validation and
// by strategy implementations rejecting vertical-specific options.
type ValidationError struct {
	Fields []FieldError
}

func (e *ValidationError) Error() string { return "validation failed" }

// CodedError attaches a machine-readable [Problem.Code] to one of the sentinels
// above, without changing which status that sentinel maps to.
//
// Wrapping rather than replacing, so `errors.Is(err, ErrUnauthorized)` keeps
// working everywhere it already does — including in middleware and tests that
// have no idea this type exists.
type CodedError struct {
	// Err must be, or wrap, one of the sentinels. It chooses the status.
	Err error
	// Code is the stable reason string. Keep these short, lowercase and
	// snake_cased, and never reword one once a shipped client reads it.
	Code string
	// Detail is a sentence for a human, and is subject to the same rule as
	// everywhere else here: never internals.
	Detail string
}

func (e *CodedError) Error() string { return e.Code + ": " + e.Err.Error() }
func (e *CodedError) Unwrap() error { return e.Err }

// Coded wraps err with a reason code.
func Coded(err error, code, detail string) error {
	return &CodedError{Err: err, Code: code, Detail: detail}
}

// WriteProblem renders err as problem details, choosing the status from the
// error's identity.
//
// Note what is deliberately *not* here: err.Error() is never sent to the client
// for a 5xx. Internal messages leak schema names, host names and query
// fragments; they belong in the log, keyed by request ID, not in the response.
func WriteProblem(w http.ResponseWriter, r *http.Request, err error) {
	reqID := RequestIDFromContext(r.Context())

	p := Problem{
		Type:      "about:blank",
		Status:    http.StatusInternalServerError,
		Title:     "Internal server error",
		Instance:  r.URL.Path,
		RequestID: reqID,
	}

	var validation *ValidationError

	switch {
	case errors.As(err, &validation):
		p.Status = http.StatusUnprocessableEntity
		p.Title = "Validation failed"
		p.Errors = validation.Fields

	case errors.Is(err, ErrNotFound):
		p.Status = http.StatusNotFound
		p.Title = "Not found"
		p.Detail = sentenceFor(err, ErrNotFound)

	case errors.Is(err, ErrUnauthorized):
		p.Status = http.StatusUnauthorized
		p.Title = "Unauthorized"

	case errors.Is(err, ErrForbidden):
		p.Status = http.StatusForbidden
		p.Title = "Forbidden"

	case errors.Is(err, ErrCapacityTaken):
		p.Status = http.StatusConflict
		p.Title = "Capacity exhausted"
		p.Detail = "The selected time window filled before this order was submitted."

	case errors.Is(err, ErrIdempotencyMismatch):
		p.Status = http.StatusUnprocessableEntity
		p.Title = "Idempotency key reused"
		p.Detail = "This Idempotency-Key was already used with a different request body."

	case errors.Is(err, ErrConflict):
		p.Status = http.StatusConflict
		p.Title = "Conflict"
		p.Detail = sentenceFor(err, ErrConflict)
	}

	// Applied after the switch, so the code rides along with whatever status the
	// wrapped sentinel already chose rather than needing a case of its own for
	// each combination.
	var coded *CodedError
	if errors.As(err, &coded) {
		p.Code = coded.Code
		if coded.Detail != "" {
			p.Detail = coded.Detail
		}
	}

	if p.Status >= 500 {
		slog.ErrorContext(r.Context(), "request failed",
			"error", err,
			"request_id", reqID,
			"path", r.URL.Path,
			"method", r.Method,
		)
	}

	WriteJSON(w, p.Status, "application/problem+json", p)
}

// sentenceFor recovers the sentence an error was written with.
//
// Dozens of places in this codebase build errors as
// fmt.Errorf("%w: <sentence>", sentinel), where the sentence is the whole point.
// "this order is completed, refund it instead of cancelling" tells the operator
// what to do next; "there is no setting called \"nope\" — settings are declared
// in a migration so that a typo cannot create one" explains why a request that
// looks reasonable is being refused. A bare 409 or 404 tells them to file a
// ticket.
//
// Only the outermost wrap is surfaced, and only when the sentinel is *at* the
// front of the chain: that is exactly the "%w: sentence" shape and nothing else.
// An error that picked up the sentinel deeper down — behind a "load order: …"
// wrap that might carry a driver message — falls through to the bare title
// rather than risk putting internals in a response body.
func sentenceFor(err, sentinel error) string {
	prefix := sentinel.Error() + ": "
	msg := err.Error()
	if !strings.HasPrefix(msg, prefix) {
		return ""
	}
	return strings.TrimPrefix(msg, prefix)
}

func WriteJSON(w http.ResponseWriter, status int, contentType string, body any) {
	if contentType == "" {
		contentType = "application/json"
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// Headers are already out; the only thing left is to record it.
		slog.Error("failed to encode response body", "error", err)
	}
}
