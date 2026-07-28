package media

import (
	"errors"
	"testing"

	"github.com/marketplace/api/internal/httpx"
)

func validProbe() Probe {
	return Probe{
		DurationMS: 30_000,
		Width:      720,
		Height:     1280,
		Bytes:      12 << 20,
		VideoCodec: "h264",
		AudioCodec: "aac",
		HasVideo:   true,
		HasAudio:   true,
	}
}

func codesFor(t *testing.T, p Probe) []string {
	t.Helper()
	err := Validate(p, DefaultLimits())
	if err == nil {
		return nil
	}
	var v *httpx.ValidationError
	if !errors.As(err, &v) {
		t.Fatalf("Validate returned %T, want *httpx.ValidationError", err)
	}
	codes := make([]string, 0, len(v.Fields))
	for _, f := range v.Fields {
		codes = append(codes, f.Code)
	}
	return codes
}

func hasCode(codes []string, want string) bool {
	for _, c := range codes {
		if c == want {
			return true
		}
	}
	return false
}

func TestValidProbePasses(t *testing.T) {
	if codes := codesFor(t, validProbe()); codes != nil {
		t.Fatalf("a good vertical clip was rejected: %v", codes)
	}
}

func TestDurationBounds(t *testing.T) {
	short := validProbe()
	short.DurationMS = 2_000
	if !hasCode(codesFor(t, short), "too_short") {
		t.Error("a 2s clip was accepted")
	}

	long := validProbe()
	long.DurationMS = 91_000
	if !hasCode(codesFor(t, long), "too_long") {
		t.Error("a 91s clip was accepted")
	}

	// Exactly on the boundaries must pass — an off-by-one here silently
	// rejects every clip a vendor trimmed to the documented limit.
	atMin := validProbe()
	atMin.DurationMS = 3_000
	if codes := codesFor(t, atMin); codes != nil {
		t.Errorf("3.000s rejected: %v", codes)
	}
	atMax := validProbe()
	atMax.DurationMS = 90_000
	if codes := codesFor(t, atMax); codes != nil {
		t.Errorf("90.000s rejected: %v", codes)
	}
}

func TestLandscapeIsRejected(t *testing.T) {
	// A 16:9 clip letterboxes into black bars in a full-bleed vertical feed,
	// and no client-side cropping saves it.
	wide := validProbe()
	wide.Width, wide.Height = 1920, 1080
	if !hasCode(codesFor(t, wide), "not_vertical") {
		t.Fatal("a landscape clip was accepted")
	}
}

func TestNearVerticalIsAccepted(t *testing.T) {
	// 1080×1920 is exactly 9:16; 1080×1800 is close enough that rejecting it
	// would annoy vendors for no visible benefit.
	for _, dims := range [][2]int{{1080, 1920}, {1080, 1800}, {720, 1280}} {
		p := validProbe()
		p.Width, p.Height = dims[0], dims[1]
		if codes := codesFor(t, p); codes != nil {
			t.Errorf("%dx%d rejected: %v", dims[0], dims[1], codes)
		}
	}
}

func TestUnsupportedCodecIsRejected(t *testing.T) {
	p := validProbe()
	p.VideoCodec = "prores"
	if !hasCode(codesFor(t, p), "unsupported_codec") {
		t.Fatal("prores was accepted")
	}
}

func TestSilentVideoIsFine(t *testing.T) {
	// Autoplay feeds are muted by default; plenty of good service reels have no
	// audio track at all.
	p := validProbe()
	p.HasAudio = false
	p.AudioCodec = ""
	if codes := codesFor(t, p); codes != nil {
		t.Fatalf("a silent clip was rejected: %v", codes)
	}
}

func TestEmptyContainerIsRejected(t *testing.T) {
	p := Probe{DurationMS: 10_000}
	codes := codesFor(t, p)
	if !hasCode(codes, "no_video_stream") || !hasCode(codes, "empty_media") {
		t.Fatalf("a container with no streams produced %v", codes)
	}
}

func TestValidateReportsEveryProblemAtOnce(t *testing.T) {
	// One round-trip per problem means the vendor re-exports and re-uploads
	// 20 MB over hotel wifi to discover the next rule.
	bad := Probe{
		DurationMS: 120_000,
		Width:      1920,
		Height:     1080,
		Bytes:      600 << 20,
		VideoCodec: "prores",
		HasVideo:   true,
	}
	codes := codesFor(t, bad)
	for _, want := range []string{"too_long", "too_large", "not_vertical", "unsupported_codec"} {
		if !hasCode(codes, want) {
			t.Errorf("missing %q in %v", want, codes)
		}
	}
}

// --- ffprobe parsing --------------------------------------------------------

func TestRotatedPhoneVideoIsTreatedAsVertical(t *testing.T) {
	// Phones record 9:16 by writing a 16:9 frame plus a 90° rotation tag.
	// Reading width and height literally rejects every iPhone upload — which is
	// the single most common source of video in this product.
	raw := []byte(`{
	  "streams": [
	    {"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"rotation":-90},
	    {"codec_type":"audio","codec_name":"aac"}
	  ],
	  "format": {"duration":"28.500000","size":"14680064","format_name":"mov,mp4"}
	}`)

	p, err := parseFFProbe(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.Width != 1080 || p.Height != 1920 {
		t.Fatalf("dimensions = %dx%d, want 1080x1920 after applying rotation", p.Width, p.Height)
	}
	if codes := codesFor(t, p); codes != nil {
		t.Fatalf("a rotated phone video was rejected: %v", codes)
	}
}

func TestFFProbeStringRotationIsHandled(t *testing.T) {
	// Some ffprobe builds emit rotation as a string.
	raw := []byte(`{"streams":[{"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"rotation":"270"}],
	                "format":{"duration":"10.0","size":"1000"}}`)
	p, err := parseFFProbe(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.Width != 1080 {
		t.Fatalf("string rotation ignored: %dx%d", p.Width, p.Height)
	}
}

func TestFFProbeUnrotatedIsLeftAlone(t *testing.T) {
	raw := []byte(`{"streams":[{"codec_type":"video","codec_name":"h264","width":720,"height":1280,"rotation":180}],
	                "format":{"duration":"30.0","size":"1000"}}`)
	p, err := parseFFProbe(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.Width != 720 || p.Height != 1280 {
		t.Fatalf("180° rotation transposed the frame: %dx%d", p.Width, p.Height)
	}
}

func TestFFProbeParsesDurationAndSize(t *testing.T) {
	raw := []byte(`{"streams":[{"codec_type":"video","codec_name":"h264","width":720,"height":1280}],
	                "format":{"duration":"28.500000","size":"14680064"}}`)
	p, err := parseFFProbe(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.DurationMS != 28_500 {
		t.Errorf("duration = %dms, want 28500", p.DurationMS)
	}
	if p.Bytes != 14_680_064 {
		t.Errorf("bytes = %d, want 14680064", p.Bytes)
	}
}

func TestLadderIsCappedAt720p(t *testing.T) {
	// 1080p roughly doubles egress — the largest line in the Phase 2 cost model
	// — for a difference nobody perceives on a phone-sized autoplay feed.
	for _, r := range Ladder() {
		if r.Height > 1280 {
			t.Errorf("rendition %s is %dp — the ladder is capped at 720p", r.Label, r.Height)
		}
	}
}
