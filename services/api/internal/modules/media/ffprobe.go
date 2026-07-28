package media

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Prober reads a source file's technical properties.
//
// An interface because the production implementation shells out to a binary
// that may not exist on a developer's machine, and because the validation gate
// — which is the part with the interesting logic — should be testable without
// one.
type Prober interface {
	Probe(ctx context.Context, sourceURL string) (Probe, []byte, error)
}

// FFProbe runs the ffprobe binary against a URL.
//
// Against a presigned URL rather than a downloaded file: ffprobe reads the
// container header and seeks, so it pulls a few hundred kilobytes of a 30 MB
// object rather than the whole thing. Downloading first would move the entire
// asset through the worker for information that lives in the first pages of it.
type FFProbe struct {
	// Binary defaults to "ffprobe" on PATH.
	Binary string
	// Timeout bounds a single probe. Generous relative to the work, because the
	// variable is network latency to S3, not CPU.
	Timeout time.Duration
}

func (f FFProbe) binary() string {
	if f.Binary != "" {
		return f.Binary
	}
	return "ffprobe"
}

func (f FFProbe) timeout() time.Duration {
	if f.Timeout > 0 {
		return f.Timeout
	}
	return 60 * time.Second
}

func (f FFProbe) Probe(ctx context.Context, sourceURL string) (Probe, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, f.timeout())
	defer cancel()

	cmd := exec.CommandContext(ctx, f.binary(),
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		sourceURL,
	)

	out, err := cmd.Output()
	if err != nil {
		// A malformed upload makes ffprobe exit non-zero, and that is a
		// validation failure rather than an infrastructure one. The caller
		// distinguishes them by the reel's state, not by inspecting this error.
		return Probe{}, nil, fmt.Errorf("media: ffprobe failed: %w", err)
	}

	p, err := parseFFProbe(out)
	if err != nil {
		return Probe{}, out, err
	}
	return p, out, nil
}

// ffprobeOutput is the subset we read. Everything is a string in ffprobe's JSON
// — including numbers — which is why each field is parsed rather than
// unmarshalled directly into an int.
type ffprobeOutput struct {
	Streams []struct {
		CodecType     string `json:"codec_type"`
		CodecName     string `json:"codec_name"`
		Width         int    `json:"width"`
		Height        int    `json:"height"`
		Duration      string `json:"duration"`
		SideDataList  []any  `json:"side_data_list"`
		Rotation      any    `json:"rotation"`
		DisplayAspect string `json:"display_aspect_ratio"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
		Size     string `json:"size"`
		Format   string `json:"format_name"`
	} `json:"format"`
}

func parseFFProbe(raw []byte) (Probe, error) {
	var out ffprobeOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return Probe{}, fmt.Errorf("media: parse ffprobe output: %w", err)
	}

	var p Probe
	for _, s := range out.Streams {
		switch s.CodecType {
		case "video":
			if p.HasVideo {
				continue
			}
			p.HasVideo = true
			p.VideoCodec = s.CodecName
			p.Width, p.Height = s.Width, s.Height
			// Phones record 9:16 by writing a 16:9 frame plus a 90° rotation
			// tag. Reading width and height literally would reject every
			// iPhone upload as "not vertical", which is the single most common
			// source of video in this product.
			if rotated(s.Rotation) {
				p.Width, p.Height = p.Height, p.Width
			}
		case "audio":
			p.HasAudio = true
			p.AudioCodec = s.CodecName
		}
	}

	if secs, err := strconv.ParseFloat(out.Format.Duration, 64); err == nil {
		p.DurationMS = int(secs * 1000)
	}
	if bytes, err := strconv.ParseInt(out.Format.Size, 10, 64); err == nil {
		p.Bytes = bytes
	}

	return p, nil
}

// rotated reports whether the rotation tag transposes the frame. ffprobe emits
// it as a number in some builds and a string in others.
func rotated(v any) bool {
	var deg float64
	switch t := v.(type) {
	case float64:
		deg = t
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		if err != nil {
			return false
		}
		deg = parsed
	default:
		return false
	}

	normalised := int(deg) % 360
	if normalised < 0 {
		normalised += 360
	}
	return normalised == 90 || normalised == 270
}

// StubProber returns a valid vertical clip. Used when ffprobe is absent, so the
// pipeline can be exercised end to end on a laptop.
type StubProber struct{}

func (StubProber) Probe(_ context.Context, sourceURL string) (Probe, []byte, error) {
	if sourceURL == "" {
		return Probe{}, nil, fmt.Errorf("media: empty source url")
	}
	return Probe{
		DurationMS: 30_000,
		Width:      720,
		Height:     1280,
		Bytes:      12 << 20,
		VideoCodec: "h264",
		AudioCodec: "aac",
		HasVideo:   true,
		HasAudio:   true,
	}, []byte(`{"stub":true}`), nil
}
