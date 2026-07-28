// Command api is the transactional monolith.
//
// One process, module boundaries enforced by package structure rather than by
// network calls. The one split the plan calls for is the feed read path
// (PLAN-VIDEO-FIRST §3) — it arrives in Phase 2 as cmd/feed, not as a
// premature extraction of anything here.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/config"
	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/platform/postgres"
	"github.com/marketplace/api/internal/seam/action"
	"github.com/marketplace/api/internal/seam/servicetype"
)

// version is stamped at build time: -ldflags "-X main.version=$(git rev-parse --short HEAD)"
var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	setupLogging(cfg)

	// Signal handling wraps everything so that a Ctrl-C during startup is as
	// clean as one during serving.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.Connect(ctx, postgres.Config{
		URL:             cfg.DatabaseURL,
		MaxConns:        20,
		MinConns:        2,
		MaxConnLifetime: time.Hour,
		MaxConnIdleTime: 30 * time.Minute,
	})
	if err != nil {
		return err
	}
	defer pool.Close()

	// The two seams and the strategy registry are constructed once and handed
	// to modules. Modules register into them; nothing reaches back.
	actions := action.NewRegistry()
	verticals := servicetype.NewRegistry()

	// Phase 1 registers laundry here as the walking skeleton:
	//
	//	laundry.Register(verticals, actions, pool)
	//
	// Note for whoever picks this up: laundry is the *engineering* skeleton, not
	// a launch decision. PLAN-VIDEO-FIRST §5 ranks it last for feed suitability
	// and no phase in §8 ships it to customers — see docs/README.md open
	// decision #3, which is still unresolved.
	_ = verticals

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           newRouter(cfg, pool, actions),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// Generous relative to ReadTimeout: presigned-upload responses are
		// small, but the ledger endpoints do real work under load.
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("http listening", "addr", cfg.HTTPAddr, "env", cfg.Env, "version", version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutdown signal received")
	}

	// Drain in-flight requests. Keep this comfortably under the orchestrator's
	// termination grace period or the pod is SIGKILLed mid-drain and in-flight
	// orders die with it.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	slog.Info("shutdown complete")
	return nil
}

func newRouter(cfg config.Config, pool *pgxpool.Pool, actions *action.Registry) http.Handler {
	r := chi.NewRouter()

	r.Use(httpx.RequestID)
	r.Use(httpx.Recover)
	r.Use(httpx.AccessLog)
	r.Use(httpx.Locale)
	r.Use(httpx.Timeout(25 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins(cfg),
		AllowedMethods:   []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "Idempotency-Key", "Accept-Language", "X-Request-Id"},
		ExposedHeaders:   []string{"X-Request-Id", "Idempotent-Replay"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// Health endpoints are hand-written rather than generated: they must answer
	// even when the generated boundary or its dependencies are broken, which is
	// precisely when you need them.
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
			"status":  "ok",
			"version": version,
		})
	})

	r.Get("/readyz", readiness(pool))

	r.Route("/v1", func(r chi.Router) {
		// Generated boundary mounts here once `make gen-go` has run:
		//
		//	openapi.HandlerFromMuxWithBaseURL(strictHandler, r, "")
		//
		// It is intentionally absent from source control until the first
		// handler exists — a generated file with no implementation behind it
		// is a compile error that teaches nothing.
		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			httpx.WriteProblem(w, r, httpx.ErrNotFound)
		})
	})

	_ = actions
	return r
}

func readiness(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		checks := map[string]string{}
		status := http.StatusOK
		overall := "ok"

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			checks["postgres"] = "unreachable"
			status = http.StatusServiceUnavailable
			overall = "down"
		} else {
			checks["postgres"] = "ok"
		}

		httpx.WriteJSON(w, status, "", map[string]any{
			"status":  overall,
			"version": version,
			"checks":  checks,
		})
	}
}

func allowedOrigins(cfg config.Config) []string {
	if cfg.IsProduction() {
		// Tightened per environment at deploy time. A wildcard here would be a
		// silent downgrade of every cookie and CORS protection in the product.
		return []string{"https://app.example.ae"}
	}
	return []string{"http://localhost:3000", "http://localhost:8081", "exp://*"}
}

func setupLogging(cfg config.Config) {
	level := slog.LevelInfo
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	var handler slog.Handler
	opts := &slog.HandlerOptions{Level: level}
	if cfg.IsProduction() {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	slog.SetDefault(slog.New(handler).With("service", "api", "version", version))
}
