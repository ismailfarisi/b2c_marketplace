package config

import (
	"strings"
	"testing"
)

// The production guardrails had no test at all, which is a strange gap for the
// one function whose entire job is refusing to start. Every branch below is a
// way a deployment can be wrong while looking fine, and the cost of each is a
// production incident rather than a failed build.
//
// The pre-existing rules are covered here too, not just the new ones. A
// guardrail nobody tests is a guardrail somebody deletes while refactoring, and
// the deletion is invisible: the process still boots, which is the symptom the
// rule exists to prevent.

// prodEnv is a deployment that should boot. Tests copy it and break one thing.
func prodEnv() map[string]string {
	return map[string]string{
		"APP_ENV":            "production",
		"DATABASE_URL":       "postgres://u:p@db:5432/marketplace?sslmode=require",
		"JWT_SIGNING_KEY":    "AZ7kQm2vX9pL4tR8wN3sB6yH1jF5dG0cV2nM8qW4eT7uY9iO",
		"S3_BUCKET":          "marketplace-media",
		"S3_PUBLIC_ENDPOINT": "https://media.example.ae",
		"MEDIA_CDN_BASE_URL": "https://cdn.example.ae",
		// Passes config.Load and would still be refused by
		// app.buildTranscoder. See TestNoTranscodeVendorIsProductionReady —
		// there is no value for this that boots in production today.
		"TRANSCODE_VENDOR":  "ffmpeg",
		"ANTHROPIC_API_KEY": "sk-ant-not-a-real-key",
	}
}

func load(t *testing.T, env map[string]string) (Config, error) {
	t.Helper()
	// Setenv restores the previous value at test end and forbids t.Parallel,
	// which is what makes this safe to do to a process-wide table.
	for k, v := range env {
		t.Setenv(k, v)
	}
	return Load()
}

func TestAWellFormedProductionConfigBoots(t *testing.T) {
	// The control. Without it, a test asserting "this is refused" proves
	// nothing — every config would be refused and every test would pass.
	if _, err := load(t, prodEnv()); err != nil {
		t.Fatalf("a valid production config must boot, got: %v", err)
	}
}

func TestProductionRefusals(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		value string
		want  string
		why   string
	}{
		{
			name:  "the OTP is echoed to the logs",
			key:   "OTP_DEV_ECHO",
			value: "true",
			want:  "OTP_DEV_ECHO",
			why: "the code is written to the log in plaintext; anyone who can " +
				"read logs can sign in as any user in the product",
		},
		{
			name:  "the signing key is short",
			key:   "JWT_SIGNING_KEY",
			value: "short",
			want:  "at least 32 bytes",
			why:   "a brute-forced signing key forges every session",
		},
		{
			name:  "the signing key is CI's",
			key:   "JWT_SIGNING_KEY",
			value: "ci-only-signing-key-at-least-32-bytes-long",
			want:  "placeholder",
			why: "42 characters, so the length rule passes it — and it is " +
				"committed in this repository's own workflow file",
		},
		{
			name:  "the signing key came from the example file",
			key:   "JWT_SIGNING_KEY",
			value: "changeme-changeme-changeme-changeme-changeme",
			want:  "placeholder",
			why:   "the single most likely value in a hurried first deploy",
		},
		{
			name:  "the database connection is not encrypted",
			key:   "DATABASE_URL",
			value: "postgres://u:p@db:5432/marketplace?sslmode=disable",
			want:  "must not disable TLS",
			why:   "credentials and personal data in clear text between hosts",
		},
		{
			name:  "there is no object store",
			key:   "S3_BUCKET",
			value: "",
			want:  "S3_BUCKET is required",
			why: "media is silently disabled and a video-first marketplace " +
				"serves a feed with nothing in it",
		},
		{
			name:  "the transcoder is the stub",
			key:   "TRANSCODE_VENDOR",
			value: "stub",
			want:  "",
			why:   "reels publish with playlists that point at nothing",
		},
		{
			name:  "the public endpoint is auto",
			key:   "S3_PUBLIC_ENDPOINT",
			value: AutoHost,
			want:  AutoHost,
			why: "it resolves to a private container address, so every " +
				"presigned URL is unreachable and nothing says why",
		},
		{
			name:  "the CDN base is auto",
			key:   "MEDIA_CDN_BASE_URL",
			value: AutoHost,
			want:  AutoHost,
			why:   "same, for playback rather than upload",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := prodEnv()
			env[tc.key] = tc.value

			_, err := load(t, env)

			// TRANSCODE_VENDOR is enforced when the media pipeline is built
			// rather than by Load, so it is listed here for the record and
			// asserted only where the rule actually lives.
			if tc.want == "" {
				t.Skipf("enforced in app.buildTranscoder, not config.Load: %s", tc.why)
			}
			if err == nil {
				t.Fatalf("production booted with %s=%q — %s", tc.key, tc.value, tc.why)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error should mention %q so the operator can act on it.\n"+
					"got: %v", tc.want, err)
			}
		})
	}
}

// Development is not production, and the guardrails must not leak into it.
// A laptop has no TLS on Postgres and no bucket, and refusing to start there
// would mean nobody can run the thing.
func TestDevelopmentIsNotHeldToProductionRules(t *testing.T) {
	env := map[string]string{
		"APP_ENV":         "development",
		"DATABASE_URL":    "postgres://marketplace:marketplace@localhost:5432/marketplace?sslmode=disable",
		"JWT_SIGNING_KEY": "dev-secret",
		"OTP_DEV_ECHO":    "true",
		// Named hosts rather than `auto`: resolveAuto dials out to discover the
		// LAN address, and a unit test must not depend on this machine having a
		// network.
		"S3_PUBLIC_ENDPOINT": "http://localhost:9000",
		"MEDIA_CDN_BASE_URL": "http://localhost:9000",
	}
	if _, err := load(t, env); err != nil {
		t.Fatalf("development must boot on a laptop, got: %v", err)
	}
}

// Every problem at once, not the first one.
//
// The function's own doc gives the reason — "reporting one missing variable per
// restart wastes minutes each time in a container that takes 30 seconds to come
// up" — and that promise is only kept if the errors actually accumulate.
func TestEveryProblemIsReportedTogether(t *testing.T) {
	env := prodEnv()
	env["JWT_SIGNING_KEY"] = "changeme"
	env["DATABASE_URL"] = "postgres://u:p@db:5432/m?sslmode=disable"
	env["S3_BUCKET"] = ""

	_, err := load(t, env)
	if err == nil {
		t.Fatal("expected refusal")
	}
	for _, want := range []string{"JWT_SIGNING_KEY", "TLS", "S3_BUCKET"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("all problems should be reported at once; %q is missing from:\n%v",
				want, err)
		}
	}
}

func TestPlaceholderDetection(t *testing.T) {
	// A real key must not trip the check. A false positive here is an outage
	// at the worst possible moment, which is why this is a word list and not
	// an entropy estimate — see placeholderIn.
	for _, ok := range []string{
		"AZ7kQm2vX9pL4tR8wN3sB6yH1jF5dG0cV2nM8qW4eT7uY9iO",
		"hunter2hunter2hunter2hunter2hunter2hunter2",
		"9f8e7d6c5b4a39281706f5e4d3c2b1a09f8e7d6c5b4a3928",
	} {
		if word := placeholderIn(ok); word != "" {
			t.Errorf("%q is a legitimate key but matched %q", ok, word)
		}
	}
	for _, bad := range []string{
		"ci-only-signing-key-at-least-32-bytes-long",
		"CHANGEME-CHANGEME-CHANGEME-CHANGEME",
		"your-signing-key-here-please-replace-this",
		"example-key-example-key-example-key",
	} {
		if placeholderIn(bad) == "" {
			t.Errorf("%q should have been caught as a placeholder", bad)
		}
	}
}
