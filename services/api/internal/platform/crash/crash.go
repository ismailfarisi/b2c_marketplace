// Package crash reports panics and fatal errors to Sentry when a DSN is set.
// Every function is a no-op until Init succeeds, so callers need no checks.
package crash

import (
	"log/slog"
	"time"

	"github.com/getsentry/sentry-go"
)

// Init starts the Sentry client and returns a flush to defer in main.
func Init(dsn, env, service, release string) (flush func()) {
	if dsn == "" {
		return func() {}
	}
	err := sentry.Init(sentry.ClientOptions{
		Dsn:         dsn,
		Environment: env,
		Release:     release,
		ServerName:  service,
		// Request bodies and headers carry phone numbers and tokens.
		SendDefaultPII: false,
	})
	if err != nil {
		slog.Error("crash reporting disabled", "error", err)
		return func() {}
	}
	return func() { sentry.Flush(2 * time.Second) }
}

// Panic reports a recovered panic, tagged with the request id when there is one.
func Panic(rec any, requestID string) {
	hub := sentry.CurrentHub().Clone()
	if requestID != "" {
		hub.Scope().SetTag("request_id", requestID)
	}
	hub.Recover(rec)
}

// Error reports an error that ends the process.
func Error(err error) {
	sentry.CaptureException(err)
}
