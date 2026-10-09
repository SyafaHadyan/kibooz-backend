package env

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func valid() *Env {
	return &Env{
		JWTSecretKey:       strings.Repeat("k", 32),
		AppTimezone:        "UTC",
		TrashDailyLimit:    1,
		DeviceTokenTTLDays: 90,

		RequestTimeoutSeconds: 10,
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
