package env

import (
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
