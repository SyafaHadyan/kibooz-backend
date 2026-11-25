package env

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAVerificationThatRequiresTheCodeNeedsAKeyToSendIt(t *testing.T) {
	cfg := valid()
	require.NoError(t, cfg.validate(), "the check is off by default")

	cfg.EmailVerificationRequired = true
	require.ErrorContains(t, cfg.validate(), "RESEND_API_KEY")

	cfg.ResendAPIKey = "   "
	require.ErrorContains(t, cfg.validate(), "RESEND_API_KEY", "a key of spaces is no key")

	cfg.ResendAPIKey = "re_testkey"
	require.NoError(t, cfg.validate())
}

func TestTheMailSettingsHaveSafeDefaults(t *testing.T) {
	setRequired(t)

	cfg, err := New()
	require.NoError(t, err)
	require.False(t, cfg.EmailVerificationRequired)
	require.Empty(t, cfg.ResendAPIKey)
	require.Equal(t, "https://api.resend.com", cfg.ResendAPIURL)
	require.Equal(t, "no-reply@mail.syafahadyan.com", cfg.EmailFrom)
}

func TestTheResendAddressIsHttpsOrALoopbackAddress(t *testing.T) {
	for address, ok := range map[string]bool{
		"https://api.resend.com": true, "http://127.0.0.1:8099": true, "http://[::1]:8099": true, "": true,
		"http://api.resend.com": false, "http://10.0.0.5": false, "ftp://api.resend.com": false, "api.resend.com": false,
		"http://localhost:8099": false,
	} {
		err := (&Env{ResendAPIURL: address}).checkResendURL()

		if ok {
			require.NoError(t, err, address)
		} else {
			require.Error(t, err, address)
		}
	}
}
