// Package env loads and parses environment variables, with an optional .env file for local development
package env

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

type Env struct {
	AppPort                  uint   `env:"APP_PORT" envDefault:"8080"`
	AppTimezone              string `env:"APP_TIMEZONE" envDefault:"Asia/Jakarta"`
	BodyLimitMB              int    `env:"BODY_LIMIT_MB" envDefault:"8"`
	RequestTimeoutSeconds    int    `env:"REQUEST_TIMEOUT_SECONDS" envDefault:"10"`
	ClassVideoMaxMB          int    `env:"CLASS_VIDEO_MAX_MB" envDefault:"100"`
	VideoUploadURLSeconds    int    `env:"VIDEO_UPLOAD_URL_SECONDS" envDefault:"900"`
	UserLimiterMax           int    `env:"USER_LIMITER_MAX" envDefault:"120"`
	LimiterExpirationSeconds int    `env:"LIMITER_EXPIRATION_SECONDS" envDefault:"60"`
	AuthLimiterMax           int    `env:"AUTH_LIMITER_MAX" envDefault:"10"`
	ClassCodeLimiterMax      int    `env:"CLASS_CODE_LIMITER_MAX" envDefault:"60"`
	MoodBatchLimiterMax      int    `env:"MOOD_BATCH_LIMITER_MAX" envDefault:"30"`
	// MetricsAddress is where the Prometheus endpoint listens, such as ":9100", and empty switches it off. It is a listener of
	// its own and must only be reachable from the private network of the scraper.
	MetricsAddress          string `env:"METRICS_ADDRESS"`
	TrustProxy              bool   `env:"TRUST_PROXY" envDefault:"false"`
	ProxyHeader             string `env:"PROXY_HEADER" envDefault:"X-Forwarded-For"`
	DBHost                  string `env:"DB_HOST" envDefault:"127.0.0.1"`
	DBPort                  uint   `env:"DB_PORT" envDefault:"5432"`
	DBName                  string `env:"DB_NAME,required"`
	DBUsername              string `env:"DB_USERNAME,required"`
	DBPassword              string `env:"DB_PASSWORD,required"`
	DBSSLMode               string `env:"DB_SSL_MODE" envDefault:"disable"`
	RedisAddress            string `env:"REDIS_ADDRESS" envDefault:"127.0.0.1"`
	RedisPort               uint   `env:"REDIS_PORT" envDefault:"6379"`
	RedisTLS                bool   `env:"REDIS_TLS" envDefault:"false"`
	RedisUsername           string `env:"REDIS_USERNAME"`
	RedisPassword           string `env:"REDIS_PASSWORD"`
	RedisKeyPrefix          string `env:"REDIS_KEY_PREFIX"`
	RedisDatabase           int    `env:"REDIS_DATABASE" envDefault:"0"`
	LeaderboardCacheSeconds int    `env:"LEADERBOARD_CACHE_SECONDS" envDefault:"300"`
	KeepaliveSeconds        int    `env:"KEEPALIVE_SECONDS" envDefault:"60"`
	JWTSecretKey            string `env:"JWT_SECRET_KEY,required"`
	JWTAccessExpiredMinutes int    `env:"JWT_ACCESS_EXPIRED_MINUTES" envDefault:"15"`
	JWTRefreshExpiredDays   int    `env:"JWT_REFRESH_EXPIRED_DAYS" envDefault:"30"`
	JWTSessionMaxDays       int    `env:"JWT_SESSION_MAX_DAYS" envDefault:"90"`
	// RefreshReuseLeewaySeconds is how long after its use a refresh token can be shown again and get a new token of the same
	// session, for a client that lost the answer to its first call. 0 switches it off, and any reuse ends the session.
	RefreshReuseLeewaySeconds int    `env:"REFRESH_REUSE_LEEWAY_SECONDS" envDefault:"10"`
	DeviceTokenTTLDays        int    `env:"DEVICE_TOKEN_TTL_DAYS" envDefault:"90"`
	S3Endpoint                string `env:"S3_ENDPOINT"`
	S3AccountID               string `env:"S3_ACCOUNT_ID"`
	S3Region                  string `env:"S3_REGION" envDefault:"auto"`
	S3PublicBucketName        string `env:"S3_PUBLIC_BUCKET_NAME"`
	S3AccessKeyID             string `env:"S3_ACCESS_KEY_ID"`
	S3AccessKeySecret         string `env:"S3_ACCESS_KEY_SECRET"`
	S3PublicURL               string `env:"S3_PUBLIC_URL"`
	// S3PrivateBucketName is the bucket of the chat media, which is never public. Empty switches chat media off.
	S3PrivateBucketName string `env:"S3_PRIVATE_BUCKET_NAME"`
	ChatPhotoMaxMB      int    `env:"CHAT_PHOTO_MAX_MB" envDefault:"4"`
	ChatVideoMaxMB      int    `env:"CHAT_VIDEO_MAX_MB" envDefault:"25"`
	ChatMediaURLSeconds int    `env:"CHAT_MEDIA_URL_SECONDS" envDefault:"3600"`
	// ChatMaxSockets is how many chat sockets one API copy holds, and ChatMaxSocketsPerUser how many one account keeps open
	// FirebaseCredentialsFile is the key file of the Firebase service account. Empty switches the push notifications off.
	FirebaseCredentialsFile string `env:"FIREBASE_CREDENTIALS_FILE"`
	ChatMaxSockets          int    `env:"CHAT_MAX_SOCKETS" envDefault:"1000"`
	ChatMaxSocketsPerUser   int    `env:"CHAT_MAX_SOCKETS_PER_USER" envDefault:"5"`
	// UploadConcurrency is how many photo uploads one API copy handles at the same time, and the next one is refused with 429 and
	// a Retry-After, because each upload holds several copies of its photo in memory
	UploadConcurrency   int `env:"UPLOAD_CONCURRENCY" envDefault:"4"`
	PointsOrganik       int `env:"POINTS_ORGANIK" envDefault:"10"`
	PointsAnorganik     int `env:"POINTS_ANORGANIK" envDefault:"15"`
	PointsB3            int `env:"POINTS_B3" envDefault:"0"`
	TrashDailyLimit     int `env:"TRASH_DAILY_LIMIT" envDefault:"5"`
	IdempotencyTTLHours int `env:"IDEMPOTENCY_TTL_HOURS" envDefault:"168"`
	// SentryDSN is where the error reports go, and empty switches them off. SentryEventsPerMinute is the cap on how many leave in a minute.
	SentryDSN             string `env:"SENTRY_DSN"`
	SentryEnvironment     string `env:"SENTRY_ENVIRONMENT" envDefault:"production"`
	SentryServerName      string `env:"SENTRY_SERVER_NAME"`
	SentryEventsPerMinute int    `env:"SENTRY_EVENTS_PER_MINUTE" envDefault:"30"`
	// PasswordHashConcurrency is how many passwords the process hashes or checks at the same time, and 0 means one for each
	// CPU. A request that finds every slot taken waits PasswordHashWaitMilliseconds and is then told to try again.
	PasswordHashConcurrency int `env:"PASSWORD_HASH_CONCURRENCY" envDefault:"0"`
	// PasswordHashCost is the bcrypt cost of new hashes. 10 is the default of bcrypt and the lowest that OWASP accepts. A lower
	// value is for tests only, and every cost up to 14 is allowed because the time of a hash doubles with each step.
	PasswordHashCost             int `env:"PASSWORD_HASH_COST" envDefault:"10"`
	PasswordHashWaitMilliseconds int `env:"PASSWORD_HASH_WAIT_MILLISECONDS" envDefault:"1000"`
	// SemesterStartMonths are the months in which a semester begins, the leaderboard starts from zero on the first day of each
	SemesterStartMonths []int `env:"SEMESTER_START_MONTHS" envSeparator:"," envDefault:"1,7"`

	// location is the parsed AppTimezone, set by validate so that it is not loaded from the zone data on every request
	location *time.Location
}

// New reads .env when present, then parses and validates the process environment
func New() (*Env, error) {
	err := godotenv.Load()
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("load .env file: %w", err)
	}

	err = refuseRenamed(os.LookupEnv)
	if err != nil {
		return nil, err
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

// renamedSettings are the settings that changed their name, from the old name to the new one. The old name is not read any more,
// so a file that still holds it would run with the default of the new one without a word. It is refused at start instead.
var renamedSettings = map[string]string{
	"VIDEO_MAX_MB":   "CLASS_VIDEO_MAX_MB",
	"S3_BUCKET_NAME": "S3_PUBLIC_BUCKET_NAME",
}

// refuseRenamed fails when a setting of the old name is still set, and names the one that replaces it
func refuseRenamed(lookup func(string) (string, bool)) error {
	var found []string

	for oldName, newName := range renamedSettings {
		if _, set := lookup(oldName); set {
			found = append(found, fmt.Sprintf("%s was renamed to %s", oldName, newName))
		}
	}

	if len(found) == 0 {
		return nil
	}

	sort.Strings(found)

	return fmt.Errorf("rename these settings, the old names are not read any more: %s", strings.Join(found, ", "))
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

	if e.MetricsAddress != "" {
		_, port, err := net.SplitHostPort(e.MetricsAddress)
		if err != nil || port == "" {
			return fmt.Errorf("METRICS_ADDRESS must look like host:port or :port, got %q", e.MetricsAddress)
		}

		if number, err := strconv.Atoi(port); err != nil || number < 1 || number > maxPort || number == int(e.AppPort) {
			return fmt.Errorf("METRICS_ADDRESS needs a port from 1 to %d that is not APP_PORT, got %q", maxPort, e.MetricsAddress)
		}
	}

	// a photo travels in a multipart body, and Fiber refuses a body over BODY_LIMIT_MB before the handler runs. So the largest photo
	// has to be below the limit of the body, with room for the framing of the form, or a photo that is allowed would get a 413.
	if e.ChatPhotoMaxMB >= e.BodyLimitMB {
		return fmt.Errorf("CHAT_PHOTO_MAX_MB is %d and has to be smaller than BODY_LIMIT_MB, which is %d", e.ChatPhotoMaxMB, e.BodyLimitMB)
	}

	// the chat media would be served to anyone from the public bucket, so the two names must differ
	if e.S3PrivateBucketName != "" && e.S3PrivateBucketName == e.S3PublicBucketName {
		return errors.New("S3_PRIVATE_BUCKET_NAME must not be the public bucket S3_PUBLIC_BUCKET_NAME")
	}

	if len(e.SemesterStartMonths) == 0 {
		return errors.New("SEMESTER_START_MONTHS needs at least one month")
	}

	for _, month := range e.SemesterStartMonths {
		if month < 1 || month > 12 {
			return fmt.Errorf("SEMESTER_START_MONTHS has %d, a month is a number from 1 to 12", month)
		}
	}

	// A limit of 0 would switch a limiter off, and a lifetime that is not positive gives tokens, caches and signed addresses
	// that are over when they are issued, or in the case of the leaderboard cache that never end
	for _, rule := range []struct {
		name  string
		value int
		min   int
	}{
		{"BODY_LIMIT_MB", e.BodyLimitMB, 1},
		{"CLASS_VIDEO_MAX_MB", e.ClassVideoMaxMB, 1},
		{"VIDEO_UPLOAD_URL_SECONDS", e.VideoUploadURLSeconds, 1},
		{"CHAT_PHOTO_MAX_MB", e.ChatPhotoMaxMB, 1},
		{"CHAT_VIDEO_MAX_MB", e.ChatVideoMaxMB, 1},
		// a read address that is over when it is issued shows nobody a photo
		{"CHAT_MEDIA_URL_SECONDS", e.ChatMediaURLSeconds, 1},
		// a limit of 0 would let a flood of sockets use up the memory of the copy, which is what the limits are for
		{"CHAT_MAX_SOCKETS", e.ChatMaxSockets, 1},
		{"CHAT_MAX_SOCKETS_PER_USER", e.ChatMaxSocketsPerUser, 1},
		// a gate of 0 would refuse every photo, and the gate is what keeps a few uploads from using up the memory of the copy
		{"UPLOAD_CONCURRENCY", e.UploadConcurrency, 1},
		{"USER_LIMITER_MAX", e.UserLimiterMax, 1},
		{"AUTH_LIMITER_MAX", e.AuthLimiterMax, 1},
		{"CLASS_CODE_LIMITER_MAX", e.ClassCodeLimiterMax, 1},
		{"MOOD_BATCH_LIMITER_MAX", e.MoodBatchLimiterMax, 1},
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
		// a key that is forgotten at once cannot catch the retry of an action that was queued offline
		{"IDEMPOTENCY_TTL_HOURS", e.IdempotencyTTLHours, 1},
		// a cap of 0 would drop every report, which is what leaving the DSN empty is for
		{"SENTRY_EVENTS_PER_MINUTE", e.SentryEventsPerMinute, 1},
		// 0 turns the keepalive off, which is what the pipelines that run against a throwaway stack do
		// a wait of 0 would refuse a request whenever the slots happen to be taken, even for a spike of a few milliseconds
		{"PASSWORD_HASH_WAIT_MILLISECONDS", e.PasswordHashWaitMilliseconds, 1},
		// 0 is one slot for each CPU except one
		{"PASSWORD_HASH_CONCURRENCY", e.PasswordHashConcurrency, 0},
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

	// a leeway that is long gives a stolen token that much time to mint tokens of its own, and a negative one has no meaning
	if e.RefreshReuseLeewaySeconds < 0 || e.RefreshReuseLeewaySeconds > maxRefreshReuseLeewaySeconds {
		return fmt.Errorf("REFRESH_REUSE_LEEWAY_SECONDS must be between 0 and %d", maxRefreshReuseLeewaySeconds)
	}

	// below 4 bcrypt would not hash at all, and above 14 a single sign in would take seconds of a whole CPU
	if e.PasswordHashCost < minPasswordHashCost || e.PasswordHashCost > maxPasswordHashCost {
		return fmt.Errorf("PASSWORD_HASH_COST must be between %d and %d", minPasswordHashCost, maxPasswordHashCost)
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

// maxRefreshReuseLeewaySeconds is the longest REFRESH_REUSE_LEEWAY_SECONDS. A thief who holds a used token can mint tokens of its own
// only for this long after the owner used it, and a retry of an app that lost its answer needs seconds and not minutes.
const maxRefreshReuseLeewaySeconds = 60

// the range of PASSWORD_HASH_COST, bcrypt itself refuses a cost below 4
const (
	minPasswordHashCost = 4
	maxPasswordHashCost = 14
)

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

// PrivateStorageEnabled reports whether the private bucket of the chat media can be used. It shares the account and the keys of
// the public bucket, so it needs the same settings and a bucket name of its own.
func (e *Env) PrivateStorageEnabled() bool {
	return e.StorageEnabled() && e.S3PrivateBucketName != ""
}

// StorageEnabled reports whether object storage is fully configured
func (e *Env) StorageEnabled() bool {
	return e.S3PublicBucketName != "" && e.S3AccessKeyID != "" && e.S3AccessKeySecret != "" &&
		e.S3PublicURL != "" && (e.S3Endpoint != "" || e.S3AccountID != "")
}
