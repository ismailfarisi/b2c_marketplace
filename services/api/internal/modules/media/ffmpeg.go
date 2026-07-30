package media

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// FFmpegProvider transcodes locally, with ffmpeg, into the object store.
//
// It exists because [StubProvider] is not enough to see the product. The stub
// synthesises a Result whose playlist URLs point at objects nobody wrote, which
// is exactly right for exercising the state machine and useless for looking at a
// feed: the app resolves the URLs, gets 404s, and renders the overlay over a
// black rectangle. Every screenshot taken that way is a lie about whether
// playback works.
//
// This is a **development** provider and the distinction is not about quality —
// the output is real HLS that a real player streams. It is about shape:
//
//   - Submit does the whole encode inline and returns a handle to a finished
//     job. A hosted encoder returns in milliseconds and calls back minutes later,
//     which is the asynchrony the seam is built around and the reason Submit
//     cannot return a Result. Here the "callback" has already happened by the
//     time Submit returns, so the pipeline's async path is exercised but never
//     stressed.
//   - Results live in memory. A worker restart loses the map, and the
//     reconciliation sweep then asks about a job this provider has never heard
//     of. On a laptop that costs a re-upload; in production it would be a class
//     of permanently stuck reels, which is why the wiring refuses it there.
//
// What it is genuinely good for: a local stack that plays video, and a
// reference for what a real provider's Result has to contain.
type FFmpegProvider struct {
	// Binary is the ffmpeg executable. Empty means "ffmpeg" on PATH.
	Binary string

	// Store writes the outputs. Narrow on purpose — this package must not learn
	// what a bucket is.
	Store Uploader

	// BaseURL is where the written objects are readable from, and it is the one
	// field that is easy to get wrong. It must be reachable **from the player**,
	// not from the worker: an Android emulator reaches the host on 10.0.2.2, so
	// a BaseURL of localhost produces playlists that resolve fine in curl on the
	// dev machine and time out on the device.
	BaseURL string

	mu      sync.Mutex
	results map[string]Result
}

// Uploader is the slice of the object store this provider needs.
type Uploader interface {
	Put(ctx context.Context, key, contentType string, body io.Reader) error
}

func (p *FFmpegProvider) Name() string { return "ffmpeg" }

// Submit encodes the source into an HLS ladder and uploads it.
//
// sourceURL is read by ffmpeg directly, so a presigned GET works and the bytes
// never pass through this process.
func (p *FFmpegProvider) Submit(ctx context.Context, sourceURL string, prof Profile) (JobRef, error) {
	if sourceURL == "" {
		return JobRef{}, fmt.Errorf("media: empty source url")
	}
	if p.Store == nil {
		return JobRef{}, fmt.Errorf("media: ffmpeg provider has no store")
	}

	jobID := uuid.NewString()
	work, err := os.MkdirTemp("", "reel-"+jobID)
	if err != nil {
		return JobRef{}, fmt.Errorf("media: temp dir: %w", err)
	}
	defer os.RemoveAll(work)

	rends := prof.Renditions
	if len(rends) == 0 {
		rends = Ladder()
	}
	seg := prof.SegmentSeconds
	if seg <= 0 {
		seg = 2
	}

	if err := p.encode(ctx, sourceURL, work, rends, seg); err != nil {
		return JobRef{}, err
	}
	if err := p.poster(ctx, sourceURL, work, prof.PosterAtMS); err != nil {
		return JobRef{}, err
	}

	result, err := p.upload(ctx, work, jobID, rends)
	if err != nil {
		return JobRef{}, err
	}

	p.mu.Lock()
	if p.results == nil {
		p.results = map[string]Result{}
	}
	p.results[jobID] = result
	p.mu.Unlock()

	return JobRef{Provider: "ffmpeg", ID: jobID}, nil
}

func (p *FFmpegProvider) Fetch(_ context.Context, ref JobRef) (Result, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r, ok := p.results[ref.ID]
	// ok=false rather than an error for an unknown job. That is what a real
	// provider says about a job it is still working on, and the pipeline's
	// reconciliation sweep is written against that answer.
	return r, ok, nil
}

// encode produces one variant directory per rendition plus a master playlist.
//
// One ffmpeg invocation rather than one per rendition: the source is decoded
// once and split, which on a laptop is the difference between a few seconds and
// most of a minute for a 15-second clip.
func (p *FFmpegProvider) encode(ctx context.Context, src, work string, rends []Rendition, seg int) error {
	var filter strings.Builder
	fmt.Fprintf(&filter, "[0:v]split=%d", len(rends))
	for i := range rends {
		fmt.Fprintf(&filter, "[s%d]", i)
	}
	for i, r := range rends {
		// Scaled on **width**, because these are vertical reels and the ladder
		// says so — 360p here means 360 wide, not 360 tall. Scaling on height
		// would produce a 202-pixel-wide "360p" and a ladder whose top rung is
		// narrower than its label.
		//
		// -2 keeps the aspect ratio and rounds to an even number, which H.264
		// requires and which a plain -1 does not guarantee.
		fmt.Fprintf(&filter, ";[s%d]scale=%d:-2[v%d]", i, r.Width, i)
	}

	args := []string{"-y", "-loglevel", "error", "-i", src, "-filter_complex", filter.String()}
	for i, r := range rends {
		args = append(args,
			"-map", fmt.Sprintf("[v%d]", i),
			fmt.Sprintf("-c:v:%d", i), "libx264",
			fmt.Sprintf("-b:v:%d", i), fmt.Sprintf("%dk", r.BitrateKbps),
			fmt.Sprintf("-maxrate:v:%d", i), fmt.Sprintf("%dk", r.BitrateKbps*3/2),
			fmt.Sprintf("-bufsize:v:%d", i), fmt.Sprintf("%dk", r.BitrateKbps*2),
		)
	}

	// Every variant gets the audio track when there is one. `?` makes the map
	// optional: a clip with no audio is normal — plenty of reels are shot muted
	// — and ffmpeg fails hard on an unsatisfiable map without it.
	for range rends {
		args = append(args, "-map", "0:a?", "-c:a", "aac", "-b:a", "96k")
	}

	varMap := make([]string, 0, len(rends))
	for i := range rends {
		varMap = append(varMap, fmt.Sprintf("v:%d,a:%d", i, i))
	}

	args = append(args,
		"-preset", "veryfast",
		// Keyframe every segment, and this is what makes the ladder switchable:
		// a player can only change variant at a keyframe, so segments that do
		// not start on one force it to stay on whichever rung it began with.
		"-g", fmt.Sprintf("%d", seg*30),
		"-keyint_min", fmt.Sprintf("%d", seg*30),
		"-sc_threshold", "0",
		"-f", "hls",
		"-hls_time", fmt.Sprintf("%d", seg),
		"-hls_playlist_type", "vod",
		"-hls_flags", "independent_segments",
		"-var_stream_map", strings.Join(varMap, " "),
		"-master_pl_name", "master.m3u8",
		"-hls_segment_filename", filepath.Join(work, "v%v", "seg%03d.ts"),
		filepath.Join(work, "v%v", "index.m3u8"),
	)

	for i := range rends {
		if err := os.MkdirAll(filepath.Join(work, fmt.Sprintf("v%d", i)), 0o755); err != nil {
			return fmt.Errorf("media: variant dir: %w", err)
		}
	}

	return p.run(ctx, args, "transcode")
}

func (p *FFmpegProvider) poster(ctx context.Context, src, work string, atMS int) error {
	if atMS <= 0 {
		atMS = 1000
	}
	// -ss before -i seeks by keyframe and is far faster than decoding to the
	// timestamp. The frame may land slightly early; for a poster that is
	// invisible and the alternative is decoding the whole file twice.
	return p.run(ctx, []string{
		"-y", "-loglevel", "error",
		"-ss", fmt.Sprintf("%.3f", float64(atMS)/1000.0),
		"-i", src, "-frames:v", "1", "-q:v", "3",
		filepath.Join(work, "poster.jpg"),
	}, "poster")
}

func (p *FFmpegProvider) run(ctx context.Context, args []string, stage string) error {
	bin := p.Binary
	if bin == "" {
		bin = "ffmpeg"
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// ffmpeg's own stderr, verbatim. Wrapping it in "transcode failed" and
		// dropping the output turns a one-line diagnosis into an afternoon.
		return fmt.Errorf("media: ffmpeg %s: %w: %s", stage, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// upload walks the work directory into the store and builds the Result.
//
// Keyed under `hls/{jobID}/` so the whole output of one job is a single prefix —
// which is what makes a takedown a prefix delete rather than a manifest walk.
func (p *FFmpegProvider) upload(ctx context.Context, work, jobID string, rends []Rendition) (Result, error) {
	prefix := path.Join("hls", jobID)

	err := filepath.WalkDir(work, func(fullPath string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, relErr := filepath.Rel(work, fullPath)
		if relErr != nil {
			return relErr
		}
		f, openErr := os.Open(fullPath)
		if openErr != nil {
			return openErr
		}
		defer f.Close()

		key := path.Join(prefix, filepath.ToSlash(rel))
		return p.Store.Put(ctx, key, contentTypeFor(rel), f)
	})
	if err != nil {
		return Result{}, fmt.Errorf("media: upload hls: %w", err)
	}

	out := make([]Rendition, len(rends))
	copy(out, rends)
	for i := range out {
		// The variant's own playlist, not the master. The Rendition type is a
		// per-rung record and the feed hands one URL to the player; pointing
		// every rung at the master would make the ladder a lie and the client's
		// rendition choice a no-op.
		out[i].PlaylistURL = fmt.Sprintf("%s/%s/v%d/index.m3u8",
			strings.TrimSuffix(p.BaseURL, "/"), prefix, i)
	}

	return Result{
		Renditions: out,
		PosterKey:  path.Join(prefix, "poster.jpg"),
		// No animated preview. ffmpeg can make one, and a webp loop of a clip
		// nobody has scrolled to yet is bandwidth spent on a maybe. Left empty
		// rather than faked so that "the preview is missing" stays visible.
		PreviewKey: "",
		// A real BlurHash needs the poster's pixels and a BlurHash encoder,
		// neither of which belongs in a dev transcoder. Empty, so the client
		// paints its own placeholder rather than decoding a string that is not
		// one.
		LQIP:       "",
		DurationMS: 0,
	}, nil
}

func contentTypeFor(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".m3u8":
		return "application/vnd.apple.mpegurl"
	case ".ts":
		return "video/mp2t"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	default:
		return "application/octet-stream"
	}
}
