package media

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Rendition is one rung of the ladder.
type Rendition struct {
	Label       string
	Width       int
	Height      int
	BitrateKbps int
	PlaylistURL string
}

// Ladder is the §2.2 ladder, capped at 720p.
//
// The cap is a cost decision, not a quality oversight: 1080p roughly doubles
// egress for a difference nobody perceives on a phone-sized autoplay feed, and
// egress is the single largest line in the Phase 2 cost model. Revisit only if
// analytics show sustained full-screen viewing — which is a measurement, not a
// hunch.
func Ladder() []Rendition {
	return []Rendition{
		{Label: "360p", Width: 360, Height: 640, BitrateKbps: 600},
		{Label: "540p", Width: 540, Height: 960, BitrateKbps: 1200},
		{Label: "720p", Width: 720, Height: 1280, BitrateKbps: 2200},
	}
}

// Profile is what a provider is asked to produce.
type Profile struct {
	Renditions []Rendition
	// HLS segment length. Two seconds trades a little packaging overhead for
	// startup latency, and startup latency is the entire experience in a feed
	// where the user swipes every four seconds.
	SegmentSeconds int
	PosterAtMS     int
	PreviewMS      int
}

func DefaultProfile() Profile {
	return Profile{
		Renditions:     Ladder(),
		SegmentSeconds: 2,
		PosterAtMS:     1_000,
		PreviewMS:      2_000,
	}
}

// JobRef is the provider's handle. Stored on moderation_jobs/provider_job_id's
// sibling column so a webhook can be correlated back to a reel.
type JobRef struct {
	Provider string
	ID       string
}

// Result is what a completed transcode yields, normalised across providers.
// Mux and MediaConvert describe their output very differently; the difference
// stops at this struct.
type Result struct {
	Renditions []Rendition
	PosterKey  string
	PreviewKey string
	LQIP       string
	DurationMS int
}

// TranscodeProvider is the seam.
//
// Completion arrives asynchronously — by webhook from Mux, by EventBridge → SQS
// from MediaConvert — so Submit returns a handle and never a Result. Modelling
// it as a blocking call would work in a demo and fall over the first time a
// 90-second source takes four minutes.
type TranscodeProvider interface {
	Name() string
	Submit(ctx context.Context, sourceURL string, p Profile) (JobRef, error)
	// Fetch resolves a completed job. Callable from the webhook handler (to
	// confirm what the webhook claimed) and from a reconciliation sweep (to
	// recover jobs whose webhook never arrived — and some never will).
	Fetch(ctx context.Context, ref JobRef) (Result, bool, error)
}

// StubProvider synthesises a completed transcode without an encoder.
//
// It exists so the pipeline, the state machine and the feed can be exercised
// end to end before a Mux account exists — and so tests never depend on a
// third-party service being up. It is refused in production by the wiring in
// cmd/worker, not by a comment here.
type StubProvider struct {
	// BaseURL is where the fictional playlists live, e.g. a MinIO bucket.
	BaseURL string
}

func (StubProvider) Name() string { return "stub" }

func (s StubProvider) Submit(_ context.Context, sourceURL string, _ Profile) (JobRef, error) {
	if sourceURL == "" {
		return JobRef{}, fmt.Errorf("media: empty source url")
	}
	return JobRef{Provider: "stub", ID: uuid.NewString()}, nil
}

func (s StubProvider) Fetch(_ context.Context, ref JobRef) (Result, bool, error) {
	rs := Ladder()
	for i := range rs {
		rs[i].PlaylistURL = fmt.Sprintf("%s/%s/%s.m3u8", s.BaseURL, ref.ID, rs[i].Label)
	}
	return Result{
		Renditions: rs,
		PosterKey:  fmt.Sprintf("posters/%s.jpg", ref.ID),
		PreviewKey: fmt.Sprintf("previews/%s.webp", ref.ID),
		// A real BlurHash comes from the poster frame. This is a valid
		// placeholder string of the right shape so client rendering can be
		// built against it.
		LQIP:       "LEHV6nWB2yk8pyo0adR*.7kCMdnj",
		DurationMS: 30_000,
	}, true, nil
}

// PollInterval is how often the reconciliation sweep asks a provider about jobs
// it has not heard back on. Webhooks are the fast path; this is the one that
// makes the pipeline eventually correct rather than usually correct.
const PollInterval = 30 * time.Second
