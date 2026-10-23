package e2e

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	authrepository "github.com/SyafaHadyan/kibooz-backend/internal/app/auth/repository"
	userrepository "github.com/SyafaHadyan/kibooz-backend/internal/app/user/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
)

// inFlightRefresh opens the transaction of a refresh and stops before it commits. It has marked the token as used, and
// the function it returns stores the next token and commits, which is what the rest of that request does.
func inFlightRefresh(t *testing.T, token string) (finish func()) {
	t.Helper()

	return inFlightRefreshOfHash(t, refreshHash(token))
}

// inFlightRefreshOfHash is inFlightRefresh for a token that is known by its hash
func inFlightRefreshOfHash(t *testing.T, hash string) (finish func()) {
	t.Helper()

	finish, err := tryInFlightRefreshOfHash(t, hash, 10*time.Second)
	require.NoError(t, err)

	return finish
}

// tryInFlightRefreshOfHash gives up with the error of the database when the token is locked for longer than wait, so a
// refresh that has to wait for a row fails the test or lets it stop, and never hangs it
func tryInFlightRefreshOfHash(t *testing.T, hash string, wait time.Duration) (finish func(), err error) {
	t.Helper()

	tx := testDB.Begin()
	require.NoError(t, tx.Error)

	t.Cleanup(func() { _ = tx.Rollback().Error })

	require.NoError(t, tx.Exec("SET LOCAL lock_timeout = '"+strconv.FormatInt(wait.Milliseconds(), 10)+"ms'").Error)

	used := tx.Exec("UPDATE refresh_tokens SET used_at = now() WHERE token_hash = ? AND used_at IS NULL", hash)
	if used.Error != nil {
		return nil, used.Error
	}

	require.EqualValues(t, 1, used.RowsAffected, "the token has to be one that was not used")

	return func() {
		t.Helper()

		next := tx.Exec(
			"INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at, family_id, session_started_at) "+
				"SELECT gen_random_uuid(), user_id, ?, expires_at, family_id, session_started_at FROM refresh_tokens WHERE token_hash = ?",
			refreshHash(uuid.NewString()), hash,
		)
		require.NoError(t, next.Error)
		require.EqualValues(t, 1, next.RowsAffected)
		require.NoError(t, tx.Commit().Error)
	}, nil
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
			authrepository.RotationPolicy{SessionMax: 90 * 24 * time.Hour, ReuseLeeway: time.Minute},
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

	successor := registerGuru(t, "Successor During Refresh")
	finish := inFlightRefresh(t, guru.RefreshToken)

	repo := userrepository.NewUserDB(testDB)
	done := startWhileRefreshing(t, func(ctx context.Context) error {
		_, err := repo.SoftDeleteAccount(ctx, uuid.MustParse(userID), constants.RoleGuru, passwordHashOf(t, userID), successor.Email, noFiles)

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
			authrepository.RotationPolicy{SessionMax: 90 * 24 * time.Hour, ReuseLeeway: time.Minute},
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

// The lock of a deletion holds the tokens that were live when it started, and its DELETE reads the rows from the moment it
// starts. A refresh of the token that an earlier refresh has just stored begins in between and is not held by the lock, so
// the DELETE removes the token it used and leaves the one it stored. A stand-in holds the oldest token, so the DELETE stops
// there before it reaches the newer ones, and the second refresh runs while it waits.
func TestARefreshThatBeginsWhileASessionIsBeingEndedLeavesNoTokenBehind(t *testing.T) {
	guru := registerGuru(t, "Late Refresh")
	family := familyOf(t, guru.RefreshToken)

	first := refresh(t, guru.RefreshToken)
	require.Equal(t, http.StatusOK, first.Status, "body %v", first.Body)

	live := first.data("refreshToken").(string)

	holder := testDB.Begin()
	require.NoError(t, holder.Error)

	t.Cleanup(func() { _ = holder.Rollback().Error })

	require.NoError(t, holder.Exec("SELECT id FROM refresh_tokens WHERE token_hash = ? FOR UPDATE", refreshHash(guru.RefreshToken)).Error)

	finish := inFlightRefresh(t, live)

	repo := authrepository.NewAuthDB(testDB)
	done := startWhileRefreshing(t, func(ctx context.Context) error {
		return repo.DeleteRefreshToken(ctx, refreshHash(live))
	})

	// the refresh commits, so the lock lets go and the DELETE starts, and it waits for the stand-in at once
	finish()

	require.Eventually(t, func() bool {
		var waiting int64

		err := testDB.Raw(
			"SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND pid <> pg_backend_pid() " +
				"AND query LIKE 'DELETE FROM refresh_tokens WHERE family_id%'",
		).Scan(&waiting).Error

		return err == nil && waiting > 0
	}, 10*time.Second, 25*time.Millisecond, "the DELETE has to be waiting, or the test proves nothing")

	// the token that the refresh stored is refreshed again, which the lock of the deletion did not cover
	var newest string

	require.NoError(t, testDB.Raw(
		"SELECT token_hash FROM refresh_tokens WHERE family_id = ?::uuid AND used_at IS NULL", family,
	).Scan(&newest).Error)
	require.NotEmpty(t, newest)

	second, err := tryInFlightRefreshOfHash(t, newest, 3*time.Second)
	if err != nil {
		var pgErr *pgconn.PgError

		// any other error is a failure of the test and not a reason to stop it
		require.ErrorAs(t, err, &pgErr)
		require.Equal(t, "55P03", pgErr.Code, "only a lock timeout means that the DELETE holds the token: %v", err)

		// The DELETE visits the rows in the order they are stored, and it normally reaches the held token before the new
		// one. When it reached the new one first it holds it, and there is no moment left in which the lock does not cover
		// it, so this run has nothing to prove.
		require.NoError(t, holder.Commit().Error)
		requireDone(t, done)
		t.Skipf("the DELETE reached the new token before the held one (%v)", err)
	}

	second()

	require.NoError(t, holder.Commit().Error)
	requireDone(t, done)

	require.Zero(t, tokensWhere(t, "family_id", family), "the token of the second refresh must be gone as well")
}

// standInSignIn is a sign in that has checked the old password, read the user row under a share lock and stored its token,
// and has not committed yet. The function it returns commits it.
func standInSignIn(t *testing.T, userID string) (commit func()) {
	t.Helper()

	tx := testDB.Begin()
	require.NoError(t, tx.Error)

	t.Cleanup(func() { _ = tx.Rollback().Error })

	require.NoError(t, tx.Exec("SELECT password_hash FROM users WHERE id = ?::uuid FOR SHARE", userID).Error)

	stored := tx.Exec(
		"INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at, family_id, session_started_at) "+
			"VALUES (gen_random_uuid(), ?::uuid, ?, now() + interval '1 day', gen_random_uuid(), now())",
		userID, refreshHash(uuid.NewString()),
	)
	require.NoError(t, stored.Error)
	require.EqualValues(t, 1, stored.RowsAffected)

	return func() {
		t.Helper()

		require.NoError(t, tx.Commit().Error)
	}
}

// waitForLockWaiter returns once a statement that matches like is waiting for a lock
func waitForLockWaiter(t *testing.T, like string, why string) {
	t.Helper()

	require.Eventually(t, func() bool {
		var waiting int64

		err := testDB.Raw(
			"SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND pid <> pg_backend_pid() AND query LIKE ?", like,
		).Scan(&waiting).Error

		return err == nil && waiting > 0
	}, 10*time.Second, 25*time.Millisecond, why)
}

func userIDOfEmail(t *testing.T, email string) string {
	t.Helper()

	var userID string

	require.NoError(t, testDB.Raw("SELECT id::text FROM users WHERE email = ?", email).Scan(&userID).Error)
	require.NotEmpty(t, userID)

	return userID
}

// A sign in that has stored its session but not committed is still under way when the password is changed. The change
// takes the user row first, so it waits for the sign in, and when it has the row it deletes the session that was stored.
// Without that, the change ended the sessions before the sign in committed and the session lived on.
func TestAPasswordChangeEndsTheSessionOfASignInThatWasUnderWay(t *testing.T) {
	guru := registerGuru(t, "Sandi Menunggu")
	userID := userIDOfEmail(t, guru.Email)

	var hash string

	require.NoError(t, testDB.Raw("SELECT password_hash FROM users WHERE id = ?::uuid", userID).Scan(&hash).Error)

	commit := standInSignIn(t, userID)

	var changed bool

	repo := userrepository.NewUserDB(testDB)
	done := make(chan error, 1)

	go func() {
		var err error

		changed, err = repo.UpdatePassword(context.Background(), uuid.MustParse(userID), hash, "changed-hash")
		done <- err
	}()

	waitForLockWaiter(t, "%FROM users WHERE id%FOR NO KEY UPDATE%", "the change has to wait for the sign in, or the test proves nothing")

	commit()
	requireDone(t, done)

	require.True(t, changed)
	require.Zero(t, tokensWhere(t, "user_id", userID), "the session of the sign in must be gone as well")
}

// The same for an account deletion, which takes its locks in the same order
func TestDeletingAnAccountEndsTheSessionOfASignInThatWasUnderWay(t *testing.T) {
	guru := registerGuru(t, "Hapus Menunggu")
	userID := userIDOfEmail(t, guru.Email)

	commit := standInSignIn(t, userID)

	repo := userrepository.NewUserDB(testDB)
	done := make(chan error, 1)

	go func() {
		_, err := repo.SoftDeleteAccount(context.Background(), uuid.MustParse(userID), constants.RoleGuru, passwordHashOf(t, userID), "", noFiles)
		done <- err
	}()

	waitForLockWaiter(t, "%FROM users WHERE id%FOR NO KEY UPDATE%", "the deletion has to wait for the sign in, or the test proves nothing")

	commit()
	requireDone(t, done)

	require.Zero(t, tokensWhere(t, "user_id", userID), "the session of the sign in must be gone as well")
}

// impatient returns a handle on one connection that gives up on a lock after 300 ms, which is how a deletion fails when
// another transaction holds the tokens it has to lock
func impatient(t *testing.T) *gorm.DB {
	t.Helper()

	pool, err := testDB.DB()
	require.NoError(t, err)

	conn, err := pool.Conn(context.Background())
	require.NoError(t, err)

	_, err = conn.ExecContext(context.Background(), "SET lock_timeout = '300ms'")
	require.NoError(t, err)

	// the connection goes back to the pool, so it must not keep the setting for the tests that follow
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), "RESET lock_timeout")
		_ = conn.Close()
	})

	db, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{})
	require.NoError(t, err)

	return db
}

func TestEndingASessionFailsWhenTheTokensCannotBeLockedAndChangesNothing(t *testing.T) {
	guru := registerGuru(t, "Lock Timeout")

	var userID string

	require.NoError(t, testDB.Raw("SELECT user_id::text FROM refresh_tokens WHERE token_hash = ?", refreshHash(guru.RefreshToken)).Scan(&userID).Error)

	holder := testDB.Begin()
	require.NoError(t, holder.Error)

	t.Cleanup(func() { _ = holder.Rollback().Error })

	require.NoError(t, holder.Exec("SELECT id FROM refresh_tokens WHERE token_hash = ? FOR UPDATE", refreshHash(guru.RefreshToken)).Error)

	db := impatient(t)

	t.Run("logging out", func(t *testing.T) {
		require.Error(t, authrepository.NewAuthDB(db).DeleteRefreshToken(context.Background(), refreshHash(guru.RefreshToken)))
	})

	t.Run("deleting the account", func(t *testing.T) {
		_, err := userrepository.NewUserDB(db).SoftDeleteAccount(
			context.Background(), uuid.MustParse(userID), constants.RoleGuru, passwordHashOf(t, userID), registerGuru(t, "Successor Lock Timeout").Email, noFiles,
		)
		require.Error(t, err)

		var active int64

		require.NoError(t, testDB.Raw("SELECT count(*) FROM users WHERE id = ?::uuid AND deleted_at IS NULL", userID).Scan(&active).Error)
		require.EqualValues(t, 1, active, "the account is still there because the whole deletion was rolled back")
		require.EqualValues(t, 1, tokensWhere(t, "user_id", userID))
	})
}

// requireLockTimeout fails the test unless err is the error of PostgreSQL for a lock that was not granted in time
func requireLockTimeout(t *testing.T, err error) {
	t.Helper()

	var pgErr *pgconn.PgError

	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "55P03", pgErr.Code, "the change has to fail on the lock of the user row: %v", err)
}

// A change that cannot take the user row fails as a whole and changes nothing, like one that cannot take the tokens
func TestAChangeFailsWhenTheUserRowCannotBeLockedAndChangesNothing(t *testing.T) {
	guru := registerGuru(t, "User Lock Timeout")
	userID := userIDOfEmail(t, guru.Email)

	var hash string

	require.NoError(t, testDB.Raw("SELECT password_hash FROM users WHERE id = ?::uuid", userID).Scan(&hash).Error)

	// a sign in under way holds the row, and the stand-in is never committed
	standInSignIn(t, userID)

	db := impatient(t)

	t.Run("changing the password", func(t *testing.T) {
		_, err := userrepository.NewUserDB(db).UpdatePassword(context.Background(), uuid.MustParse(userID), hash, "changed-hash")
		requireLockTimeout(t, err)

		var current string

		require.NoError(t, testDB.Raw("SELECT password_hash FROM users WHERE id = ?::uuid", userID).Scan(&current).Error)
		require.Equal(t, hash, current)
		require.EqualValues(t, 1, tokensWhere(t, "user_id", userID), "only the token of the registration is there")
	})

	t.Run("deleting the account", func(t *testing.T) {
		_, err := userrepository.NewUserDB(db).SoftDeleteAccount(context.Background(), uuid.MustParse(userID), constants.RoleGuru, passwordHashOf(t, userID), "", noFiles)
		requireLockTimeout(t, err)

		var active int64

		require.NoError(t, testDB.Raw("SELECT count(*) FROM users WHERE id = ?::uuid AND deleted_at IS NULL", userID).Scan(&active).Error)
		require.EqualValues(t, 1, active)
	})
}

func TestLoggingOutWithATokenNobodyKnowsChangesNothing(t *testing.T) {
	guru := registerGuru(t, "Unknown Logout")
	family := familyOf(t, guru.RefreshToken)

	require.NoError(t, authrepository.NewAuthDB(testDB).DeleteRefreshToken(context.Background(), refreshHash(uuid.NewString())))
	require.EqualValues(t, 1, tokensWhere(t, "family_id", family))
}

// passwordHashOf is the stored hash of an account, which a deletion has to be bound to
func passwordHashOf(t *testing.T, userID string) string {
	t.Helper()

	var hash string

	require.NoError(t, testDB.Raw("SELECT password_hash FROM users WHERE id = ?", userID).Scan(&hash).Error)

	return hash
}

// A password that was checked before the transaction must still be the password inside it, so a change that committed in
// between cannot confirm the deletion.
func TestDeleteAccountRefusesAHashThatWasReplaced(t *testing.T) {
	guru := registerGuru(t, "Stale Hash Class")

	var userID string

	require.NoError(t, testDB.Raw("SELECT id::text FROM users WHERE email = ?", guru.Email).Scan(&userID).Error)

	checked := passwordHashOf(t, userID)

	require.NoError(t, testDB.Exec("UPDATE users SET password_hash = ? WHERE id = ?", checked+"x", userID).Error)

	_, err := userrepository.NewUserDB(testDB).SoftDeleteAccount(
		context.Background(), uuid.MustParse(userID), constants.RoleGuru, checked, "", noFiles,
	)
	require.ErrorIs(t, err, apperror.ErrPasswordIncorrect)

	var deleted int64

	require.NoError(t, testDB.Raw("SELECT COUNT(*) FROM users WHERE id = ? AND deleted_at IS NOT NULL", userID).Scan(&deleted).Error)
	require.Zero(t, deleted)
}
