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
)

// Problem is RFC 9457 problem details. One error shape for the whole API, so
// clients write one error handler instead of one per endpoint.
type Problem struct {
	Type      string       `json:"type"`
	Title     string       `json:"title"`
	Status    int          `json:"status"`
	Detail    string       `json:"detail,omitempty"`
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
	ErrNotFound        = errors.New("not found")
	ErrUnauthorized    = errors.New("unauthorized")
	ErrForbidden       = errors.New("forbidden")
	ErrConflict        = errors.New("conflict")
	ErrCapacityTaken   = errors.New("capacity exhausted")
	ErrIdempotencyMismatch = errors.New("idempotency key reused with a different body")
)

// ValidationError carries field-level detail. Returned by request validation and
// by strategy implementations rejecting vertical-specific options.
type ValidationError struct {
	Fields []FieldError
}

func (e *ValidationError) Error() string { return "validation failed" }

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
