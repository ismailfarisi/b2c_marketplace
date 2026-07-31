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
	"github.com/marketplace/api/internal/modules/comments"
	"github.com/marketplace/api/internal/modules/engagement"
	"github.com/marketplace/api/internal/modules/feed"
	"github.com/marketplace/api/internal/modules/interests"
	"github.com/marketplace/api/internal/modules/media"
	"github.com/marketplace/api/internal/modules/moderation"
	"github.com/marketplace/api/internal/modules/pipeline"
	"github.com/marketplace/api/internal/modules/ranking"
	"github.com/marketplace/api/internal/modules/reels"
	"github.com/marketplace/api/internal/modules/search"
	"github.com/marketplace/api/internal/modules/social"
	"github.com/marketplace/api/internal/modules/vehicles"
	"github.com/marketplace/api/internal/platform/objectstore"
)

// Media is the Phase 2 dependency graph, extended in Phase 3 with the action
// layer and attribution, and in Phase 4 with ranking, social and the analytics
// warehouse.
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

	// Phase 4.
	Ranking   *ranking.DBProvider
	Interests *interests.Service
	Likes     *social.Likes
	Comments  *comments.Service
	// ClickHouse is nil unless CLICKHOUSE_URL is set. Held on the struct so the
	// worker can flush it and log what it dropped — a sink that loses events
	// silently is a conversion rate that is wrong silently.
	ClickHouse *engagement.ClickHouseSink
}

// BuildMedia constructs everything the media stack needs.
//
// Returns nil, nil when no object store is configured. The transactional API
// then runs without the media surface rather than failing to boot — a
// deployment that only serves orders is a legitimate configuration, and the
// alternative is that a missing bucket name takes down checkout.
func BuildMedia(ctx context.Context, cfg config.Config, pool *pgxpool.Pool, rdb *redis.Client, registries Registries) (*Media, error) {
	if cfg.S3Bucket == "" {
		slog.Warn("media disabled: S3_BUCKET is not set")
		return nil, nil
	}

	store, err := objectstore.New(ctx, objectstore.Config{
		Region:         cfg.S3Region,
		Bucket:         cfg.S3Bucket,
		Endpoint:       cfg.S3Endpoint,
		PublicEndpoint: cfg.S3PublicEndpoint,
		AccessKeyID:    cfg.S3AccessKeyID,
		SecretKey:      cfg.S3SecretKey,
		ForcePathStyle: cfg.S3ForcePathStyle,
	})
	if err != nil {
		return nil, err
	}

	// Seller reels (Phase 6). The media pipeline gains one interface, not a
	// vertical: it learns that some subjects have owners, never that some
	// subjects are cars.
	reelSvc := reels.NewService(pool, store, vehicles.NewSubjectAuthorizer(pool))
	modRepo := moderation.NewRepository(pool)

	prober, err := buildProber(cfg)
	if err != nil {
		return nil, err
	}
	transcoder, err := buildTranscoder(cfg, store)
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

	// Ranking weights come from the database, cached, with the compiled-in
	// defaults as the fallback for every failure path. Retuning ranking is a
	// twice-a-week activity and a weight change that needs a deploy is one that
	// gets batched with an unrelated feature and rolled back with it.
	rank := ranking.NewDBProvider(pool)

	moderationSvc := moderation.NewService(pool, modRepo, reelSvc)

	m := &Media{
		Store:      store,
		Reels:      reelSvc,
		Feed:       feed.NewService(pool, seen, counterSource, rank),
		Moderation: moderationSvc,
		ModRepo:    modRepo,
		Counters:   counters,
		Indexer:    indexer,
		Ranking:    rank,
		Interests:  interests.NewService(pool),
	}

	// The actions service borrows the reels service's branch authorisation
	// rather than reimplementing it. Two copies of an authorisation rule is one
	// too many, and the one that drifts is always the one nobody is watching.
	m.Actions = actions.NewService(pool, registries.Actions, reelSvc.Repo())
	m.Attribution = attribution.NewService(pool)

	m.Likes = social.NewLikes(pool, counterOrNil(counters))
	// Comments reuse the reel pipeline's text moderator. The same classifier,
	// the same GCC policy prompt, the same failure rule: an unavailable
	// moderator holds content rather than publishing it.
	m.Comments = comments.NewService(pool, text, counterOrNil(counters), moderationSvc)

	sink, chSink, err := buildEventSink(cfg, pool)
	if err != nil {
		return nil, err
	}
	m.ClickHouse = chSink
	m.Ingest = engagement.NewIngest(sink, counters)
	m.Pipeline = pipeline.NewRunner(pipeline.Deps{
		Pool:       pool,
		Reels:      reelSvc,
		Moderation: modRepo,
		Store:      store,
		Prober:     prober,
		Transcoder: transcoder,
		Moderator:  moderation.Pipeline{Vision: vision, Text: text},
		// The publish gate's source. Without this the pipeline publishes a
		// reel regardless of whether the listing it advertises was approved.
		Subjects: registries.Subjects,
	})

	return m, nil
}

// counterOrNil avoids handing a typed-nil *engagement.Counters to an interface
// parameter, which would satisfy the `!= nil` guard at every call site and then
// panic on first use. It is the oldest trap in Go interface plumbing and it is
// worth two lines to close.
func counterOrNil(c *engagement.Counters) social.Counter {
	if c == nil {
		return nil
	}
	return c
}

// buildEventSink chooses where engagement events land.
//
// Postgres by default, ClickHouse when configured, and both during a cutover.
// The tee is the part worth keeping: a migration that switches the old store
// off first has nothing to check its first bad number against.
//
// No production guard here, unlike the transcoder and the moderators. The
// reason is the asymmetry in what goes wrong: a stub moderator silently
// publishes everything, while a Postgres event sink at city scale merely gets
// slow and expensive — visibly, in a place someone is already watching.
func buildEventSink(cfg config.Config, pool *pgxpool.Pool) (engagement.Sink, *engagement.ClickHouseSink, error) {
	pg := engagement.NewPostgresSink(pool)
	if cfg.ClickHouseURL == "" {
		return pg, nil, nil
	}

	ch, err := engagement.NewClickHouseSink(engagement.ClickHouseConfig{
		URL:      cfg.ClickHouseURL,
		Database: cfg.ClickHouseDatabase,
		Table:    cfg.ClickHouseTable,
		Username: cfg.ClickHouseUser,
		Password: cfg.ClickHousePassword,
	})
	if err != nil {
		return nil, nil, err
	}

	if cfg.ClickHouseTee {
		slog.Info("analytics events writing to both ClickHouse and Postgres (cutover mode)")
		return engagement.TeeSink{Primary: ch, Secondary: pg}, ch, nil
	}
	return ch, ch, nil
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

func buildTranscoder(cfg config.Config, store *objectstore.Store) (media.TranscodeProvider, error) {
	switch cfg.TranscodeVendor {
	case "stub", "":
		// The guard is here and not in a comment: a stub transcoder in
		// production publishes reels whose playlists point at nothing.
		if cfg.IsProduction() {
			return nil, fmt.Errorf("TRANSCODE_VENDOR=stub is not permitted in production")
		}
		return media.StubProvider{BaseURL: cfg.MediaCDNBaseURL}, nil
	case "ffmpeg":
		// Local encoding. Refused in production for a different reason than the
		// stub: the output is real, but the job table is an in-memory map, so a
		// worker restart strands every in-flight reel permanently.
		if cfg.IsProduction() {
			return nil, fmt.Errorf("TRANSCODE_VENDOR=ffmpeg is not permitted in production — " +
				"its job state is in-memory and does not survive a restart")
		}
		// Store is attached by the caller, which has one. Returning a provider
		// with a nil store and letting it fail on the first upload would put the
		// error four minutes and one upload away from the misconfiguration.
		return &media.FFmpegProvider{
			Binary:  cfg.FFmpegBinary,
			Store:   store,
			BaseURL: cfg.MediaCDNBaseURL,
		}, nil
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
