package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authrepository "github.com/SyafaHadyan/kibooz-backend/internal/app/auth/repository"
	userrepository "github.com/SyafaHadyan/kibooz-backend/internal/app/user/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
)

// inFlightRefresh opens the transaction of a refresh and stops before it commits. It has marked the token as used, and
// the function it returns stores the next token and commits, which is what the rest of that request does.
func inFlightRefresh(t *testing.T, token string) (finish func()) {
	t.Helper()

	tx := testDB.Begin()
	require.NoError(t, tx.Error)

	t.Cleanup(func() { _ = tx.Rollback().Error })

	used := tx.Exec("UPDATE refresh_tokens SET used_at = now() WHERE token_hash = ? AND used_at IS NULL", refreshHash(token))
	require.NoError(t, used.Error)
	require.EqualValues(t, 1, used.RowsAffected, "the token has to be one that was not used")

	return func() {
		t.Helper()

		next := tx.Exec(
			"INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at, family_id, session_started_at) "+
				"SELECT gen_random_uuid(), user_id, ?, expires_at, family_id, session_started_at FROM refresh_tokens WHERE token_hash = ?",
			refreshHash(uuid.NewString()), refreshHash(token),
		)
		require.NoError(t, next.Error)
		require.EqualValues(t, 1, next.RowsAffected)
		require.NoError(t, tx.Commit().Error)
	}
}

// startWhileRefreshing runs end in the background and returns once it waits for the lock of the refresh that is under way
func startWhileRefreshing(t *testing.T, end func(ctx context.Context) error) <-chan error {
	t.Helper()

	done := make(chan error, 1)

	go func() { done <- end(context.Background()) }()

	require.Eventually(t, func() bool {
		var waiting int64

		err := testDB.Raw(
			"SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND pid <> pg_backend_pid() " +
				"AND query LIKE '%refresh_tokens%FOR UPDATE%'",
		).Scan(&waiting).Error

		return err == nil && waiting > 0
	}, 10*time.Second, 25*time.Millisecond, "the deletion has to wait for the refresh, or the test proves nothing")

	return done
}

func requireDone(t *testing.T, done <-chan error) {
	t.Helper()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("the deletion did not finish after the refresh committed")
	}
}

func familyOf(t *testing.T, token string) string {
	t.Helper()

	var family string

	require.NoError(t, testDB.Raw("SELECT family_id::text FROM refresh_tokens WHERE token_hash = ?", refreshHash(token)).Scan(&family).Error)
	require.NotEmpty(t, family)

	return family
}

func tokensWhere(t *testing.T, column string, value string) int64 {
	t.Helper()

	var count int64

	require.NoError(t, testDB.Raw("SELECT count(*) FROM refresh_tokens WHERE "+column+" = ?::uuid", value).Scan(&count).Error)

	return count
}

// A DELETE that starts during a refresh reads the tokens from before it, so without a lock it removes the used token
// when the refresh commits and leaves the token that the refresh stored. These tests hold a refresh open to make that happen.
func TestLoggingOutDuringARefreshEndsTheWholeSession(t *testing.T) {
	guru := registerGuru(t, "Logout During Refresh")
	family := familyOf(t, guru.RefreshToken)
	finish := inFlightRefresh(t, guru.RefreshToken)

	repo := authrepository.NewAuthDB(testDB)
	done := startWhileRefreshing(t, func(ctx context.Context) error {
		return repo.DeleteRefreshToken(ctx, refreshHash(guru.RefreshToken))
	})

	finish()
	requireDone(t, done)

	require.Zero(t, tokensWhere(t, "family_id", family), "the token that the refresh stored must be gone as well")
}

func TestReuseDetectionDuringARefreshEndsTheWholeSession(t *testing.T) {
	guru := registerGuru(t, "Reuse During Refresh")

	first := refresh(t, guru.RefreshToken)
	require.Equal(t, http.StatusOK, first.Status, "body %v", first.Body)

	live := first.data("refreshToken").(string)
	family := familyOf(t, live)

	// the owner refreshes again while a thief shows the first token after the grace period
	ageUse(t, guru.RefreshToken, 2*time.Minute)

	finish := inFlightRefresh(t, live)

	repo := authrepository.NewAuthDB(testDB)

	var rotation authrepository.Rotation

	done := startWhileRefreshing(t, func(ctx context.Context) error {
		var err error

		rotation, err = repo.RotateRefreshToken(
			ctx, refreshHash(guru.RefreshToken), time.Now(), &entity.RefreshToken{},
			authrepository.RotationPolicy{SessionMax: 90 * 24 * time.Hour, ReuseGrace: time.Minute},
		)

		return err
	})

	finish()
	requireDone(t, done)

	require.True(t, rotation.Reused)
	require.Zero(t, tokensWhere(t, "family_id", family), "the token that the refresh stored must be gone as well")
}

func TestDeletingAnAccountDuringARefreshRemovesEveryToken(t *testing.T) {
	guru := registerGuru(t, "Deletion During Refresh")

	var userID string

	require.NoError(t, testDB.Raw("SELECT user_id::text FROM refresh_tokens WHERE token_hash = ?", refreshHash(guru.RefreshToken)).Scan(&userID).Error)

	finish := inFlightRefresh(t, guru.RefreshToken)

	repo := userrepository.NewUserDB(testDB)
	done := startWhileRefreshing(t, func(ctx context.Context) error {
		_, err := repo.SoftDeleteAccount(ctx, uuid.MustParse(userID), constants.RoleGuru)

		return err
	})

	finish()
	requireDone(t, done)

	require.Zero(t, tokensWhere(t, "user_id", userID), "no token of a deleted account may stay")
}

// A refresh cleans up the expired tokens of its user after it stored the next one. If that cleanup waited for a row that
// a deletion holds, the deletion could be waiting for the refresh at the same time and PostgreSQL would have to abort one
// of them. So the cleanup leaves a row that is locked and goes on.
func TestARefreshDoesNotWaitForAnExpiredTokenThatSomeoneElseHolds(t *testing.T) {
	guru := registerGuru(t, "Cleanup Lock")

	login := call(t, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
		"email": guru.Email, "password": guru.Password, "role": "GURU",
	})
	require.Equal(t, http.StatusOK, login.Status, "body %v", login.Body)

	second := login.data("refreshToken").(string)

	// the token of the first sign in is left to expire without being used
	expired := testDB.Exec("UPDATE refresh_tokens SET expires_at = now() - interval '1 day' WHERE token_hash = ?", refreshHash(guru.RefreshToken))
	require.NoError(t, expired.Error)
	require.EqualValues(t, 1, expired.RowsAffected)

	holder := testDB.Begin()
	require.NoError(t, holder.Error)

	t.Cleanup(func() { _ = holder.Rollback().Error })

	require.NoError(t, holder.Exec("SELECT id FROM refresh_tokens WHERE token_hash = ? FOR UPDATE", refreshHash(guru.RefreshToken)).Error)

	repo := authrepository.NewAuthDB(testDB)
	next := &entity.RefreshToken{ID: uuid.New(), TokenHash: refreshHash(uuid.NewString()), ExpiresAt: time.Now().Add(time.Hour)}

	var rotation authrepository.Rotation

	done := make(chan error, 1)

	go func() {
		var err error

		rotation, err = repo.RotateRefreshToken(
			context.Background(), refreshHash(second), time.Now(), next,
			authrepository.RotationPolicy{SessionMax: 90 * 24 * time.Hour, ReuseGrace: time.Minute},
		)

		done <- err
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("the refresh waited for a row that it only had to clean up")
	}

	require.NotNil(t, rotation.User, "the refresh went through")

	var left int64

	require.NoError(t, testDB.Raw("SELECT count(*) FROM refresh_tokens WHERE token_hash = ?", refreshHash(guru.RefreshToken)).Scan(&left).Error)
	require.EqualValues(t, 1, left, "the locked row was left for the next time")
}
