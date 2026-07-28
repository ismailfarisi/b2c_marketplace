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

	S3Endpoint      string
	S3Region        string
	S3Bucket        string
	S3AccessKeyID   string
	S3SecretKey     string
	S3ForcePathStyle bool

	DefaultCurrency string
	DefaultLocale   string
	DefaultTimezone string
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
		S3Region:         optional("S3_REGION", "me-central-1"),
		S3Bucket:         optional("S3_BUCKET", ""),
		S3AccessKeyID:    optional("S3_ACCESS_KEY_ID", ""),
		S3SecretKey:      optional("S3_SECRET_ACCESS_KEY", ""),
		S3ForcePathStyle: boolean("S3_FORCE_PATH_STYLE", false),

		DefaultCurrency: optional("DEFAULT_CURRENCY", "AED"),
		DefaultLocale:   optional("DEFAULT_LOCALE", "en"),
		DefaultTimezone: optional("DEFAULT_TIMEZONE", "Asia/Dubai"),
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
