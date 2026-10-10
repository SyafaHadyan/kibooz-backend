package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func refreshHash(token string) string {
	sum := sha256.Sum256([]byte(token))

	return hex.EncodeToString(sum[:])
}

// ageUse pretends that a refresh token was used some time ago, which the test cannot wait for
func ageUse(t *testing.T, token string, by time.Duration) {
	t.Helper()

	result := testDB.Exec("UPDATE refresh_tokens SET used_at = used_at - make_interval(secs => ?) WHERE token_hash = ?", by.Seconds(), refreshHash(token))
	require.NoError(t, result.Error)
	require.EqualValues(t, 1, result.RowsAffected, "the token has to be a used one")
}

func tokensOfSession(t *testing.T, token string) int64 {
	t.Helper()

	var count int64

	require.NoError(t, testDB.Raw(
		"SELECT count(*) FROM refresh_tokens WHERE family_id IN (SELECT family_id FROM refresh_tokens WHERE token_hash = ?)",
		refreshHash(token),
	).Scan(&count).Error)

	return count
}

func TestAUsedRefreshTokenThatIsShownAgainEndsItsSession(t *testing.T) {
	client := redisClient(t)
	ctx := t.Context()

	guru := registerGuru(t, "Stolen Token")

	first := refresh(t, guru.RefreshToken)
	require.Equal(t, http.StatusOK, first.Status, "body %v", first.Body)

	second := first.data("refreshToken").(string)

	t.Run("a replay a moment later is only refused and the session goes on", func(t *testing.T) {
		refresh(t, guru.RefreshToken).requireError(t, http.StatusUnauthorized, "AUTH_REFRESH_INVALID")

		// a client that lost the answer to its first call keeps the new token that it gets with the next one
		third := refresh(t, second)
		require.Equal(t, http.StatusOK, third.Status, "body %v", third.Body)

		second = third.data("refreshToken").(string)
	})

	t.Run("a replay after the grace period ends the whole session", func(t *testing.T) {
		ageUse(t, guru.RefreshToken, 2*time.Minute)

		// Redis flags a used token for a minute, and after that the database decides
		require.NoError(t, client.FlushDB(ctx).Err())

		require.EqualValues(t, 3, tokensOfSession(t, second), "two used tokens and the one in use")

		refresh(t, guru.RefreshToken).requireError(t, http.StatusUnauthorized, "AUTH_REFRESH_INVALID")

		// the token of the owner is gone too, so the thief and the owner both have to sign in again
		refresh(t, second).requireError(t, http.StatusUnauthorized, "AUTH_REFRESH_INVALID")
		require.Zero(t, tokensOfSession(t, second))
	})

	t.Run("a sign in on another device is not affected", func(t *testing.T) {
		login := call(t, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
			"email": guru.Email, "password": guru.Password, "role": "GURU",
		})
		require.Equal(t, http.StatusOK, login.Status, "body %v", login.Body)

		other := login.data("refreshToken").(string)

		// the first session is over, and a token of it that is shown again must not reach the new one
		require.NoError(t, client.FlushDB(ctx).Err())
		refresh(t, guru.RefreshToken).requireError(t, http.StatusUnauthorized, "AUTH_REFRESH_INVALID")

		require.Equal(t, http.StatusOK, refresh(t, other).Status)
	})
}

func TestLogoutEndsTheWholeSession(t *testing.T) {
	guru := registerGuru(t, "Whole Session")

	rotated := refresh(t, guru.RefreshToken)
	require.Equal(t, http.StatusOK, rotated.Status, "body %v", rotated.Body)

	current := rotated.data("refreshToken").(string)
	require.EqualValues(t, 2, tokensOfSession(t, current))

	require.Equal(t, http.StatusOK, call(t, http.MethodPost, "/api/v1/auth/logout", "", map[string]any{"refreshToken": current}).Status)
	require.Zero(t, tokensOfSession(t, current), "the used token of the session goes with it")

	refresh(t, current).requireError(t, http.StatusUnauthorized, "AUTH_REFRESH_INVALID")
}

func TestASessionEndsAtItsLongestLifetime(t *testing.T) {
	const maxDays = 90

	t.Run("a swap near the end hands out a token that stops with the session", func(t *testing.T) {
		guru := registerGuru(t, "Near The End")

		started := time.Now().Add(-(maxDays*24*time.Hour - 24*time.Hour))
		require.NoError(t, testDB.Exec("UPDATE refresh_tokens SET session_started_at = ? WHERE token_hash = ?", started, refreshHash(guru.RefreshToken)).Error)

		res := refresh(t, guru.RefreshToken)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		var expires time.Time

		require.NoError(t, testDB.Raw("SELECT expires_at FROM refresh_tokens WHERE token_hash = ?", refreshHash(res.data("refreshToken").(string))).Scan(&expires).Error)

		require.WithinDuration(t, started.Add(maxDays*24*time.Hour), expires, time.Minute)
	})

	t.Run("a swap after the end gives nothing and clears the session", func(t *testing.T) {
		guru := registerGuru(t, "Past The End")

		started := time.Now().Add(-(maxDays*24*time.Hour + time.Hour))
		require.NoError(t, testDB.Exec("UPDATE refresh_tokens SET session_started_at = ? WHERE token_hash = ?", started, refreshHash(guru.RefreshToken)).Error)

		refresh(t, guru.RefreshToken).requireError(t, http.StatusUnauthorized, "AUTH_REFRESH_INVALID")
		require.Zero(t, tokensOfSession(t, guru.RefreshToken))
	})

	t.Run("a new sign in starts a new session", func(t *testing.T) {
		guru := registerGuru(t, "Fresh")

		login := call(t, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
			"email": guru.Email, "password": guru.Password, "role": "GURU",
		})
		require.Equal(t, http.StatusOK, login.Status, "body %v", login.Body)

		var started time.Time

		require.NoError(t, testDB.Raw("SELECT session_started_at FROM refresh_tokens WHERE token_hash = ?", refreshHash(login.data("refreshToken").(string))).Scan(&started).Error)
		require.WithinDuration(t, time.Now(), started, time.Minute)
	})
}
