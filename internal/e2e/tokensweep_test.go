package e2e

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/sessions"
)

func tokenRows(t *testing.T, token string) int64 {
	t.Helper()

	var count int64

	require.NoError(t, testDB.Raw("SELECT count(*) FROM refresh_tokens WHERE token_hash = ?", refreshHash(token)).Scan(&count).Error)

	return count
}

// The sweep deletes what no longer matters for every account, which a refresh does for its own account only
func TestTheRefreshTokenSweepDeletesOnlyTheTokensThatNoLongerMatter(t *testing.T) {
	maxAge := 90 * 24 * time.Hour

	// a session that refreshed once, so its first token is a used one, and then reached its longest lifetime
	old := registerGuru(t, "Sweep Tua")
	refreshed := call(t, http.MethodPost, "/api/v1/auth/refresh-token", "", map[string]any{"refreshToken": old.RefreshToken})
	require.Equal(t, http.StatusOK, refreshed.Status, "body %v", refreshed.Body)

	oldNext := refreshed.data("refreshToken").(string)

	// the expiry of a token is capped at the end of its session, so the live token of such a session has expired as well
	require.NoError(t, testDB.Exec(
		"UPDATE refresh_tokens SET session_started_at = now() - make_interval(secs => ?), expires_at = now() - interval '1 hour' WHERE family_id = ?::uuid",
		(maxAge+time.Hour).Seconds(), familyOf(t, old.RefreshToken),
	).Error)

	// a session that refreshed once and is young, so its used token has to stay for the theft check
	young := registerGuru(t, "Sweep Muda")
	moved := call(t, http.MethodPost, "/api/v1/auth/refresh-token", "", map[string]any{"refreshToken": young.RefreshToken})
	require.Equal(t, http.StatusOK, moved.Status, "body %v", moved.Body)

	youngNext := moved.data("refreshToken").(string)

	// a token that was never used and has expired
	expired := registerGuru(t, "Sweep Basi")
	require.NoError(t, testDB.Exec("UPDATE refresh_tokens SET expires_at = now() - interval '1 minute' WHERE token_hash = ?", refreshHash(expired.RefreshToken)).Error)

	removed, err := sessions.NewSweeper(testDB, maxAge).Sweep(t.Context())
	require.NoError(t, err)
	require.GreaterOrEqual(t, removed, 3, "the used and the live token of the old session and the expired one")

	require.Zero(t, tokenRows(t, old.RefreshToken), "a used token of a session past its longest lifetime")
	require.Zero(t, tokenRows(t, expired.RefreshToken), "a token that expired without being used")

	// the live token of the old session expired with the session, so it went as well
	require.Zero(t, tokenRows(t, oldNext))

	require.EqualValues(t, 1, tokenRows(t, young.RefreshToken), "the used token of a young session stays, because showing it again ends the session")
	require.EqualValues(t, 1, tokenRows(t, youngNext), "the live token of a young session stays")

	// a used token that has expired, in a session that is still young, has to stay as well, because showing it again must end the session
	require.NoError(t, testDB.Exec("UPDATE refresh_tokens SET expires_at = now() - interval '1 minute' WHERE token_hash = ?", refreshHash(young.RefreshToken)).Error)

	_, err = sessions.NewSweeper(testDB, maxAge).Sweep(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 1, tokenRows(t, young.RefreshToken), "a used token goes with the end of its session and not with its own expiry")

	again := call(t, http.MethodPost, "/api/v1/auth/refresh-token", "", map[string]any{"refreshToken": youngNext})
	require.Equal(t, http.StatusOK, again.Status, "the young session still refreshes, body %v", again.Body)
}

// A token that a refresh or a sign out holds is skipped, so the sweep never waits for a lock and cannot join a circle of waits
func TestTheRefreshTokenSweepSkipsATokenThatIsLockedAndDoesNotWait(t *testing.T) {
	account := registerGuru(t, "Sweep Kunci")
	require.NoError(t, testDB.Exec("UPDATE refresh_tokens SET expires_at = now() - interval '1 minute' WHERE token_hash = ?", refreshHash(account.RefreshToken)).Error)

	holder := testDB.Begin()
	require.NoError(t, holder.Error)

	t.Cleanup(func() { _ = holder.Rollback().Error })

	require.NoError(t, holder.Exec("SELECT id FROM refresh_tokens WHERE token_hash = ? FOR UPDATE", refreshHash(account.RefreshToken)).Error)

	started := time.Now()
	_, err := sessions.NewSweeper(testDB, 90*24*time.Hour).Sweep(t.Context())

	require.NoError(t, err)
	require.Less(t, time.Since(started), 5*time.Second, "the sweep did not wait for the lock")
	require.EqualValues(t, 1, tokenRows(t, account.RefreshToken), "the locked token was left for the next run")

	require.NoError(t, holder.Rollback().Error)

	_, err = sessions.NewSweeper(testDB, 90*24*time.Hour).Sweep(t.Context())
	require.NoError(t, err)
	require.Zero(t, tokenRows(t, account.RefreshToken), "the next run takes it")
}
