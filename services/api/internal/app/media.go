// Package app holds wiring shared by the three binaries.
//
// It exists because cmd/api, cmd/feed and cmd/worker need overlapping subsets
// of the same dependency graph, and three copies of that graph drift — usually
// by one binary getting a stub provider that the others do not, which is
// invisible until content is published unmoderated.
package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/marketplace/api/internal/config"
	"github.com/marketplace/api/internal/modules/actions"
	"github.com/marketplace/api/internal/modules/attribution"
	"github.com/marketplace/api/internal/modules/engagement"
	"github.com/marketplace/api/internal/modules/feed"
	"github.com/marketplace/api/internal/modules/media"
	"github.com/marketplace/api/internal/modules/moderation"
	"github.com/marketplace/api/internal/modules/pipeline"
	"github.com/marketplace/api/internal/modules/reels"
	"github.com/marketplace/api/internal/modules/search"
	"github.com/marketplace/api/internal/platform/objectstore"
	"github.com/marketplace/api/internal/seam/action"
)

// Media is the Phase 2 dependency graph, extended in Phase 3 with the action
// layer and attribution.
type Media struct {
	Store      *objectstore.Store
	Reels      *reels.Service
	Feed       *feed.Service
	Moderation *moderation.Service
	ModRepo    *moderation.Repository
	Counters   *engagement.Counters
	Ingest     *engagement.Ingest
	Pipeline   *pipeline.Runner
	Indexer    search.Indexer

	Actions     *actions.Service
	Attribution *attribution.Service
}

// BuildMedia constructs everything the media stack needs.
//
// Returns nil, nil when no object store is configured. The transactional API
// then runs without the media surface rather than failing to boot — a
// deployment that only serves orders is a legitimate configuration, and the
// alternative is that a missing bucket name takes down checkout.
func BuildMedia(ctx context.Context, cfg config.Config, pool *pgxpool.Pool, rdb *redis.Client, registry *action.Registry) (*Media, error) {
	if cfg.S3Bucket == "" {
		slog.Warn("media disabled: S3_BUCKET is not set")
		return nil, nil
	}

	store, err := objectstore.New(ctx, objectstore.Config{
		Region:         cfg.S3Region,
		Bucket:         cfg.S3Bucket,
		Endpoint:       cfg.S3Endpoint,
		AccessKeyID:    cfg.S3AccessKeyID,
		SecretKey:      cfg.S3SecretKey,
		ForcePathStyle: cfg.S3ForcePathStyle,
	})
	if err != nil {
		return nil, err
	}

	reelSvc := reels.NewService(pool, store)
	modRepo := moderation.NewRepository(pool)

	prober, err := buildProber(cfg)
	if err != nil {
		return nil, err
	}
	transcoder, err := buildTranscoder(cfg)
	if err != nil {
		return nil, err
	}
	vision, text, err := buildModerators(cfg)
	if err != nil {
		return nil, err
	}

	var counters *engagement.Counters
	var seen feed.SeenStore = feed.NoopSeenStore{}
	if rdb != nil {
		counters = engagement.NewCounters(rdb, pool)
		seen = feed.NewRedisSeenStore(rdb)
	} else {
		// Stated rather than silent: without Redis the feed repeats itself and
		// engagement counts do not move. Both are survivable; neither should be
		// discovered by a confused analyst.
		slog.Warn("redis not configured: feed will repeat and counters will not update")
	}

	var counterSource feed.CounterSource
	if counters != nil {
		counterSource = counters
	}

	var indexer search.Indexer = search.NoopIndexer{}
	if cfg.OpenSearchURL != "" {
		indexer = search.NewClient(cfg.OpenSearchURL, cfg.OpenSearchIndex)
	}

	m := &Media{
		Store:      store,
		Reels:      reelSvc,
		Feed:       feed.NewService(pool, seen, counterSource),
		Moderation: moderation.NewService(pool, modRepo, reelSvc),
		ModRepo:    modRepo,
		Counters:   counters,
		Indexer:    indexer,
	}

	// The actions service borrows the reels service's branch authorisation
	// rather than reimplementing it. Two copies of an authorisation rule is one
	// too many, and the one that drifts is always the one nobody is watching.
	m.Actions = actions.NewService(pool, registry, reelSvc.Repo())
	m.Attribution = attribution.NewService(pool)

	m.Ingest = engagement.NewIngest(engagement.NewPostgresSink(pool), counters)
	m.Pipeline = pipeline.NewRunner(pipeline.Deps{
		Pool:       pool,
		Reels:      reelSvc,
		Moderation: modRepo,
		Store:      store,
		Prober:     prober,
		Transcoder: transcoder,
		Moderator:  moderation.Pipeline{Vision: vision, Text: text},
	})

	return m, nil
}

func buildProber(cfg config.Config) (media.Prober, error) {
	if cfg.FFProbeBinary == "stub" {
		if cfg.IsProduction() {
			return nil, fmt.Errorf("FFPROBE_BINARY=stub is not permitted in production")
		}
		return media.StubProber{}, nil
	}
	return media.FFProbe{Binary: cfg.FFProbeBinary}, nil
}

func buildTranscoder(cfg config.Config) (media.TranscodeProvider, error) {
	switch cfg.TranscodeVendor {
	case "stub", "":
		// The guard is here and not in a comment: a stub transcoder in
		// production publishes reels whose playlists point at nothing.
		if cfg.IsProduction() {
			return nil, fmt.Errorf("TRANSCODE_VENDOR=stub is not permitted in production")
		}
		return media.StubProvider{BaseURL: cfg.MediaCDNBaseURL}, nil
	default:
		return nil, fmt.Errorf("transcode vendor %q is not implemented — "+
			"implement media.TranscodeProvider and register it here", cfg.TranscodeVendor)
	}
}

// buildModerators resolves the moderation layers.
//
// The production guard is the important part. A stub vision moderator returns
// no labels, which Decide reads as approve — so a misconfigured production
// deployment would not fail, it would silently publish everything. That failure
// is invisible until it is a screenshot, so it is refused at boot instead.
func buildModerators(cfg config.Config) (moderation.VisionModerator, moderation.TextModerator, error) {
	var text moderation.TextModerator = moderation.StubTextModerator{}
	if cfg.AnthropicAPIKey != "" {
		claude, err := moderation.NewClaudeModerator(moderation.ClaudeConfig{
			APIKey: cfg.AnthropicAPIKey,
			Model:  cfg.ModerationModel,
		})
		if err != nil {
			return nil, nil, err
		}
		text = claude
	} else if cfg.IsProduction() {
		return nil, nil, fmt.Errorf("ANTHROPIC_API_KEY is required in production — " +
			"the stub text moderator approves everything")
	}

	// Rekognition lands with the AWS integration. Until then the vision layers
	// are stubs, and production refuses to run with them.
	var vision moderation.VisionModerator = moderation.StubVisionModerator{}
	if cfg.IsProduction() {
		return nil, nil, fmt.Errorf("no vision moderator is implemented — " +
			"wire Rekognition before deploying")
	}

	return vision, text, nil
}
