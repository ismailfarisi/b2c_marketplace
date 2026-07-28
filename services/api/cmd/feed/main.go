// Command feed serves the read path.
//
// The one split PLAN-VIDEO-FIRST §3 calls for, and the reason it earns its
// keep: feed reads scale with viewers and order writes scale with buyers, and
// in a video-first product those two numbers differ by orders of magnitude.
// Sharing one process means autoscaling the transactional core — connection
// pool, payment gateway clients, ledger — on the traffic of people who are only
// watching.
//
// It is deliberately not a microservice split of the domain. This binary owns
// no tables and no writes to reels; it reads the same Postgres the API writes,
// and it can be deleted tomorrow by mounting the same routes in cmd/api.
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

	"github.com/marketplace/api/internal/api"
	"github.com/marketplace/api/internal/app"
	"github.com/marketplace/api/internal/auth"
	"github.com/marketplace/api/internal/config"
	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/boosts"
	"github.com/marketplace/api/internal/platform/metrics"
	"github.com/marketplace/api/internal/platform/postgres"
	"github.com/marketplace/api/internal/platform/redisx"
)

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

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)).
		With("service", "feed", "version", version))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// A larger pool than the API's despite doing less work per request: this
	// process serves the highest-QPS endpoint in the product, and its queries
	// are short.
	pool, err := postgres.Connect(ctx, postgres.Config{
		URL:             cfg.DatabaseURL,
		MaxConns:        40,
		MinConns:        4,
		MaxConnLifetime: time.Hour,
		MaxConnIdleTime: 30 * time.Minute,
	})
	if err != nil {
		return err
	}
	defer pool.Close()

	rdb, err := redisx.Connect(ctx, cfg.RedisURL)
	if err != nil {
		return err
	}
	if rdb != nil {
		defer rdb.Close()
	}

	media, err := app.BuildMedia(ctx, cfg, pool, rdb, app.BuildRegistries(pool).Actions)
	if err != nil {
		return err
	}
	if media == nil {
		return errors.New("feed service requires media configuration (S3_BUCKET)")
	}

	// Phase 9. Wired identically to cmd/api, and it has to be: this process
	// serves most of the impressions in the product, so a boost system attached
	// only to the transactional binary would sell reach that is almost never
	// delivered — and would look, from the campaign report, exactly like a
	// campaign nobody wanted to watch.
	boostSvc := boosts.NewService(pool, nil)
	media.Feed.WithSponsored(app.SponsoredFiller{Boosts: boostSvc})

	issuer := auth.NewIssuer(cfg.JWTSigningKey, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	handlers := &api.ReelHandlers{
		Reels:  media.Reels,
		Feed:   media.Feed,
		Boosts: boostSvc,
		// Actions is what makes a feed served here identical to one served by
		// cmd/api: the same registry, the same handlers, the same CTAs. A feed
		// without it would render reels with no buttons, which is the whole
		// product minus the part that earns money.
		Actions: media.Actions,
		Ingest:  media.Ingest,
		// Likes is read-only here: StateFor answers "have I liked this" for the
		// page. The write endpoints stay on cmd/api — a like is a small write
		// and the read path should not grow one.
		Likes:    media.Likes,
		Comments: media.Comments,
		Ranking:  media.Ranking,
	}

	r := chi.NewRouter()
	r.Use(httpx.RequestID)
	// Phase 10. The feed process is the one whose latency is the product
	// rather than a quality of it, so it carries the same RED middleware as
	// cmd/api and a `source` label that tells the two apart on one dashboard.
	r.Use(metrics.Middleware)
	r.Use(httpx.Recover)
	r.Use(httpx.AccessLog)
	r.Use(httpx.Locale)
	// Tighter than the API's 25s. A feed request that takes eight seconds has
	// already failed the user — they have swiped away — so holding the
	// connection open only consumes capacity that a fresh request could use.
	r.Use(httpx.Timeout(8 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: allowedOrigins(cfg),
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: httpx.RequestHeaders,
		ExposedHeaders: httpx.ResponseHeaders,
		MaxAge:         300,
	}))

	metrics.BindPool(metrics.Default, pool, "feed")
	r.Get("/metrics", metrics.Handler(metrics.Default))

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"status": "ok", "version": version})
	})
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		checkCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(checkCtx); err != nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, "", map[string]any{
				"status": "down", "checks": map[string]string{"postgres": "unreachable"},
			})
			return
		}
		// Redis is deliberately not a readiness gate. The feed degrades without
		// it; taking the pod out of rotation would turn a degradation into an
		// outage.
		httpx.WriteJSON(w, http.StatusOK, "", map[string]any{
			"status": "ok", "checks": map[string]string{"postgres": "ok"},
		})
	})

	r.Route("/v1", func(r chi.Router) {
		api.MountFeed(r, handlers, issuer)
		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			httpx.WriteProblem(w, r, httpx.ErrNotFound)
		})
	})

	srv := &http.Server{
		Addr:              feedAddr(cfg),
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("feed listening", "addr", srv.Addr, "env", cfg.Env)
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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// feedAddr defaults to :8081 so both binaries run on one machine in
// development without an extra environment variable to remember.
func feedAddr(cfg config.Config) string {
	if addr := os.Getenv("FEED_HTTP_ADDR"); addr != "" {
		return addr
	}
	return ":8081"
}

func allowedOrigins(cfg config.Config) []string {
	if cfg.IsProduction() {
		return []string{"https://app.example.ae"}
	}
	return []string{"http://localhost:3000", "http://localhost:8081", "exp://*"}
}
