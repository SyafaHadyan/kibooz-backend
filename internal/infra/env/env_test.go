package env

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func valid() *Env {
	return &Env{
		JWTSecretKey:                 strings.Repeat("k", 32),
		AppTimezone:                  "UTC",
		TrashDailyLimit:              1,
		DeviceTokenTTLDays:           90,
		IdempotencyTTLHours:          168,
		PasswordHashWaitMilliseconds: 1000,
		SemesterStartMonths:          []int{1, 7},
		SentryEventsPerMinute:        30,
		ChatPhotoMaxMB:               4,
		ChatVideoMaxMB:               25,
		ChatMediaURLSeconds:          3600,
		ChatMaxSockets:               1000,
		ChatMaxSocketsPerUser:        5,

		RequestTimeoutSeconds: 10,

		AppPort: 8080, DBPort: 5432, RedisPort: 6379,
		BodyLimitMB: 8, VideoMaxMB: 100, VideoUploadURLSeconds: 900,
		UserLimiterMax: 120, AuthLimiterMax: 10, ClassCodeLimiterMax: 60, MoodBatchLimiterMax: 30, LimiterExpirationSeconds: 60,
		LeaderboardCacheSeconds: 300, JWTAccessExpiredMinutes: 60, JWTRefreshExpiredDays: 30, JWTSessionMaxDays: 90,
	}
}

func TestAValidConfigPasses(t *testing.T) {
	require.NoError(t, valid().validate())
}

func TestADeviceTokenLifetimeThatIsNotPositiveIsRejected(t *testing.T) {
	for _, days := range []int{0, -1, -90} {
		cfg := valid()
		cfg.DeviceTokenTTLDays = days

		err := cfg.validate()
		require.Error(t, err, "days %d", days)
		require.Contains(t, err.Error(), "DEVICE_TOKEN_TTL_DAYS")
	}

	cfg := valid()
	cfg.DeviceTokenTTLDays = 1
	require.NoError(t, cfg.validate())
}

func TestTheOtherRulesStillApply(t *testing.T) {
	short := valid()
	short.JWTSecretKey = "short"
	require.ErrorContains(t, short.validate(), "JWT_SECRET_KEY")

	zone := valid()
	zone.AppTimezone = "Not/AZone"
	require.ErrorContains(t, zone.validate(), "APP_TIMEZONE")

	limit := valid()
	limit.TrashDailyLimit = 0
	require.ErrorContains(t, limit.validate(), "TRASH_DAILY_LIMIT")
}

func TestARequestTimeoutThatIsNotPositiveIsRejected(t *testing.T) {
	for _, seconds := range []int{0, -1, -30} {
		cfg := valid()
		cfg.RequestTimeoutSeconds = seconds

		require.ErrorContains(t, cfg.validate(), "REQUEST_TIMEOUT_SECONDS", "seconds %d", seconds)
	}
}

func TestTheRequestTimeoutDefaultsToTenSeconds(t *testing.T) {
	setRequired(t)
	require.NoError(t, os.Unsetenv("REQUEST_TIMEOUT_SECONDS"))

	cfg, err := New()
	require.NoError(t, err)
	require.Equal(t, 10, cfg.RequestTimeoutSeconds)
}

func TestTheIdempotencyKeysAreKeptForSevenDaysByDefault(t *testing.T) {
	setRequired(t)
	require.NoError(t, os.Unsetenv("IDEMPOTENCY_TTL_HOURS"))

	cfg, err := New()
	require.NoError(t, err)
	require.Equal(t, 168, cfg.IdempotencyTTLHours)
}

func TestAnIdempotencyRetentionThatIsNotPositiveIsRejected(t *testing.T) {
	for _, hours := range []int{0, -1, -24} {
		cfg := valid()
		cfg.IdempotencyTTLHours = hours

		require.ErrorContains(t, cfg.validate(), "IDEMPOTENCY_TTL_HOURS", "hours %d", hours)
	}
}

// setRequired gives New the variables it cannot start without, and removes the one under test so that its default applies
func setRequired(t *testing.T) {
	t.Helper()

	t.Setenv("JWT_SECRET_KEY", strings.Repeat("k", 32))
	t.Setenv("DB_NAME", "kibooz")
	t.Setenv("DB_USERNAME", "kibooz")
	t.Setenv("DB_PASSWORD", strings.Repeat("d", 12))

	// Setenv first so that the original value comes back when the test ends
	t.Setenv("DEVICE_TOKEN_TTL_DAYS", "")
	require.NoError(t, os.Unsetenv("DEVICE_TOKEN_TTL_DAYS"))
}

func TestTheDeviceTokenLifetimeDefaultsToNinetyDays(t *testing.T) {
	setRequired(t)

	cfg, err := New()
	require.NoError(t, err)
	require.Equal(t, 90, cfg.DeviceTokenTTLDays)
}

func TestTheDeviceTokenLifetimeCanBeSet(t *testing.T) {
	setRequired(t)
	t.Setenv("DEVICE_TOKEN_TTL_DAYS", "30")

	cfg, err := New()
	require.NoError(t, err)
	require.Equal(t, 30, cfg.DeviceTokenTTLDays)
}

func TestStartupFailsForADeviceTokenLifetimeOfZero(t *testing.T) {
	setRequired(t)
	t.Setenv("DEVICE_TOKEN_TTL_DAYS", "0")

	_, err := New()
	require.ErrorContains(t, err, "DEVICE_TOKEN_TTL_DAYS")
}

func TestTheSampleJWTSecretIsRejected(t *testing.T) {
	for _, secret := range []string{
		"change-me-to-a-random-string-of-32-chars-or-more",
		"CHANGE-ME-TO-A-RANDOM-STRING-OF-32-CHARS-OR-MORE",
		"Change-Me-and-then-some-more-characters-here-1",
	} {
		cfg := valid()
		cfg.JWTSecretKey = secret

		require.ErrorContains(t, cfg.validate(), "sample value", secret)
	}

	cfg := valid()
	cfg.JWTSecretKey = "a-secret-that-mentions-change-me-in-the-middle-1234"
	require.NoError(t, cfg.validate())
}

// A limit of 0 switches a limiter off, a cache time of 0 never ends and a negative number of points breaks every claim
func TestValuesThatBreakTheServiceAreRejected(t *testing.T) {
	tests := map[string]func(*Env){
		"BODY_LIMIT_MB":                   func(e *Env) { e.BodyLimitMB = 0 },
		"VIDEO_MAX_MB":                    func(e *Env) { e.VideoMaxMB = 0 },
		"VIDEO_UPLOAD_URL_SECONDS":        func(e *Env) { e.VideoUploadURLSeconds = -1 },
		"USER_LIMITER_MAX":                func(e *Env) { e.UserLimiterMax = 0 },
		"AUTH_LIMITER_MAX":                func(e *Env) { e.AuthLimiterMax = 0 },
		"CLASS_CODE_LIMITER_MAX":          func(e *Env) { e.ClassCodeLimiterMax = 0 },
		"MOOD_BATCH_LIMITER_MAX":          func(e *Env) { e.MoodBatchLimiterMax = 0 },
		"LIMITER_EXPIRATION_SECONDS":      func(e *Env) { e.LimiterExpirationSeconds = 0 },
		"LEADERBOARD_CACHE_SECONDS":       func(e *Env) { e.LeaderboardCacheSeconds = 0 },
		"JWT_ACCESS_EXPIRED_MINUTES":      func(e *Env) { e.JWTAccessExpiredMinutes = 0 },
		"JWT_REFRESH_EXPIRED_DAYS":        func(e *Env) { e.JWTRefreshExpiredDays = -5 },
		"JWT_SESSION_MAX_DAYS":            func(e *Env) { e.JWTSessionMaxDays = 0 },
		"KEEPALIVE_SECONDS":               func(e *Env) { e.KeepaliveSeconds = -1 },
		"REDIS_DATABASE":                  func(e *Env) { e.RedisDatabase = -1 },
		"POINTS_ORGANIK":                  func(e *Env) { e.PointsOrganik = -1 },
		"POINTS_ANORGANIK":                func(e *Env) { e.PointsAnorganik = -1 },
		"POINTS_B3":                       func(e *Env) { e.PointsB3 = -1 },
		"IDEMPOTENCY_TTL_HOURS":           func(e *Env) { e.IdempotencyTTLHours = 0 },
		"SENTRY_EVENTS_PER_MINUTE":        func(e *Env) { e.SentryEventsPerMinute = 0 },
		"PASSWORD_HASH_WAIT_MILLISECONDS": func(e *Env) { e.PasswordHashWaitMilliseconds = 0 },
		"PASSWORD_HASH_CONCURRENCY":       func(e *Env) { e.PasswordHashConcurrency = -1 },
	}

	for name, change := range tests {
		cfg := valid()
		change(cfg)

		require.ErrorContains(t, cfg.validate(), name)
	}
}

func TestAKeepaliveOfZeroAndZeroPointsAreAllowed(t *testing.T) {
	cfg := valid()
	cfg.KeepaliveSeconds = 0
	cfg.PointsB3 = 0
	cfg.PointsOrganik = 0
	cfg.RedisDatabase = 0

	require.NoError(t, cfg.validate())
}

func TestAPortHasToBeBetweenOneAndSixtyFiveThousand(t *testing.T) {
	for name, change := range map[string]func(*Env, uint){
		"APP_PORT":   func(e *Env, v uint) { e.AppPort = v },
		"DB_PORT":    func(e *Env, v uint) { e.DBPort = v },
		"REDIS_PORT": func(e *Env, v uint) { e.RedisPort = v },
	} {
		for _, port := range []uint{0, 65536, 100000} {
			cfg := valid()
			change(cfg, port)

			require.ErrorContains(t, cfg.validate(), name, "port %d", port)
		}

		cfg := valid()
		change(cfg, 65535)
		require.NoError(t, cfg.validate())
	}
}

func TestTheTimezoneIsLoadedOnce(t *testing.T) {
	cfg := valid()
	cfg.AppTimezone = "Asia/Jakarta"
	require.NoError(t, cfg.validate())

	first := cfg.Location()
	require.Equal(t, "Asia/Jakarta", first.String())
	require.Same(t, first, cfg.Location(), "every call has to return the zone that validate loaded")

	// a config built without validate still answers
	bare := &Env{AppTimezone: "Asia/Jakarta"}
	require.Equal(t, "Asia/Jakarta", bare.Location().String())
	require.Equal(t, "UTC", (&Env{AppTimezone: "Not/AZone"}).Location().String())
}

func TestTheSemestersDefaultToJanuaryAndJuly(t *testing.T) {
	setRequired(t)

	cfg, err := New()
	require.NoError(t, err)
	require.Equal(t, []int{1, 7}, cfg.SemesterStartMonths)
}

func TestTheSemesterMonthsCanBeSet(t *testing.T) {
	setRequired(t)
	t.Setenv("SEMESTER_START_MONTHS", "1,5,9")

	cfg, err := New()
	require.NoError(t, err)
	require.Equal(t, []int{1, 5, 9}, cfg.SemesterStartMonths)
}

func TestASemesterMonthThatIsNotAMonthIsRejected(t *testing.T) {
	for _, months := range []string{"0", "13", "1,13", "-1"} {
		setRequired(t)
		t.Setenv("SEMESTER_START_MONTHS", months)

		_, err := New()
		require.ErrorContains(t, err, "SEMESTER_START_MONTHS", "months %q", months)
	}

	cfg := valid()
	cfg.SemesterStartMonths = nil

	require.ErrorContains(t, cfg.validate(), "SEMESTER_START_MONTHS")
}

func TestMetricsAreOffByDefaultAndTakeAnAddress(t *testing.T) {
	require.Empty(t, valid().MetricsAddress)

	for _, address := range []string{":9100", "127.0.0.1:9100", "0.0.0.0:9100", "[::1]:9100"} {
		cfg := valid()
		cfg.MetricsAddress = address

		require.NoError(t, cfg.validate(), address)
	}
}

func TestAMetricsAddressThatCannotWorkIsRejected(t *testing.T) {
	for _, address := range []string{"9100", "localhost", "127.0.0.1:", ":0", ":65536", ":abc", "127.0.0.1:8080", ":8080"} {
		cfg := valid()
		cfg.MetricsAddress = address

		err := cfg.validate()
		require.Error(t, err, address)
		require.Contains(t, err.Error(), "METRICS_ADDRESS", address)
	}
}

func TestTheErrorReportsAreOffByDefaultAndCapped(t *testing.T) {
	setRequired(t)
	require.NoError(t, os.Unsetenv("SENTRY_DSN"))
	require.NoError(t, os.Unsetenv("SENTRY_ENVIRONMENT"))
	require.NoError(t, os.Unsetenv("SENTRY_SERVER_NAME"))
	require.NoError(t, os.Unsetenv("SENTRY_EVENTS_PER_MINUTE"))

	cfg, err := New()
	require.NoError(t, err)
	require.Empty(t, cfg.SentryDSN, "an empty DSN switches the reports off")
	require.Equal(t, "production", cfg.SentryEnvironment)
	require.Empty(t, cfg.SentryServerName)
	require.Equal(t, 30, cfg.SentryEventsPerMinute)
}

func TestChatMediaSettingsNeedAPositiveValueAndABucketOfTheirOwn(t *testing.T) {
	for name, set := range map[string]func(*Env){
		"CHAT_PHOTO_MAX_MB":         func(e *Env) { e.ChatPhotoMaxMB = 0 },
		"CHAT_VIDEO_MAX_MB":         func(e *Env) { e.ChatVideoMaxMB = 0 },
		"CHAT_MEDIA_URL_SECONDS":    func(e *Env) { e.ChatMediaURLSeconds = 0 },
		"CHAT_MAX_SOCKETS":          func(e *Env) { e.ChatMaxSockets = 0 },
		"CHAT_MAX_SOCKETS_PER_USER": func(e *Env) { e.ChatMaxSocketsPerUser = 0 },
	} {
		cfg := valid()
		set(cfg)

		require.ErrorContains(t, cfg.validate(), name)
	}

	cfg := valid()
	cfg.S3BucketName, cfg.S3PrivateBucketName = "kibooz-production-media", "kibooz-production-media"
	require.ErrorContains(t, cfg.validate(), "S3_PRIVATE_BUCKET_NAME")

	cfg.S3PrivateBucketName = "kibooz-production-private"
	require.NoError(t, cfg.validate())
}

func TestThePrivateBucketNeedsTheSharedStorageSettingsToo(t *testing.T) {
	cfg := valid()
	cfg.S3PrivateBucketName = "kibooz-production-private"
	require.False(t, cfg.PrivateStorageEnabled(), "the account and the keys are missing")

	cfg.S3BucketName, cfg.S3AccessKeyID, cfg.S3AccessKeySecret = "kibooz-production-media", "id", "secret"
	cfg.S3PublicURL, cfg.S3AccountID = "https://media.example.test", "account"
	require.True(t, cfg.PrivateStorageEnabled())

	cfg.S3PrivateBucketName = ""
	require.False(t, cfg.PrivateStorageEnabled())
}
