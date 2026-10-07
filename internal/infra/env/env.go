// Package env loads and parses environment variables, with an optional .env file for local development
package env

import (
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

type Env struct {
	AppEnv                   string `env:"APP_ENV" envDefault:"development"`
	AppPort                  uint   `env:"APP_PORT" envDefault:"8080"`
	AppTimezone              string `env:"APP_TIMEZONE" envDefault:"Asia/Jakarta"`
	BodyLimitMB              int    `env:"BODY_LIMIT_MB" envDefault:"8"`
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
	RedisDatabase            int    `env:"REDIS_DATABASE" envDefault:"0"`
	LeaderboardCacheSeconds  int    `env:"LEADERBOARD_CACHE_SECONDS" envDefault:"300"`
	KeepaliveSeconds         int    `env:"KEEPALIVE_SECONDS" envDefault:"60"`
	JWTSecretKey             string `env:"JWT_SECRET_KEY,required"`
	JWTAccessExpiredMinutes  int    `env:"JWT_ACCESS_EXPIRED_MINUTES" envDefault:"60"`
	JWTRefreshExpiredDays    int    `env:"JWT_REFRESH_EXPIRED_DAYS" envDefault:"30"`
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

func (e *Env) validate() error {
	if len(e.JWTSecretKey) < 32 {
		return errors.New("JWT_SECRET_KEY must be at least 32 characters")
	}

	_, err := time.LoadLocation(e.AppTimezone)
	if err != nil {
		return fmt.Errorf("invalid APP_TIMEZONE %q: %w", e.AppTimezone, err)
	}

	if e.TrashDailyLimit < 1 {
		return errors.New("TRASH_DAILY_LIMIT must be at least 1")
	}

	return nil
}

// Location returns the school timezone used for "today" and weekly boundaries
func (e *Env) Location() *time.Location {
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
