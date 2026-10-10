// Package env loads and parses environment variables, with an optional .env file for local development
package env

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

type Env struct {
	AppEnv                   string `env:"APP_ENV" envDefault:"development"`
	AppPort                  uint   `env:"APP_PORT" envDefault:"8080"`
	AppTimezone              string `env:"APP_TIMEZONE" envDefault:"Asia/Jakarta"`
	BodyLimitMB              int    `env:"BODY_LIMIT_MB" envDefault:"8"`
	RequestTimeoutSeconds    int    `env:"REQUEST_TIMEOUT_SECONDS" envDefault:"10"`
	VideoMaxMB               int    `env:"VIDEO_MAX_MB" envDefault:"100"`
	VideoUploadURLSeconds    int    `env:"VIDEO_UPLOAD_URL_SECONDS" envDefault:"900"`
	UserLimiterMax           int    `env:"USER_LIMITER_MAX" envDefault:"120"`
	LimiterExpirationSeconds int    `env:"LIMITER_EXPIRATION_SECONDS" envDefault:"60"`
	AuthLimiterMax           int    `env:"AUTH_LIMITER_MAX" envDefault:"10"`
	TrustProxy               bool   `env:"TRUST_PROXY" envDefault:"false"`
	ProxyHeader              string `env:"PROXY_HEADER" envDefault:"X-Forwarded-For"`
	DBHost                   string `env:"DB_HOST" envDefault:"127.0.0.1"`
	DBPort                   uint   `env:"DB_PORT" envDefault:"5432"`
	DBName                   string `env:"DB_NAME,required"`
	DBUsername               string `env:"DB_USERNAME,required"`
	DBPassword               string `env:"DB_PASSWORD,required"`
	DBSSLMode                string `env:"DB_SSL_MODE" envDefault:"disable"`
	RedisAddress             string `env:"REDIS_ADDRESS" envDefault:"127.0.0.1"`
	RedisPort                uint   `env:"REDIS_PORT" envDefault:"6379"`
	RedisTLS                 bool   `env:"REDIS_TLS" envDefault:"false"`
	RedisUsername            string `env:"REDIS_USERNAME"`
	RedisPassword            string `env:"REDIS_PASSWORD"`
	RedisKeyPrefix           string `env:"REDIS_KEY_PREFIX"`
	RedisDatabase            int    `env:"REDIS_DATABASE" envDefault:"0"`
	LeaderboardCacheSeconds  int    `env:"LEADERBOARD_CACHE_SECONDS" envDefault:"300"`
	KeepaliveSeconds         int    `env:"KEEPALIVE_SECONDS" envDefault:"60"`
	JWTSecretKey             string `env:"JWT_SECRET_KEY,required"`
	JWTAccessExpiredMinutes  int    `env:"JWT_ACCESS_EXPIRED_MINUTES" envDefault:"60"`
	JWTRefreshExpiredDays    int    `env:"JWT_REFRESH_EXPIRED_DAYS" envDefault:"30"`
	JWTSessionMaxDays        int    `env:"JWT_SESSION_MAX_DAYS" envDefault:"90"`
	DeviceTokenTTLDays       int    `env:"DEVICE_TOKEN_TTL_DAYS" envDefault:"90"`
	S3Endpoint               string `env:"S3_ENDPOINT"`
	S3AccountID              string `env:"S3_ACCOUNT_ID"`
	S3Region                 string `env:"S3_REGION" envDefault:"auto"`
	S3BucketName             string `env:"S3_BUCKET_NAME"`
	S3AccessKeyID            string `env:"S3_ACCESS_KEY_ID"`
	S3AccessKeySecret        string `env:"S3_ACCESS_KEY_SECRET"`
	S3PublicURL              string `env:"S3_PUBLIC_URL"`
	PointsOrganik            int    `env:"POINTS_ORGANIK" envDefault:"10"`
	PointsAnorganik          int    `env:"POINTS_ANORGANIK" envDefault:"15"`
	PointsB3                 int    `env:"POINTS_B3" envDefault:"0"`
	TrashDailyLimit          int    `env:"TRASH_DAILY_LIMIT" envDefault:"5"`

	// location is the parsed AppTimezone, set by validate so that it is not loaded from the zone data on every request
	location *time.Location
}

// New reads .env when present, then parses and validates the process environment
func New() (*Env, error) {
	err := godotenv.Load()
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("load .env file: %w", err)
	}

	cfg := new(Env)

	err = env.Parse(cfg)
	if err != nil {
		return nil, fmt.Errorf("parse environment: %w", err)
	}

	err = cfg.validate()
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

// placeholderSecretPrefix starts the sample JWT secret of .env.example, which anyone can read in the repository
const placeholderSecretPrefix = "change-me"

func (e *Env) validate() error {
	if len(e.JWTSecretKey) < 32 {
		return errors.New("JWT_SECRET_KEY must be at least 32 characters")
	}

	if strings.HasPrefix(strings.ToLower(e.JWTSecretKey), placeholderSecretPrefix) {
		return errors.New("JWT_SECRET_KEY still holds the sample value from .env.example, set a random secret of your own")
	}

	loc, err := time.LoadLocation(e.AppTimezone)
	if err != nil {
		return fmt.Errorf("invalid APP_TIMEZONE %q: %w", e.AppTimezone, err)
	}

	e.location = loc

	// A limit of 0 would switch a limiter off, and a lifetime that is not positive gives tokens, caches and signed addresses
	// that are over when they are issued, or in the case of the leaderboard cache that never end
	for _, rule := range []struct {
		name  string
		value int
		min   int
	}{
		{"BODY_LIMIT_MB", e.BodyLimitMB, 1},
		{"VIDEO_MAX_MB", e.VideoMaxMB, 1},
		{"VIDEO_UPLOAD_URL_SECONDS", e.VideoUploadURLSeconds, 1},
		{"USER_LIMITER_MAX", e.UserLimiterMax, 1},
		{"AUTH_LIMITER_MAX", e.AuthLimiterMax, 1},
		{"LIMITER_EXPIRATION_SECONDS", e.LimiterExpirationSeconds, 1},
		{"LEADERBOARD_CACHE_SECONDS", e.LeaderboardCacheSeconds, 1},
		{"JWT_ACCESS_EXPIRED_MINUTES", e.JWTAccessExpiredMinutes, 1},
		{"JWT_REFRESH_EXPIRED_DAYS", e.JWTRefreshExpiredDays, 1},
		// a session that no limit ends could be kept alive for ever with a stolen refresh token
		{"JWT_SESSION_MAX_DAYS", e.JWTSessionMaxDays, 1},
		// a request without a deadline can hold a database connection for as long as a storage call or a lock wait lasts
		{"REQUEST_TIMEOUT_SECONDS", e.RequestTimeoutSeconds, 1},
		// a device token that is already over when it is issued gives nobody a bucket of their own
		{"DEVICE_TOKEN_TTL_DAYS", e.DeviceTokenTTLDays, 1},
		{"TRASH_DAILY_LIMIT", e.TrashDailyLimit, 1},
		// 0 turns the keepalive off, which is what the pipelines that run against a throwaway stack do
		{"KEEPALIVE_SECONDS", e.KeepaliveSeconds, 0},
		{"REDIS_DATABASE", e.RedisDatabase, 0},
		// points are stored under a check constraint that refuses a negative number, so every claim would fail
		{"POINTS_ORGANIK", e.PointsOrganik, 0},
		{"POINTS_ANORGANIK", e.PointsAnorganik, 0},
		{"POINTS_B3", e.PointsB3, 0},
	} {
		if rule.value < rule.min {
			return fmt.Errorf("%s must be at least %d", rule.name, rule.min)
		}
	}

	for _, port := range []struct {
		name  string
		value uint
	}{
		{"APP_PORT", e.AppPort},
		{"DB_PORT", e.DBPort},
		{"REDIS_PORT", e.RedisPort},
	} {
		if port.value < 1 || port.value > maxPort {
			return fmt.Errorf("%s must be between 1 and %d", port.name, maxPort)
		}
	}

	return nil
}

const maxPort = 65535

// Location returns the school timezone used for "today" and weekly boundaries
func (e *Env) Location() *time.Location {
	if e.location != nil {
		return e.location
	}

	loc, err := time.LoadLocation(e.AppTimezone)
	if err != nil {
		return time.UTC
	}

	return loc
}

// StorageEnabled reports whether object storage is fully configured
func (e *Env) StorageEnabled() bool {
	return e.S3BucketName != "" && e.S3AccessKeyID != "" && e.S3AccessKeySecret != "" &&
		e.S3PublicURL != "" && (e.S3Endpoint != "" || e.S3AccountID != "")
}
