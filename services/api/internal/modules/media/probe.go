// Package media owns transcode and the validation gate in front of it.
//
// The gate matters more than it looks. Rejecting a bad source at probe costs
// one ffprobe; rejecting it after transcode costs a transcode, and transcode is
// the second-largest line in the Phase 2 cost model after egress
// (docs/PHASE-2-MEDIA-AND-FEED.md §7). Every rule here exists to fail before
// spending.
package media

import (
	"fmt"
	"math"
	"strings"

	"github.com/marketplace/api/internal/httpx"
)

// Probe is the normalised subset of ffprobe output the gate reasons about. The
// raw JSON is stored alongside it; this is what code reads.
type Probe struct {
	DurationMS int
	Width      int
	Height     int
	Bytes      int64
	VideoCodec string
	AudioCodec string
	HasVideo   bool
	HasAudio   bool
}

// Limits are the §2.3 thresholds, as data rather than as constants buried in an
// if-chain, so they can be tuned per market or per author tier without editing
// the validator.
type Limits struct {
	MinDurationMS int
	MaxDurationMS int
	MaxBytes      int64

	// Target aspect ratio and how far from it is still acceptable. The feed is
	// full-bleed vertical; a 16:9 clip letterboxes into black bars that make the
	// whole feed look broken, and no amount of client-side cropping saves it.
	TargetAspect    float64
	AspectTolerance float64

	AllowedVideoCodecs []string
}

// DefaultLimits implements §2.3.
//
// The 3s floor is not arbitrary: below it there is no time for a viewer to
// register what a service is, and the reel occupies a feed slot that a usable
// one could have had. The 90s ceiling is where completion rate collapses.
func DefaultLimits() Limits {
	return Limits{
		MinDurationMS:      3_000,
		MaxDurationMS:      90_000,
		MaxBytes:           500 << 20, // 500 MB
		TargetAspect:       9.0 / 16.0,
		AspectTolerance:    0.10,
		AllowedVideoCodecs: []string{"h264", "hevc", "vp9", "av1"},
	}
}

// Validate returns a ValidationError listing every problem, not just the first.
//
// One round-trip per problem is a miserable vendor experience: they re-export
// the clip, re-upload 20 MB over a hotel wifi, and discover the next rule. Tell
// them everything that is wrong the first time.
func Validate(p Probe, l Limits) error {
	var fields []httpx.FieldError

	add := func(field, code, msg string) {
		fields = append(fields, httpx.FieldError{Field: field, Code: code, Message: msg})
	}

	if !p.HasVideo {
		add("video", "no_video_stream", "the file contains no video stream")
	}
	// Audio is not required — plenty of good service reels are silent, and
	// autoplay feeds are muted by default anyway. But a file with neither
	// stream is not a video, it is a container.
	if !p.HasVideo && !p.HasAudio {
		add("file", "empty_media", "the file contains no audio and no video")
	}

	switch {
	case p.DurationMS <= 0:
		add("duration", "unknown_duration", "duration could not be determined")
	case p.DurationMS < l.MinDurationMS:
		add("duration", "too_short",
			fmt.Sprintf("minimum is %ds", l.MinDurationMS/1000))
	case p.DurationMS > l.MaxDurationMS:
		add("duration", "too_long",
			fmt.Sprintf("maximum is %ds", l.MaxDurationMS/1000))
	}

	if p.Bytes > l.MaxBytes {
		add("size", "too_large",
			fmt.Sprintf("maximum is %d MB", l.MaxBytes>>20))
	}

	if p.HasVideo {
		if p.Width <= 0 || p.Height <= 0 {
			add("dimensions", "unknown_dimensions", "width or height could not be determined")
		} else {
			aspect := float64(p.Width) / float64(p.Height)
			if math.Abs(aspect-l.TargetAspect) > l.AspectTolerance {
				add("aspect_ratio", "not_vertical",
					fmt.Sprintf("expected roughly 9:16, got %d×%d", p.Width, p.Height))
			}
		}

		if !codecAllowed(p.VideoCodec, l.AllowedVideoCodecs) {
			add("codec", "unsupported_codec",
				fmt.Sprintf("%q is not supported", p.VideoCodec))
		}
	}

	if len(fields) > 0 {
		return &httpx.ValidationError{Fields: fields}
	}
	return nil
}

func codecAllowed(codec string, allowed []string) bool {
	c := strings.ToLower(strings.TrimSpace(codec))
	if c == "" {
		return false
	}
	for _, a := range allowed {
		if c == a {
			return true
		}
	}
	return false
}
