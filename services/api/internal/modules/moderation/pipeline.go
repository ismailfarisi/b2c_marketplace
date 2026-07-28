package moderation

import (
	"context"
	"log/slog"
	"strings"
	"sync"
)

// Layer names, matching moderation_jobs.layer.
const (
	LayerPoster  = "poster"
	LayerCaption = "caption"
	LayerVideo   = "video"
	LayerAudio   = "audio"
)

// Input is everything the pipeline needs about one reel.
type Input struct {
	PosterURL string
	VideoURL  string
	Caption   string
	// Transcript from Transcribe, when the audio layer has run. Empty skips it.
	Transcript string
}

// LayerResult is one layer's contribution, kept whole so every raw label can be
// persisted. Tuning the thresholds later is only possible against labels that
// were stored, including the ones that crossed nothing.
//
// Labels and Categories are separate on purpose. A vision provider returns
// scored labels that the threshold table interprets; a text model returns a
// judgement with named categories and no meaningful confidence. Forcing the
// latter through the former would mean inventing a number, and any number
// chosen is wrong: there is no value both at-or-above every review threshold
// (tobacco, 50) and strictly below every reject threshold (hate symbols, 50).
type LayerResult struct {
	Layer    string
	Provider string
	// Scored detections, for the threshold table.
	Labels []Label
	// Named categories from a judging model, for the record and the author's
	// rejection reason.
	Categories []string
	Verdict    Verdict
	Detail     string
	Err        error
}

// Result is the combined outcome.
type Result struct {
	Verdict  Verdict
	Decision Decision
	Layers   []LayerResult
	Language string
}

// Pipeline runs the §3.1 layers.
type Pipeline struct {
	Vision VisionModerator
	Text   TextModerator
}

// Run executes the layers in cost order with an early exit.
//
// Stage 1 is poster + caption, concurrently. They are the two cheap layers and
// they are independent, so running them in parallel costs nothing extra and
// halves the latency of the common case.
//
// Stage 2 — video frames and audio transcript — runs only if stage 1 did not
// already reject. That ordering is the entire cost argument of §3.1: video
// moderation is priced per minute analysed and is the expensive layer, so
// spending it on content the poster frame already condemned is money burned on
// a conclusion you have.
//
// A layer that errors yields VerdictReview, never VerdictApprove. An unavailable
// moderator must fail toward a human; failing toward publish means one AWS
// incident puts unmoderated content in the feed and nothing in the logs says so.
func (p Pipeline) Run(ctx context.Context, in Input) Result {
	res := Result{Verdict: VerdictApprove}

	stage1 := p.runConcurrent(ctx,
		func() LayerResult { return p.poster(ctx, in.PosterURL) },
		func() LayerResult { return p.caption(ctx, in.Caption) },
	)
	res.Layers = append(res.Layers, stage1...)
	for _, l := range stage1 {
		res.Verdict = Stricter(res.Verdict, l.Verdict)
		if l.Layer == LayerCaption && l.Detail != "" {
			res.Language = l.Detail
		}
	}

	if res.Verdict != VerdictReject {
		var stage2 []func() LayerResult
		if in.VideoURL != "" {
			stage2 = append(stage2, func() LayerResult { return p.video(ctx, in.VideoURL) })
		}
		if strings.TrimSpace(in.Transcript) != "" {
			stage2 = append(stage2, func() LayerResult { return p.audio(ctx, in.Transcript) })
		}
		if len(stage2) > 0 {
			results := p.runConcurrent(ctx, stage2...)
			res.Layers = append(res.Layers, results...)
			for _, l := range results {
				res.Verdict = Stricter(res.Verdict, l.Verdict)
			}
		}
	}

	// Score every vision label together, so a category that is borderline on
	// the poster and borderline again in the video is judged on its peak rather
	// than layer by layer.
	var scored []Label
	for _, l := range res.Layers {
		scored = append(scored, l.Labels...)
	}
	res.Decision = Decide(scored)

	// Fold in the text layers' named categories, which carry no confidence and
	// therefore never pass through Decide. The verdict they justified is
	// already in res.Verdict.
	for _, l := range res.Layers {
		for _, c := range l.Categories {
			res.Decision.Categories = appendUnique(res.Decision.Categories, normaliseCategory(c))
		}
	}

	res.Verdict = Stricter(res.Decision.Verdict, res.Verdict)
	res.Decision.Verdict = res.Verdict
	return res
}

func appendUnique(xs []string, x string) []string {
	if x == "" {
		return xs
	}
	for _, existing := range xs {
		if existing == x {
			return xs
		}
	}
	return append(xs, x)
}

func (p Pipeline) runConcurrent(ctx context.Context, fns ...func() LayerResult) []LayerResult {
	out := make([]LayerResult, len(fns))
	var wg sync.WaitGroup
	for i, fn := range fns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = fn()
		}()
	}
	wg.Wait()

	for _, l := range out {
		if l.Err != nil {
			slog.ErrorContext(ctx, "moderation layer failed",
				"layer", l.Layer, "provider", l.Provider, "error", l.Err)
		}
	}
	return out
}

func (p Pipeline) poster(ctx context.Context, url string) LayerResult {
	r := LayerResult{Layer: LayerPoster, Provider: p.Vision.Name()}
	if url == "" {
		// No poster yet is not a failure — the poster frame comes out of
		// transcode, and on the fast path this layer runs before it exists.
		r.Verdict = VerdictApprove
		return r
	}
	labels, err := p.Vision.ModerateImage(ctx, url)
	if err != nil {
		r.Err, r.Verdict, r.Detail = err, VerdictReview, "poster moderation unavailable"
		return r
	}
	r.Labels = labels
	r.Verdict = Decide(labels).Verdict
	return r
}

func (p Pipeline) video(ctx context.Context, url string) LayerResult {
	r := LayerResult{Layer: LayerVideo, Provider: p.Vision.Name()}
	labels, err := p.Vision.ModerateVideo(ctx, url)
	if err != nil {
		r.Err, r.Verdict, r.Detail = err, VerdictReview, "video moderation unavailable"
		return r
	}
	r.Labels = labels
	r.Verdict = Decide(labels).Verdict
	return r
}

func (p Pipeline) caption(ctx context.Context, caption string) LayerResult {
	r := LayerResult{Layer: LayerCaption, Provider: p.Text.Name()}
	out, err := p.Text.ModerateText(ctx, caption)
	if err != nil {
		r.Err, r.Verdict, r.Detail = err, VerdictReview, "text moderation unavailable"
		return r
	}
	r.Verdict = out.Verdict
	r.Detail = out.Language
	r.Categories = out.Categories
	return r
}

func (p Pipeline) audio(ctx context.Context, transcript string) LayerResult {
	r := LayerResult{Layer: LayerAudio, Provider: p.Text.Name()}
	out, err := p.Text.ModerateText(ctx, transcript)
	if err != nil {
		r.Err, r.Verdict, r.Detail = err, VerdictReview, "audio moderation unavailable"
		return r
	}
	r.Verdict = out.Verdict
	r.Categories = out.Categories
	return r
}
