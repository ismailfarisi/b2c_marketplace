// Package config loads runtime configuration from the environment.
//
// Everything is read once at startup and validated eagerly. A misconfigured
// process should fail to boot, not fail on the first request that happens to
// touch the missing value.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env      string
	HTTPAddr string
	LogLevel string

	DatabaseURL string
	RedisURL    string

	JWTSigningKey   string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration

	OTPDevEcho bool

	// WhatsApp Cloud API (Meta). Both set enables real WhatsApp sends;
	// both empty keeps the log provider (dev) or refusal (production).
	WhatsAppAccessToken   string
	WhatsAppPhoneNumberID string
	WhatsAppAPIVersion    string
	OTPTTL                time.Duration

	S3Endpoint string
	// S3PublicEndpoint is the host presigned URLs name, when clients reach the
	// object store somewhere this process does not. Empty means the same host.
	// See objectstore.Config.PublicEndpoint — getting this wrong produces an
	// upload URL the phone cannot reach, or a HEAD the server cannot make.
	S3PublicEndpoint string
	S3Region         string
	S3Bucket         string
	S3AccessKeyID    string
	S3SecretKey      string
	S3ForcePathStyle bool

	DefaultCurrency string
	DefaultLocale   string
	DefaultTimezone string

	// Phase 2: media, moderation, feed.
	MediaCDNBaseURL string
	TranscodeVendor string
	AnthropicAPIKey string
	ModerationModel string
	OpenSearchURL   string
	OpenSearchIndex string
	FFProbeBinary   string
	// FFmpegBinary is used only by the local transcode provider. Empty means
	// "ffmpeg on PATH", which is right on a laptop and wrong in a container
	// where the binary is pinned to a known build.
	FFmpegBinary string

	// AWS Elemental MediaConvert — the first transcode provider permitted in
	// production. Unused by any other vendor, and validated by the provider
	// itself at wiring time rather than required here, so a deployment running
	// ffmpeg locally does not have to set them.
	MediaConvertRole        string
	MediaConvertQueue       string
	MediaConvertDestination string

	// Phase 4: ranking, social, analytics warehouse.
	//
	// Unset means the Postgres event sink stays in use. That is correct for
	// development and for a launch district, and wrong at city scale —
	// PLAN-VIDEO-FIRST §3 is explicit that view events must never live in
	// Postgres at volume.
	ClickHouseURL      string
	ClickHouseDatabase string
	ClickHouseTable    string
	ClickHouseUser     string
	ClickHousePassword string
	// ClickHouseTee writes every event to both stores during the cutover, so
	// the numbers can be compared before anything depends on the new one.
	ClickHouseTee bool
}

func (c Config) IsProduction() bool { return c.Env == "production" }

// Load reads configuration from the environment, returning every validation
// failure at once. Reporting one missing variable per restart wastes minutes
// each time in a container that takes 30 seconds to come up.
func Load() (Config, error) {
	var problems []string

	require := func(key string) string {
		v := strings.TrimSpace(os.Getenv(key))
		if v == "" {
			problems = append(problems, key+" is required")
		}
		return v
	}

	optional := func(key, fallback string) string {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
		return fallback
	}

	duration := func(key, fallback string) time.Duration {
		raw := optional(key, fallback)
		d, err := time.ParseDuration(raw)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %q is not a duration", key, raw))
			return 0
		}
		return d
	}

	boolean := func(key string, fallback bool) bool {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			return fallback
		}
		b, err := strconv.ParseBool(raw)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %q is not a boolean", key, raw))
			return fallback
		}
		return b
	}

	cfg := Config{
		Env:      optional("APP_ENV", "development"),
		HTTPAddr: optional("HTTP_ADDR", ":8080"),
		LogLevel: optional("LOG_LEVEL", "info"),

		DatabaseURL: require("DATABASE_URL"),
		RedisURL:    optional("REDIS_URL", ""),

		JWTSigningKey:   require("JWT_SIGNING_KEY"),
		AccessTokenTTL:  duration("ACCESS_TOKEN_TTL", "15m"),
		RefreshTokenTTL: duration("REFRESH_TOKEN_TTL", "720h"),

		OTPDevEcho: boolean("OTP_DEV_ECHO", false),

		WhatsAppAccessToken:   optional("WHATSAPP_ACCESS_TOKEN", ""),
		WhatsAppPhoneNumberID: optional("WHATSAPP_PHONE_NUMBER_ID", ""),
		WhatsAppAPIVersion:    optional("WHATSAPP_API_VERSION", "v21.0"),
		OTPTTL:                duration("OTP_TTL", "5m"),

		S3Endpoint:       optional("S3_ENDPOINT", ""),
		S3PublicEndpoint: optional("S3_PUBLIC_ENDPOINT", ""),
		S3Region:         optional("S3_REGION", "me-central-1"),
		S3Bucket:         optional("S3_BUCKET", ""),
		S3AccessKeyID:    optional("S3_ACCESS_KEY_ID", ""),
		S3SecretKey:      optional("S3_SECRET_ACCESS_KEY", ""),
		S3ForcePathStyle: boolean("S3_FORCE_PATH_STYLE", false),

		DefaultCurrency: optional("DEFAULT_CURRENCY", "AED"),
		DefaultLocale:   optional("DEFAULT_LOCALE", "en"),
		DefaultTimezone: optional("DEFAULT_TIMEZONE", "Asia/Dubai"),

		MediaCDNBaseURL: optional("MEDIA_CDN_BASE_URL", ""),
		TranscodeVendor: optional("TRANSCODE_VENDOR", "stub"),
		AnthropicAPIKey: optional("ANTHROPIC_API_KEY", ""),
		// Opus 5 is the default. Haiku 4.5 is roughly 5× cheaper at moderation
		// volume — but its minimum cacheable prefix is 4096 tokens against
		// Opus's 512, so switching without checking the policy prompt still
		// crosses that line turns caching off and can cost more than it saves.
		ModerationModel: optional("MODERATION_MODEL", "claude-opus-5"),
		OpenSearchURL:   optional("OPENSEARCH_URL", ""),
		OpenSearchIndex: optional("OPENSEARCH_INDEX", "reels"),
		FFProbeBinary:   optional("FFPROBE_BINARY", ""),
		FFmpegBinary:    optional("FFMPEG_BINARY", ""),

		MediaConvertRole:        optional("MEDIACONVERT_ROLE_ARN", ""),
		MediaConvertQueue:       optional("MEDIACONVERT_QUEUE_ARN", ""),
		MediaConvertDestination: optional("MEDIACONVERT_DESTINATION", ""),

		ClickHouseURL:      optional("CLICKHOUSE_URL", ""),
		ClickHouseDatabase: optional("CLICKHOUSE_DATABASE", "analytics"),
		ClickHouseTable:    optional("CLICKHOUSE_TABLE", "reel_events"),
		ClickHouseUser:     optional("CLICKHOUSE_USER", ""),
		ClickHousePassword: optional("CLICKHOUSE_PASSWORD", ""),
		ClickHouseTee:      boolean("CLICKHOUSE_TEE", false),
	}

	if (cfg.WhatsAppAccessToken == "") != (cfg.WhatsAppPhoneNumberID == "") {
		problems = append(problems, "WHATSAPP_ACCESS_TOKEN and WHATSAPP_PHONE_NUMBER_ID must be set together")
	}

	// Guardrails that only apply once the process is actually deployed.
	if cfg.IsProduction() {
		if cfg.OTPDevEcho {
			problems = append(problems, "OTP_DEV_ECHO must be false in production")
		}
		if len(cfg.JWTSigningKey) < 32 {
			problems = append(problems, "JWT_SIGNING_KEY must be at least 32 bytes in production")
		}
		// Length is not entropy. The key CI uses —
		// "ci-only-signing-key-at-least-32-bytes-long" — is 42 characters and
		// passes the check above, and a copied .env.example passes it too. A
		// signing key that is guessable forges any session in the product, so
		// the obvious placeholders are named rather than trusted.
		if word := placeholderIn(cfg.JWTSigningKey); word != "" {
			problems = append(problems,
				"JWT_SIGNING_KEY looks like a placeholder (contains "+word+
					") — generate one with: openssl rand -base64 48")
		}
		// The object store, required for the same reason the transcoder and the
		// moderator are, and it was the one leg of the media pipeline that was
		// not.
		//
		// BuildMedia returns nil when the bucket is unset and the API runs
		// without the media surface, which its comment defends as "a deployment
		// that only serves orders is a legitimate configuration". That holds in
		// development. In production it means the entire point of a video-first
		// marketplace is switched off by an unset variable, announced by one
		// warning line, and discovered when someone asks why the feed is empty.
		if cfg.S3Bucket == "" {
			problems = append(problems,
				"S3_BUCKET is required in production — without it the media "+
					"surface is silently disabled and the feed has nothing to serve")
		}
		if strings.Contains(cfg.DatabaseURL, "sslmode=disable") {
			problems = append(problems, "DATABASE_URL must not disable TLS in production")
		}
		// `auto` resolves to this machine's LAN address, which in a deployment
		// is a private container address. A presigned URL built on one resolves
		// for nobody and the symptom is every upload failing with no mention of
		// a hostname — so it is refused here rather than resolved.
		for name, value := range map[string]string{
			"S3_PUBLIC_ENDPOINT": cfg.S3PublicEndpoint,
			"MEDIA_CDN_BASE_URL": cfg.MediaCDNBaseURL,
		} {
			if strings.Contains(value, AutoHost) {
				problems = append(problems,
					name+" cannot be \""+AutoHost+"\" in production — name a real host")
			}
		}
	} else {
		// Development. Both of these are rendered on a *device*, so they cannot
		// say localhost — and they cannot say 10.0.2.2 either, because the web
		// admin has to reach the same objects from a browser on the host. The
		// only address that works for all of them is this machine's on the LAN,
		// and it changes with the DHCP lease.
		//
		// Resolved here rather than written into .env, where it goes stale and
		// takes every image and every upload with it.
		for _, target := range []*string{&cfg.S3PublicEndpoint, &cfg.MediaCDNBaseURL} {
			resolved, err := resolveAuto(*target)
			if err != nil {
				problems = append(problems, err.Error())
				continue
			}
			*target = resolved
		}
	}

	if len(problems) > 0 {
		return Config{}, fmt.Errorf("invalid configuration:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return cfg, nil
}

// placeholderIn reports the first placeholder marker found in a secret, or "".
//
// Substring matching on a small list, deliberately. Entropy estimation would be
// the clever answer and it is the wrong one here: a base64 key from
// `openssl rand` and a determined person's "correcthorsebatterystaple" score
// similarly, and a false positive at boot is an outage. These words appear in
// the values this repository itself ships — .env.example, the CI signing key —
// which are the ones that actually get copied into a deployment by accident.
func placeholderIn(secret string) string {
	lower := strings.ToLower(secret)
	for _, word := range []string{
		"changeme", "change-me", "change_me",
		"placeholder", "example", "ci-only", "replace",
		"your-", "xxxx", "insecure", "notsecret", "dev-secret",
	} {
		if strings.Contains(lower, word) {
			return word
		}
	}
	return ""
}
