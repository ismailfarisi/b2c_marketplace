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
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/api"
	"github.com/marketplace/api/internal/app"
	"github.com/marketplace/api/internal/auth"
	"github.com/marketplace/api/internal/config"
	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/attribution"
	"github.com/marketplace/api/internal/modules/identity"
	"github.com/marketplace/api/internal/modules/ordering"
	"github.com/marketplace/api/internal/modules/payments"
	"github.com/marketplace/api/internal/platform/idempotency"
	"github.com/marketplace/api/internal/platform/postgres"
	"github.com/marketplace/api/internal/platform/redisx"
)

// version is stamped at build time: -ldflags "-X main.version=$(git rev-parse --short HEAD)"
var version = "dev"

// Commission and platform VAT.
//
// Constants because the commission model is still open (docs/README.md decision
// #4) — flat, per-vertical and volume-tiered all imply different tables. One
// hard-coded number is honest about that; inventing a schema for a model nobody
// has chosen is not.
const (
	commissionBP  = 1_500 // 15%
	platformVATBP = 500   // 5% UAE VAT on the platform's own commission
)

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

	// The two seams are constructed and wired once, in app, so all three
	// binaries end up with an identical handler set. Modules register into
	// them; nothing reaches back.
	registries := app.BuildRegistries(pool)

	issuer := auth.NewIssuer(cfg.JWTSigningKey, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	idempotencyStore := idempotency.New(pool)

	// The sandbox gateway approves everything. config.Load has already rejected
	// APP_ENV=production with dev flags on, but this is the one that would move
	// real money, so it gets its own guard rather than trusting that check.
	var provider payments.Provider = payments.SandboxProvider{}
	if cfg.IsProduction() {
		return fmt.Errorf("no production payment provider is configured — " +
			"wire a real gateway before deploying")
	}

	// Redis is optional. Without it the feed repeats and counters stall; the
	// order path is unaffected, which is why a Redis outage must not stop the
	// API from booting.
	rdb, err := redisx.Connect(ctx, cfg.RedisURL)
	if err != nil {
		return err
	}
	if rdb != nil {
		defer rdb.Close()
	}

	// Attribution runs inside the order transaction (see ordering.PlaceOrder
	// step 7): the evidence is time-windowed, so an order that commits without
	// its attribution row can never be attributed afterwards.
	attributionSvc := attribution.NewService(pool)

	handlers := &api.Handlers{
		Identity: identity.NewService(pool, issuer, cfg.OTPTTL, cfg.OTPDevEcho),
		Ordering: ordering.NewService(pool, registries.Verticals, attributionSvc),
		Payments: payments.NewService(pool, provider, commissionBP, platformVATBP),
		Catalog:  api.NewCatalogQueries(pool),
	}

	media, err := app.BuildMedia(ctx, cfg, pool, rdb, registries.Actions)
	if err != nil {
		return err
	}
	if media != nil {
		handlers.Reels = &api.ReelHandlers{
			Reels:       media.Reels,
			Feed:        media.Feed,
			Ingest:      media.Ingest,
			Moderation:  media.Moderation,
			Actions:     media.Actions,
			Attribution: media.Attribution,
		}
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           newRouter(cfg, pool, handlers, issuer, idempotencyStore),
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

func newRouter(
	cfg config.Config,
	pool *pgxpool.Pool,
	handlers *api.Handlers,
	issuer *auth.Issuer,
	idem httpx.IdempotencyStore,
) http.Handler {
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
		api.Mount(r, handlers, issuer, idem)

		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			httpx.WriteProblem(w, r, httpx.ErrNotFound)
		})
	})

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
