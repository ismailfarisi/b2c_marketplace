// Package pipeline runs the Phase 2 media jobs: probe → transcode → moderate →
// publish.
//
// Each job is a function that takes a reel ID and is safe to run twice. That is
// not a nicety — every job here is delivered at-least-once, and a pipeline
// whose steps are not idempotent produces duplicate renditions, duplicate
// moderation spend and, eventually, a reel published twice.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/media"
	"github.com/marketplace/api/internal/modules/moderation"
	"github.com/marketplace/api/internal/modules/reels"
	"github.com/marketplace/api/internal/platform/objectstore"
	"github.com/marketplace/api/internal/platform/postgres"
)

type Runner struct {
	pool       *pgxpool.Pool
	reels      *reels.Service
	reelRepo   *reels.Repository
	modRepo    *moderation.Repository
	store      *objectstore.Store
	prober     media.Prober
	transcoder media.TranscodeProvider
	moderator  moderation.Pipeline
}

type Deps struct {
	Pool       *pgxpool.Pool
	Reels      *reels.Service
	Moderation *moderation.Repository
	Store      *objectstore.Store
	Prober     media.Prober
	Transcoder media.TranscodeProvider
	Moderator  moderation.Pipeline
}

func NewRunner(d Deps) *Runner {
	return &Runner{
		pool:       d.Pool,
		reels:      d.Reels,
		reelRepo:   d.Reels.Repo(),
		modRepo:    d.Moderation,
		store:      d.Store,
		prober:     d.Prober,
		transcoder: d.Transcoder,
		moderator:  d.Moderator,
	}
}

// Run dispatches one job.
func (r *Runner) Run(ctx context.Context, job reels.JobKind, reelID uuid.UUID) error {
	switch job {
	case reels.JobProbe:
		return r.Probe(ctx, reelID)
	case reels.JobTranscode:
		return r.Transcode(ctx, reelID)
	case reels.JobModerate:
		return r.Moderate(ctx, reelID)
	case reels.JobPublish:
		return r.Publish(ctx, reelID)
	case reels.JobNone:
		return nil
	}
	return fmt.Errorf("pipeline: unknown job %q", job)
}

// asset carries what every job needs about a reel's source object.
type asset struct {
	AssetID    uuid.UUID
	AuthorType string
	AuthorID   uuid.UUID
	S3Key      string
	Caption    string
	PosterKey  string
	State      reels.State
}

func (r *Runner) loadAsset(ctx context.Context, reelID uuid.UUID) (asset, error) {
	var a asset
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(re.media_asset_id, '00000000-0000-0000-0000-000000000000'::uuid),
		       re.author_type, re.author_id, COALESCE(ma.s3_key, ''),
		       COALESCE(re.caption, ''), COALESCE(re.poster_key, ''), re.state::text
		  FROM reels re
		  LEFT JOIN media_assets ma ON ma.id = re.media_asset_id
		 WHERE re.id = $1`, reelID,
	).Scan(&a.AssetID, &a.AuthorType, &a.AuthorID, &a.S3Key, &a.Caption, &a.PosterKey, (*string)(&a.State))
	if errors.Is(err, pgx.ErrNoRows) {
		return asset{}, httpx.ErrNotFound
	}
	if err != nil {
		return asset{}, fmt.Errorf("pipeline: load asset: %w", err)
	}
	return a, nil
}

// sourceTTL bounds how long a worker's read URL is valid. Longer than any
// single job needs; short enough that a URL leaked in a log is not a permanent
// hole in the bucket.
const sourceTTL = 30 * time.Minute

// Probe reads the source and applies the §2.3 validation gate.
//
// The gate runs here, before transcode, and that ordering is the whole point:
// rejecting at probe costs one ffprobe, rejecting after transcode costs a
// transcode.
func (r *Runner) Probe(ctx context.Context, reelID uuid.UUID) error {
	a, err := r.loadAsset(ctx, reelID)
	if err != nil {
		return err
	}
	if a.S3Key == "" {
		return r.fail(ctx, reelID, "no_source", "reel has no media asset")
	}

	sourceURL, err := r.store.PresignDownload(ctx, a.S3Key, sourceTTL)
	if err != nil {
		return err
	}

	probe, raw, err := r.prober.Probe(ctx, sourceURL)
	if err != nil {
		// An unreadable file is the author's problem, not an infrastructure
		// failure, so it rejects rather than retrying forever against a file
		// that will never parse.
		return r.reject(ctx, reelID, "unreadable_media", err.Error())
	}

	if err := media.Validate(probe, media.DefaultLimits()); err != nil {
		var v *httpx.ValidationError
		detail := err.Error()
		if errors.As(err, &v) && len(v.Fields) > 0 {
			detail = v.Fields[0].Code + ": " + v.Fields[0].Message
		}
		return r.reject(ctx, reelID, "failed_validation", detail)
	}

	err = postgres.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		return r.reelRepo.SaveProbe(ctx, tx, a.AssetID,
			probe.DurationMS, probe.Width, probe.Height, "video/mp4", rawJSON(raw))
	})
	if err != nil {
		return err
	}

	return r.advanceAndChain(ctx, reelID)
}

// Transcode submits to the provider and records what comes back.
func (r *Runner) Transcode(ctx context.Context, reelID uuid.UUID) error {
	a, err := r.loadAsset(ctx, reelID)
	if err != nil {
		return err
	}

	sourceURL, err := r.store.PresignDownload(ctx, a.S3Key, sourceTTL)
	if err != nil {
		return err
	}

	ref, err := r.transcoder.Submit(ctx, sourceURL, media.DefaultProfile())
	if err != nil {
		return r.fail(ctx, reelID, "transcode_submit_failed", err.Error())
	}

	// With a real provider this returns not-ready and completion arrives by
	// webhook. The job exits without advancing, and the webhook — or the
	// reconciliation sweep, for the webhooks that never arrive — calls
	// CompleteTranscode.
	result, ready, err := r.transcoder.Fetch(ctx, ref)
	if err != nil {
		return r.fail(ctx, reelID, "transcode_fetch_failed", err.Error())
	}
	if !ready {
		slog.InfoContext(ctx, "transcode submitted, awaiting completion",
			"reel_id", reelID, "provider", ref.Provider, "job", ref.ID)
		return nil
	}

	return r.CompleteTranscode(ctx, reelID, result)
}

// CompleteTranscode is the webhook entry point as well as the synchronous one.
func (r *Runner) CompleteTranscode(ctx context.Context, reelID uuid.UUID, result media.Result) error {
	if len(result.Renditions) == 0 {
		// Zero renditions with a success signal is a provider bug. Publishing
		// on it would put an unplayable reel in the feed, which is worse than
		// failing loudly here.
		return r.fail(ctx, reelID, "no_renditions", "provider reported success with no renditions")
	}

	converted := make([]reels.Rendition, 0, len(result.Renditions))
	for _, rend := range result.Renditions {
		converted = append(converted, reels.Rendition{
			Label: rend.Label, Width: rend.Width, Height: rend.Height,
			BitrateKbps: rend.BitrateKbps, PlaylistURL: rend.PlaylistURL,
		})
	}

	err := postgres.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		return r.reelRepo.SaveRenditions(ctx, tx, reelID, converted,
			result.PosterKey, result.PreviewKey, result.LQIP)
	})
	if err != nil {
		return err
	}
	return r.advanceAndChain(ctx, reelID)
}

// Moderate runs the layers and records the decision.
func (r *Runner) Moderate(ctx context.Context, reelID uuid.UUID) error {
	a, err := r.loadAsset(ctx, reelID)
	if err != nil {
		return err
	}

	posterURL := ""
	if a.PosterKey != "" {
		if posterURL, err = r.store.PresignDownload(ctx, a.PosterKey, sourceTTL); err != nil {
			return err
		}
	}
	videoURL, err := r.store.PresignDownload(ctx, a.S3Key, sourceTTL)
	if err != nil {
		return err
	}

	// Job rows are opened before the calls so an operator can see what is in
	// flight. A moderation run that hangs is otherwise invisible until the SLA
	// alarm fires.
	jobIDs := map[string]uuid.UUID{}
	err = postgres.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		for _, layer := range []struct{ name, provider string }{
			{moderation.LayerPoster, r.moderator.Vision.Name()},
			{moderation.LayerCaption, r.moderator.Text.Name()},
			{moderation.LayerVideo, r.moderator.Vision.Name()},
		} {
			id, err := r.modRepo.StartJob(ctx, tx, reelID, layer.name, layer.provider)
			if err != nil {
				return err
			}
			jobIDs[layer.name] = id
		}
		return nil
	})
	if err != nil {
		return err
	}

	result := r.moderator.Run(ctx, moderation.Input{
		PosterURL: posterURL,
		VideoURL:  videoURL,
		Caption:   a.Caption,
	})

	// Trust routing can only make the verdict stricter, never looser (§3.4).
	err = postgres.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		for _, layer := range result.Layers {
			jobID, ok := jobIDs[layer.Layer]
			if !ok {
				continue
			}
			if err := r.modRepo.SaveLabels(ctx, tx, jobID, reelID, layer.Labels, layer.Labels); err != nil {
				return err
			}
			if err := r.modRepo.FinishJob(ctx, tx, jobID, layer.Err); err != nil {
				return err
			}
		}
		// Any layer that was opened but produced no result — a stage-2 layer
		// skipped by the early exit — must not be left 'running' forever.
		for name, jobID := range jobIDs {
			if !layerRan(result.Layers, name) {
				if err := r.modRepo.FinishJob(ctx, tx, jobID, nil); err != nil {
					return err
				}
			}
		}

		trust, err := r.modRepo.LoadTrust(ctx, tx, a.AuthorType, a.AuthorID)
		if err != nil {
			return err
		}
		tier := moderation.ComputeTier(trust, time.Now())

		decision := result.Decision
		decision.Verdict = moderation.Route(result.Verdict, tier, sampled())

		if err := r.modRepo.SaveDecision(ctx, tx, reelID, decision, "auto", nil, ""); err != nil {
			return err
		}
		if decision.Verdict == moderation.VerdictReject {
			if err := r.modRepo.RecordStrike(ctx, tx, a.AuthorType, a.AuthorID, time.Now()); err != nil {
				return err
			}
			return r.reelRepo.RecordFailure(ctx, tx, reelID, "policy_violation",
				joinCategories(decision.Categories))
		}
		return nil
	})
	if err != nil {
		return err
	}

	return r.advanceAndChain(ctx, reelID)
}

// Publish resolves coverage and makes the reel visible.
func (r *Runner) Publish(ctx context.Context, reelID uuid.UUID) error {
	a, err := r.loadAsset(ctx, reelID)
	if err != nil {
		return err
	}

	err = postgres.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		resolved, err := r.reelRepo.ResolveCoverage(ctx, tx, reelID)
		if err != nil {
			return err
		}
		if !resolved {
			// Refusing to publish is the right call: a reel with no coverage
			// is invisible to every geo query, and a vendor whose reel
			// "published" but appears nowhere will report it as a feed bug.
			return r.reelRepo.RecordFailure(ctx, tx, reelID, "no_coverage",
				"the branch has no service area — draw one before publishing")
		}
		return nil
	})
	if err != nil {
		return err
	}

	// advance moves approved → published now that coverage exists.
	if _, err := r.reels.Advance(ctx, reelID, reels.ActorSystem); err != nil {
		return err
	}

	return postgres.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		if err := r.modRepo.RecordPublished(ctx, tx, a.AuthorType, a.AuthorID, time.Now()); err != nil {
			return err
		}
		return r.reelRepo.InsertOutbox(ctx, tx, reelID, "reel.published", map[string]any{
			"reel_id":     reelID,
			"author_type": a.AuthorType,
			"author_id":   a.AuthorID,
		})
	})
}

// advanceAndChain moves the reel and runs whatever job that unlocked.
//
// Recursion is bounded by the state graph: each step moves strictly forward, so
// the chain is at most as long as the pipeline. Chaining in-process rather than
// re-enqueuing keeps a 30-second upload-to-feed path from becoming a
// four-poll-interval one.
func (r *Runner) advanceAndChain(ctx context.Context, reelID uuid.UUID) error {
	job, err := r.reels.Advance(ctx, reelID, reels.ActorSystem)
	if err != nil {
		return err
	}
	if job == reels.JobNone {
		return nil
	}
	return r.Run(ctx, job, reelID)
}

func (r *Runner) fail(ctx context.Context, reelID uuid.UUID, code, detail string) error {
	slog.ErrorContext(ctx, "reel pipeline failure", "reel_id", reelID, "code", code, "detail", detail)
	return postgres.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		if err := r.reelRepo.RecordFailure(ctx, tx, reelID, code, detail); err != nil {
			return err
		}
		snap, err := r.reelRepo.LoadSnapshot(ctx, tx, reelID)
		if err != nil {
			return err
		}
		return r.reelRepo.Transition(ctx, tx, reelID, snap.State, reels.StateFailed,
			reels.ActorSystem, code)
	})
}

func (r *Runner) reject(ctx context.Context, reelID uuid.UUID, code, detail string) error {
	return postgres.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		if err := r.reelRepo.RecordFailure(ctx, tx, reelID, code, detail); err != nil {
			return err
		}
		snap, err := r.reelRepo.LoadSnapshot(ctx, tx, reelID)
		if err != nil {
			return err
		}
		if err := r.modRepo.SaveDecision(ctx, tx, reelID, moderation.Decision{
			Verdict: moderation.VerdictReject,
			Detail:  detail,
		}, "auto", nil, code); err != nil {
			return err
		}
		return r.reelRepo.Transition(ctx, tx, reelID, snap.State, reels.StateRejected,
			reels.ActorSystem, code)
	})
}

func layerRan(layers []moderation.LayerResult, name string) bool {
	for _, l := range layers {
		if l.Layer == name {
			return true
		}
	}
	return false
}

// sampled decides whether a trusted author's clean reel is pulled for the 5%
// audit. Random rather than deterministic-by-ID so an author cannot learn which
// of their reels will be checked.
func sampled() bool { return rand.Float64() < moderation.SampleRate }

func joinCategories(cs []string) string {
	if len(cs) == 0 {
		return ""
	}
	out := cs[0]
	for _, c := range cs[1:] {
		out += ", " + c
	}
	return out
}

func rawJSON(b []byte) any {
	if len(b) == 0 {
		return map[string]any{}
	}
	return string(b)
}
