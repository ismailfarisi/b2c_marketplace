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
	"github.com/marketplace/api/internal/modules/admin"
	"github.com/marketplace/api/internal/modules/attribution"
	"github.com/marketplace/api/internal/modules/boosts"
	"github.com/marketplace/api/internal/modules/chat"
	"github.com/marketplace/api/internal/modules/classifieds"
	"github.com/marketplace/api/internal/modules/console"
	"github.com/marketplace/api/internal/modules/dispatch"
	"github.com/marketplace/api/internal/modules/driver"
	"github.com/marketplace/api/internal/modules/identity"
	"github.com/marketplace/api/internal/modules/invoicing"
	"github.com/marketplace/api/internal/modules/kyc"
	"github.com/marketplace/api/internal/modules/moderation"
	"github.com/marketplace/api/internal/modules/ordering"
	"github.com/marketplace/api/internal/modules/payments"
	"github.com/marketplace/api/internal/modules/payouts"
	"github.com/marketplace/api/internal/modules/subscriptions"
	"github.com/marketplace/api/internal/modules/tax"
	"github.com/marketplace/api/internal/modules/trust"
	"github.com/marketplace/api/internal/modules/vehicles"
	"github.com/marketplace/api/internal/platform/idempotency"
	"github.com/marketplace/api/internal/platform/metrics"
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
		// The same commission numbers the order path uses. A subscription meal
		// and a one-off meal must not earn the platform different margins, and
		// two constants would drift the first time one of them was tuned.
		Subscriptions: &api.SubscriptionHandlers{
			Service: subscriptions.NewService(pool, registries.Verticals, subscriptions.Config{
				CommissionBP:    commissionBP,
				PlatformVATBP:   platformVATBP,
				ReserveCapacity: true,
			}),
		},
		Catalog: api.NewCatalogQueries(pool),
	}

	// Phase 7 — logistics and the console.
	//
	// `dispatch` and `driver` take a pool and a handful of numbers and nothing
	// else. Neither is handed a vertical, a registry or a catalogue, which is
	// the Fulfillment primitive's claim expressed as a constructor signature.
	dispatchSvc := dispatch.NewService(pool, dispatch.DefaultOptions())

	// Phase 8 — the notifier the driver module was written against but had no
	// implementation for. Handed in as an interface: `driver` still knows one
	// sentence about notifications, and it is the constructor signature that
	// says so.
	notifySvc := app.BuildNotify(cfg, pool)

	driverSvc := driver.NewService(pool, driver.Config{
		CommissionBP:  commissionBP,
		PlatformVATBP: platformVATBP,
	}, app.DeliveryCodeNotifier{Notify: notifySvc})
	consoleSvc := console.NewService(pool)
	// Phase 8 — payouts now run through the compliance gate. Handed the gate
	// rather than a flag: there is one path to a bank transfer in this system
	// and the check sits across it, so a future caller cannot reach the
	// transfer without passing through the check.
	kycSvc := kyc.NewService(pool)
	payoutsSvc := payouts.NewService(pool, nil, app.PayoutGate{KYC: kycSvc})
	adminSvc := admin.NewService(pool)

	handlers.DriverOps = &api.DriverHandlers{Driver: driverSvc, Dispatch: dispatchSvc}
	handlers.Console = &api.ConsoleHandlers{
		Console: consoleSvc,
		Payouts: payoutsSvc,
		Driver:  driverSvc,
		Admin:   adminSvc,
	}

	// Phase 8. `supplierIsVendor` is docs/README.md decision #4 expressed as a
	// boolean: it decides whose TRN goes on the customer's invoice, and the two
	// answers produce different legal documents from the same order. True is
	// the assumption the ledger already makes in PostOrderCaptured — the
	// vendor is the supplier of record — so the two are set from the same
	// belief rather than drifting apart.
	handlers.Compliance = &api.ComplianceHandlers{
		Notify:   notifySvc,
		Invoices: invoicing.NewService(pool, tax.NewService(pool), true),
		KYC:      kycSvc,
		Strikes:  moderation.NewStrikeService(pool),
		Admin:    adminSvc,
		Vendors:  handlers.Console,
	}

	// Phase 9 — boosts.
	//
	// Constructed unconditionally and mounted unconditionally, but it changes
	// nothing until a campaign exists: `Fill` returns no winners when no
	// campaign is active, and the feed serves the organic page it would have
	// served anyway.
	boostSvc := boosts.NewService(pool, nil)
	handlers.BoostOps = &api.BoostHandlers{
		Boosts:  boostSvc,
		Admin:   adminSvc,
		Console: handlers.Console,
	}

	// Phase 6 — classifieds. Constructed here rather than in app.BuildMedia
	// because none of it depends on the media stack: a deployment with no
	// object store still sells cars, it just sells them without video.
	//
	// The wiring reads as the dependency inversion the modules were written
	// for. `vehicles` is handed three sentences — may this seller publish, open
	// a thread, record this evidence — and never learns which packages exist,
	// how a message is scanned, or how a fraud score is computed.
	trustSvc := trust.NewService(pool)
	chatSvc := chat.NewService(pool, trustSvc)
	packagesSvc := classifieds.NewService(pool)
	vehicleSvc := vehicles.NewService(pool, packagesSvc, chatSvc, trustSvc)

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

			Likes:     media.Likes,
			Comments:  media.Comments,
			Ranking:   media.Ranking,
			Interests: media.Interests,
		}
		// Paid slots. Attached to the feed after it is constructed rather than
		// passed to the constructor, because a deployment with no advertising
		// is the normal one for a long time and threading a nil through
		// app.BuildMedia to say so would be worse than a setter.
		media.Feed.WithSponsored(app.SponsoredFiller{Boosts: boostSvc})
		handlers.Reels.Boosts = boostSvc

		// Login is the one moment an anonymous profile can be carried into an
		// account. A viewer browses anonymously, builds a real interest profile
		// over three sessions, then signs in to place their first order —
		// without this the account starts empty and the first personalised feed
		// they ever see is the worst one they will get.
		handlers.Interests = media.Interests
	}

	// Moderation is the one Phase 6 dependency that does come from the media
	// stack: listing review, held messages and the fraud queue all authorise
	// against platform_admins through it. Without media there is no moderation
	// service, and rather than mount those routes with a nil staff check —
	// which would make the fraud queue world-readable — the whole classifieds
	// surface stays down. A marketplace that cannot review listings should not
	// be publishing them.
	if media != nil {
		handlers.Vehicles = &api.VehicleHandlers{
			Listings:   vehicleSvc,
			Packages:   packagesSvc,
			Trust:      trustSvc,
			Moderation: media.Moderation,
		}
		handlers.Chat = &api.ChatHandlers{
			Chat:       chatSvc,
			Moderation: media.Moderation,
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
	// After RequestID so a panic still carries one, and before Recover so a
	// request that panics is still counted — an endpoint that only 500s is
	// invisible in a metric that skips it.
	r.Use(metrics.Middleware)
	r.Use(httpx.Recover)
	r.Use(httpx.AccessLog)
	r.Use(httpx.Locale)
	r.Use(httpx.Timeout(25 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins(cfg),
		AllowedMethods:   []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   httpx.RequestHeaders,
		ExposedHeaders:   httpx.ResponseHeaders,
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

	// Phase 10 — the scrape endpoint.
	//
	// Outside /v1, and outside the auth and CORS middleware, for the same
	// reason /healthz is: it is called by infrastructure rather than by
	// clients, it has no versioning contract, and requiring a token would mean
	// monitoring stops working after a credential rotation nobody connected to
	// it. Network policy is what keeps it private, not application auth.
	metrics.BindPool(metrics.Default, pool, "api")
	r.Get("/metrics", metrics.Handler(metrics.Default))

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
