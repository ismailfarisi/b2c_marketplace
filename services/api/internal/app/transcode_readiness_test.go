package app

import (
	"strings"
	"testing"

	"github.com/marketplace/api/internal/config"
)

// The API cannot boot in production today, and this is why.
//
// `stub` publishes reels whose playlists point at nothing, and `ffmpeg` keeps
// its job table in memory so a worker restart strands every in-flight reel.
// Both are refused in production, correctly — the guardrails are not the
// problem. The problem is that nothing else is implemented, so there is no
// value of TRANSCODE_VENDOR that both exists and is permitted.
//
// That is a missing vendor integration, not a bug, and it is the single most
// concrete blocker between this repository and a deployment. It is written as
// a test rather than a TODO because a TODO does not tell you when it stops
// being true: implement a real provider, register it in buildTranscoder, and
// this test fails and asks to be updated.
func TestNoTranscodeVendorIsProductionReady(t *testing.T) {
	prod := config.Config{Env: "production"}
	if !prod.IsProduction() {
		t.Fatal("fixture is wrong")
	}

	for _, vendor := range []string{"", "stub", "ffmpeg", "mux", "cloudflare", "aws-mediaconvert"} {
		cfg := prod
		cfg.TranscodeVendor = vendor

		_, err := buildTranscoder(cfg, nil)
		if err == nil {
			t.Fatalf("TRANSCODE_VENDOR=%q now works in production.\n\n"+
				"That is good news and this test is now wrong. Delete it, and "+
				"update the production-readiness note in docs/QA-FINDINGS.md "+
				"that points at it.", vendor)
		}
		t.Logf("%-18q refused: %s", vendor, firstLine(err.Error()))
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
