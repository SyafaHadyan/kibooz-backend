package devicetoken_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/infra/devicetoken"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
)

// the keys are built and not written out, because they are only test values
var (
	signer      = strings.Repeat("s", 40)
	otherSigner = strings.Repeat("o", 40)
)

func newTokens(signingKey string, days int) *devicetoken.Tokens {
	return devicetoken.New(&env.Env{JWTSecretKey: signingKey, DeviceTokenTTLDays: days})
}

func issue(t *testing.T, tokens *devicetoken.Tokens, email string, presented string) string {
	t.Helper()

	token, err := tokens.Issue(email, presented)
	require.NoError(t, err)

	return token
}

func TestAnIssuedTokenVerifiesForItsEmail(t *testing.T) {
	tokens := newTokens(signer, 90)
	token := issue(t, tokens, "teacher@example.com", "")

	id, ok := tokens.Verify(token, "teacher@example.com")
	require.True(t, ok)
	require.Len(t, id, 16)
}

func TestTheLetterCaseAndSpacesOfTheEmailDoNotMatter(t *testing.T) {
	tokens := newTokens(signer, 90)
	token := issue(t, tokens, "Teacher@Example.com", "")

	_, ok := tokens.Verify(token, "  teacher@example.COM ")
	require.True(t, ok)
}

func TestATokenIsOnlyValidForTheEmailItWasIssuedFor(t *testing.T) {
	tokens := newTokens(signer, 90)
	token := issue(t, tokens, "attacker@example.com", "")

	_, ok := tokens.Verify(token, "victim@example.com")
	require.False(t, ok, "the token of an attacker's own account must not help against another email")
}

func TestATokenSignedWithAnotherSecretIsRejected(t *testing.T) {
	token := issue(t, newTokens(otherSigner, 90), "a@example.com", "")

	_, ok := newTokens(signer, 90).Verify(token, "a@example.com")
	require.False(t, ok)
}

func TestInstancesWithTheSameConfigAcceptEachOthersTokens(t *testing.T) {
	token := issue(t, newTokens(signer, 90), "a@example.com", "")

	_, ok := newTokens(signer, 90).Verify(token, "a@example.com")
	require.True(t, ok, "the limiter and the use case build their own instances")
}

func TestAnExpiredTokenIsRejected(t *testing.T) {
	// a one day lifetime that is already over can be built by issuing with a negative one
	expired := issue(t, newTokens(signer, -1), "a@example.com", "")

	_, ok := newTokens(signer, 90).Verify(expired, "a@example.com")
	require.False(t, ok)
}

func TestATokenThatIsTamperedWithIsRejected(t *testing.T) {
	tokens := newTokens(signer, 90)
	token := issue(t, tokens, "a@example.com", "")
	parts := strings.Split(token, ".")

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)

	payload[20] ^= 0xff // flips a bit of the device id

	forged := parts[0] + "." + base64.RawURLEncoding.EncodeToString(payload) + "." + parts[2]
	_, ok := tokens.Verify(forged, "a@example.com")
	require.False(t, ok)

	badSignature := parts[0] + "." + parts[1] + "." + base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	_, ok = tokens.Verify(badSignature, "a@example.com")
	require.False(t, ok)
}

func TestMalformedTokensAreRejected(t *testing.T) {
	tokens := newTokens(signer, 90)

	for _, token := range []string{"", "x", "v1", "v1..", "v1.a.b", "v2.a.b", "a.b.c.d", "v1.!!!.???", strings.Repeat("a", 5000)} {
		_, ok := tokens.Verify(token, "a@example.com")
		require.False(t, ok, "token %q", token)
	}
}

func TestADeviceKeepsItsIdWhenItSignsInAgain(t *testing.T) {
	tokens := newTokens(signer, 90)
	first := issue(t, tokens, "a@example.com", "")
	second := issue(t, tokens, "a@example.com", first)

	firstID, ok := tokens.Verify(first, "a@example.com")
	require.True(t, ok)

	secondID, ok := tokens.Verify(second, "a@example.com")
	require.True(t, ok)
	require.Equal(t, firstID, secondID)
}

func TestAPresentedTokenOfAnotherEmailOrFromNowhereGivesANewDevice(t *testing.T) {
	tokens := newTokens(signer, 90)
	other := issue(t, tokens, "other@example.com", "")

	mine := issue(t, tokens, "a@example.com", other)
	again := issue(t, tokens, "a@example.com", "garbage")

	mineID, ok := tokens.Verify(mine, "a@example.com")
	require.True(t, ok)

	againID, ok := tokens.Verify(again, "a@example.com")
	require.True(t, ok)
	require.NotEqual(t, mineID, againID, "every new device gets its own id")
}

func TestTokensStayWellInsideTheRequestSizeLimit(t *testing.T) {
	token := issue(t, newTokens(signer, 90), "a@example.com", "")

	require.Less(t, len(token), 256)
}
