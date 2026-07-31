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
	OTPTTL     time.Duration

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
		OTPTTL:     duration("OTP_TTL", "5m"),

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

		ClickHouseURL:      optional("CLICKHOUSE_URL", ""),
		ClickHouseDatabase: optional("CLICKHOUSE_DATABASE", "analytics"),
		ClickHouseTable:    optional("CLICKHOUSE_TABLE", "reel_events"),
		ClickHouseUser:     optional("CLICKHOUSE_USER", ""),
		ClickHousePassword: optional("CLICKHOUSE_PASSWORD", ""),
		ClickHouseTee:      boolean("CLICKHOUSE_TEE", false),
	}

	// Guardrails that only apply once the process is actually deployed.
	if cfg.IsProduction() {
		if cfg.OTPDevEcho {
			problems = append(problems, "OTP_DEV_ECHO must be false in production")
		}
		if len(cfg.JWTSigningKey) < 32 {
			problems = append(problems, "JWT_SIGNING_KEY must be at least 32 bytes in production")
		}
		if strings.Contains(cfg.DatabaseURL, "sslmode=disable") {
			problems = append(problems, "DATABASE_URL must not disable TLS in production")
		}
	}

	if len(problems) > 0 {
		return Config{}, fmt.Errorf("invalid configuration:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return cfg, nil
}
