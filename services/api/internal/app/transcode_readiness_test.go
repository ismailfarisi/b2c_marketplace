package app

import (
	"context"
	"strings"
	"testing"

	"github.com/marketplace/api/internal/config"
)

// Which transcode vendors may run in production, and why.
//
// This file used to assert the opposite. `TestNoTranscodeVendorIsProductionReady`
// enumerated every vendor name and required all of them to be refused, because
// `stub` and `ffmpeg` are both rejected in production for good reasons and
// nothing else was implemented — so the API could not boot in production at
// all. It was written as a test rather than a TODO precisely so that
// implementing one would break it and force this update. It did.
//
// The refusals it protected are still asserted below. Losing those while
// gaining a real provider would be a poor trade: `stub` publishes reels whose
// playlists point at nothing, and `ffmpeg` keeps its job table in memory, so a
// worker restart strands every in-flight reel permanently.

func prodConfig() config.Config {
	return config.Config{
		Env:             "production",
		S3Region:        "me-central-1",
		MediaCDNBaseURL: "https://cdn.example.ae",
	}
}

func TestProductionRefusesTranscodersThatLoseReels(t *testing.T) {
	for _, tc := range []struct {
		vendor string
		want   string
		why    string
	}{
		{"", "stub", "an unset vendor falls through to the stub"},
		{"stub", "stub", "publishes reels whose playlists point at nothing"},
		{"ffmpeg", "in-memory", "a worker restart strands every in-flight reel"},
		{"mux", "not implemented", "naming a vendor does not make it exist"},
	} {
		t.Run(tc.vendor, func(t *testing.T) {
			cfg := prodConfig()
			cfg.TranscodeVendor = tc.vendor

			_, err := buildTranscoder(context.Background(), cfg, nil)
			if err == nil {
				t.Fatalf("production accepted %q — %s", tc.vendor, tc.why)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the error should say why (%q); got: %v", tc.want, err)
			}
		})
	}
}

// The one that is allowed, and the conditions on it.
//
// MediaConvert is permitted in production for a structural reason rather than a
// preference: it holds no job state in this process. The job lives in AWS under
// an id persisted with the reel, so a restart, a redeploy, or a second worker
// running the reconciliation sweep all behave identically — which is exactly
// what disqualifies the ffmpeg provider.
func TestMediaConvertIsAcceptedInProductionWhenConfigured(t *testing.T) {
	// LoadDefaultConfig walks the credential chain. Kept off the network so
	// this does not depend on the machine running it having an AWS identity,
	// or hang for the IMDS timeout on one that does not.
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")

	cfg := prodConfig()
	cfg.TranscodeVendor = "aws-mediaconvert"
	cfg.MediaConvertRole = "arn:aws:iam::123456789012:role/MediaConvertRole"
	cfg.MediaConvertDestination = "s3://marketplace-media/hls"

	p, err := buildTranscoder(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("a fully configured MediaConvert must be accepted in production: %v", err)
	}
	if p.Name() != "aws-mediaconvert" {
		t.Fatalf("got provider %q", p.Name())
	}
}

// Misconfiguration is refused at boot, by name.
//
// The alternative is an AccessDenied on the first upload of the day, which
// reads as an AWS outage rather than as one unset variable.
func TestMediaConvertRefusesIncompleteConfigAtBoot(t *testing.T) {
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")

	full := prodConfig()
	full.TranscodeVendor = "aws-mediaconvert"
	full.MediaConvertRole = "arn:aws:iam::123456789012:role/MediaConvertRole"
	full.MediaConvertDestination = "s3://marketplace-media/hls"

	for _, tc := range []struct {
		name   string
		mutate func(*config.Config)
		want   string
	}{
		{"no role", func(c *config.Config) { c.MediaConvertRole = "" }, "MEDIACONVERT_ROLE_ARN"},
		{"no destination", func(c *config.Config) { c.MediaConvertDestination = "" }, "MEDIACONVERT_DESTINATION"},
		{
			// A bucket name without the scheme is the likely typo, and
			// MediaConvert would accept the job and then fail it minutes later.
			"destination is not an s3 url",
			func(c *config.Config) { c.MediaConvertDestination = "marketplace-media/hls" },
			"MEDIACONVERT_DESTINATION",
		},
		{"no cdn base", func(c *config.Config) { c.MediaCDNBaseURL = "" }, "MEDIA_CDN_BASE_URL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := full
			tc.mutate(&cfg)

			_, err := buildTranscoder(context.Background(), cfg, nil)
			if err == nil {
				t.Fatal("expected a boot failure")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the error must name the variable to set (%q); got: %v", tc.want, err)
			}
		})
	}
}

// Development keeps its local providers. A laptop has no MediaConvert account,
// and requiring one would mean nobody can run the pipeline.
func TestDevelopmentStillGetsTheLocalProviders(t *testing.T) {
	for _, vendor := range []string{"", "stub", "ffmpeg"} {
		cfg := config.Config{Env: "development", MediaCDNBaseURL: "http://localhost:9000"}
		cfg.TranscodeVendor = vendor
		if _, err := buildTranscoder(context.Background(), cfg, nil); err != nil {
			t.Errorf("development must accept %q, got: %v", vendor, err)
		}
	}
}
