package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authrepository "github.com/kiboozapp/kibooz-backend/internal/app/auth/repository"
	userrepository "github.com/kiboozapp/kibooz-backend/internal/app/user/repository"
	"github.com/kiboozapp/kibooz-backend/internal/constants"
)

// The queries that find an account that waited for its code, and the cap of codes, run against the real database.
func TestPendingAccountQueriesAndTheCodeCap(t *testing.T) {
	ctx := context.Background()
	guru := registerGuru(t, "Menunggu Kode")
	repo := authrepository.NewAuthDB(testDB)

	var id, nip string

	require.NoError(t, testDB.Raw("SELECT id::text FROM users WHERE email = ?", guru.Email).Scan(&id).Error)
	require.NoError(t, testDB.Raw("SELECT nip FROM gurus WHERE user_id = ?::uuid", id).Scan(&nip).Error)
	require.NotEmpty(t, nip)

	// the account waits for its code since a day and a half ago, and never signed in
	require.NoError(t, testDB.Exec("DELETE FROM refresh_tokens WHERE user_id = ?::uuid", id).Error)
	require.NoError(t, testDB.Exec(
		"UPDATE users SET email_verification_pending = TRUE, created_at = now() - interval '36 hours' WHERE id = ?::uuid", id,
	).Error)

	t.Cleanup(func() {
		_ = testDB.Exec("DELETE FROM email_verifications WHERE user_id = ?::uuid", id).Error
		_ = testDB.Exec("UPDATE users SET email_verification_pending = FALSE WHERE id = ?::uuid", id).Error
	})

	day := time.Now().Add(-24 * time.Hour)

	t.Run("an account that waited more than a day is found by its email and by its NIP", func(t *testing.T) {
		byEmail, err := repo.StalePendingAccounts(ctx, guru.Email, "", day)
		require.NoError(t, err)
		require.Len(t, byEmail, 1)
		require.Equal(t, id, byEmail[0].ID.String())
		require.Equal(t, "GURU", string(byEmail[0].Role))

		byNIP, err := repo.StalePendingAccounts(ctx, "nobody@example.com", nip, day)
		require.NoError(t, err)
		require.Len(t, byNIP, 1)
	})

	t.Run("an empty NIP matches nothing", func(t *testing.T) {
		found, err := repo.StalePendingAccounts(ctx, "nobody@example.com", "", day)
		require.NoError(t, err)
		require.Empty(t, found)
	})

	t.Run("an account that waited less than the time is left alone", func(t *testing.T) {
		found, err := repo.StalePendingAccounts(ctx, guru.Email, nip, time.Now().Add(-48*time.Hour))
		require.NoError(t, err)
		require.Empty(t, found)
	})

	t.Run("an account that does not wait for a code is left alone", func(t *testing.T) {
		require.NoError(t, testDB.Exec("UPDATE users SET email_verification_pending = FALSE WHERE id = ?::uuid", id).Error)

		found, err := repo.StalePendingAccounts(ctx, guru.Email, nip, day)
		require.NoError(t, err)
		require.Empty(t, found)

		require.NoError(t, testDB.Exec("UPDATE users SET email_verification_pending = TRUE WHERE id = ?::uuid", id).Error)
	})

	t.Run("the older accounts come oldest first and the limit holds", func(t *testing.T) {
		found, err := repo.OlderPendingAccounts(ctx, day, 1)
		require.NoError(t, err)
		require.Len(t, found, 1)
	})

	t.Run("the cap of codes counts every code and a given back one is free again", func(t *testing.T) {
		userID := found(t, repo, guru.Email, nip, day)
		rule := authrepository.SendRule{TTL: time.Hour, Cooldown: 0, PerHour: 100, MaxTotal: 2}
		now := time.Now()

		wait, err := repo.StoreVerification(ctx, userID, "hash-1", now, rule)
		require.NoError(t, err)
		require.Zero(t, wait)

		wait, err = repo.StoreVerification(ctx, userID, "hash-2", now.Add(time.Second), rule)
		require.NoError(t, err)
		require.Zero(t, wait)

		_, err = repo.StoreVerification(ctx, userID, "hash-3", now.Add(2*time.Second), rule)
		require.ErrorIs(t, err, authrepository.ErrCodeLimit)

		// a code that was not mailed is given back, and then one more fits
		require.NoError(t, repo.ReleaseVerification(ctx, userID, "hash-2", now.Add(3*time.Second), 0))

		_, err = repo.StoreVerification(ctx, userID, "hash-3", now.Add(4*time.Second), rule)
		require.NoError(t, err)

		_, err = repo.StoreVerification(ctx, userID, "hash-4", now.Add(5*time.Second), rule)
		require.ErrorIs(t, err, authrepository.ErrCodeLimit)

		// an account that signed in while the verification was off has no cap, and the other limits stay
		require.NoError(t, testDB.Exec("UPDATE users SET signed_in_unverified_at = now() WHERE id = ?", userID).Error)

		wait, err = repo.StoreVerification(ctx, userID, "hash-marked", now.Add(5*time.Second), rule)
		require.NoError(t, err)
		require.Zero(t, wait)

		limited := rule
		limited.Cooldown = time.Minute

		wait, err = repo.StoreVerification(ctx, userID, "hash-cooldown", now.Add(6*time.Second), limited)
		require.NoError(t, err)
		require.Positive(t, wait)

		require.NoError(t, testDB.Exec("UPDATE users SET signed_in_unverified_at = NULL WHERE id = ?", userID).Error)

		// a rule without a cap sends on
		_, err = repo.StoreVerification(ctx, userID, "hash-5", now.Add(6*time.Second), authrepository.SendRule{TTL: time.Hour, PerHour: 100})
		require.NoError(t, err)
	})
}

// ageAccount makes the account of the email as old as the interval says and returns its id
func ageAccount(t *testing.T, email string, interval string) uuid.UUID {
	t.Helper()

	var id string

	require.NoError(t, testDB.Raw(
		"UPDATE users SET created_at = now() - ?::interval WHERE email = ? AND deleted_at IS NULL RETURNING id::text", interval, email,
	).Scan(&id).Error)

	return uuid.MustParse(id)
}

func deletedAccounts(t *testing.T, email string) (active int64, deleted int64) {
	t.Helper()

	require.NoError(t, testDB.Raw("SELECT count(*) FROM users WHERE email = ? AND deleted_at IS NULL", email).Scan(&active).Error)
	require.NoError(t, testDB.Raw("SELECT count(*) FROM users WHERE email = ? AND deleted_at IS NOT NULL", email).Scan(&deleted).Error)

	return active, deleted
}

func TestAWaitingAccountIsReplacedOrRemovedUnlessItSignedIn(t *testing.T) {
	ctx := context.Background()
	verifying, _ := startVerifying(t)
	off := app(t)
	repo := authrepository.NewAuthDB(testDB)
	users := userrepository.NewUserDB(testDB)

	t.Run("a registration replaces the account that waited more than a day", func(t *testing.T) {
		email, _ := registerWaitingGuru(t, verifying)
		ageAccount(t, email, "36 hours")

		res := callApp(t, verifying, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
			"email": email, "password": testPassword, "fullName": "Second Teacher", "role": "GURU", "class": map[string]any{"name": "Again"},
		})
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

		active, deleted := deletedAccounts(t, email)
		require.Equal(t, int64(1), active)
		require.Equal(t, int64(1), deleted)
	})

	t.Run("an account that never signed in is removed after the time", func(t *testing.T) {
		email, _ := registerWaitingGuru(t, verifying)
		id := ageAccount(t, email, "8 days")

		_, err := users.SoftDeletePendingAccount(ctx, id, constants.RoleGuru, time.Now().Add(-9*24*time.Hour))
		require.ErrorIs(t, err, userrepository.ErrNotPending, "it is newer than the limit")

		_, err = users.SoftDeletePendingAccount(ctx, id, constants.RoleGuru, time.Now().Add(-7*24*time.Hour))
		require.NoError(t, err)

		active, deleted := deletedAccounts(t, email)
		require.Zero(t, active)
		require.Equal(t, int64(1), deleted)
	})

	t.Run("a verified child of the replaced parent does not block the parent, and an account that is gone gets no code", func(t *testing.T) {
		email, _ := registerWaitingGuru(t, verifying)
		id := ageAccount(t, email, "36 hours")

		_, err := users.SoftDeletePendingAccount(ctx, id, constants.RoleGuru, time.Now())
		require.NoError(t, err)

		_, err = repo.StoreVerification(ctx, id, "hash", time.Now(), authrepository.SendRule{TTL: time.Hour, PerHour: 5})
		require.ErrorIs(t, err, authrepository.ErrNotWaiting)

		_, joinCode := classOf(t, registerGuru(t, "Kelas Anak"))
		parent, _ := registerWali(t, joinCode, "Anak Menunggu")

		require.NoError(t, repo.ClassTakesChildren(ctx, joinCode))

		var parentID, nisn string

		require.NoError(t, testDB.Raw("SELECT id::text FROM users WHERE email = ?", parent.Email).Scan(&parentID).Error)
		require.NoError(t, testDB.Raw(
			`UPDATE students SET nisn_verified_at = now() WHERE wali_id = (SELECT id FROM walis WHERE user_id = ?::uuid) RETURNING nisn`,
			parentID,
		).Scan(&nisn).Error)

		require.Error(t, repo.RegistrationTaken(ctx, "", "", nisn))
		require.NoError(t, repo.RegistrationTaken(ctx, parent.Email, "", nisn, uuid.MustParse(parentID)))
	})

	t.Run("an account that signed in while the verification was off is kept", func(t *testing.T) {
		email, _ := registerWaitingGuru(t, verifying)
		require.Equal(t, http.StatusOK, login(t, off, email, testPassword).Status)

		id := ageAccount(t, email, "8 days")

		// the mark stays after the session is gone
		require.NoError(t, testDB.Exec("DELETE FROM refresh_tokens WHERE user_id = ?", id).Error)

		before := time.Now().Add(-7 * 24 * time.Hour)

		_, err := users.SoftDeletePendingAccount(ctx, id, constants.RoleGuru, before)
		require.ErrorIs(t, err, userrepository.ErrNotPending)

		older, err := repo.OlderPendingAccounts(ctx, before, 1000)
		require.NoError(t, err)

		for _, account := range older {
			require.NotEqual(t, id, account.ID)
		}

		stale, err := repo.StalePendingAccounts(ctx, email, "", before)
		require.NoError(t, err)
		require.Empty(t, stale)

		res := callApp(t, verifying, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
			"email": email, "password": testPassword, "fullName": "Second Teacher", "role": "GURU", "class": map[string]any{"name": "Again"},
		})
		res.requireError(t, http.StatusConflict, "EMAIL_ALREADY_REGISTERED")
	})
}

// found returns the id of the one account that waited since before the time and uses the email or the NIP
func found(t *testing.T, repo authrepository.AuthDBItf, email string, nip string, before time.Time) uuid.UUID {
	t.Helper()

	accounts, err := repo.StalePendingAccounts(context.Background(), email, nip, before)
	require.NoError(t, err)
	require.Len(t, accounts, 1)

	return accounts[0].ID
}

// The schedule of the mail retries is in SQL, so it runs against the real database.
func TestMailRetriesFollowTheScheduleAndAreClaimedOnce(t *testing.T) {
	ctx := context.Background()
	repo := authrepository.NewAuthDB(testDB)
	guru := registerGuru(t, "Kode Gagal")

	var id string

	require.NoError(t, testDB.Raw("SELECT id::text FROM users WHERE email = ?", guru.Email).Scan(&id).Error)
	require.NoError(t, testDB.Exec("UPDATE users SET email_verification_pending = TRUE WHERE id = ?::uuid", id).Error)

	userID, err := uuid.Parse(id)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = testDB.Exec("DELETE FROM email_verifications WHERE user_id = ?::uuid", id).Error
		_ = testDB.Exec("UPDATE users SET email_verification_pending = FALSE WHERE id = ?::uuid", id).Error
	})

	now := time.Now().Truncate(time.Second)
	rule := authrepository.SendRule{TTL: time.Hour, Cooldown: 0, PerHour: 100}

	_, err = repo.StoreVerification(ctx, userID, "code-1", now, rule)
	require.NoError(t, err)

	retryAfter := func() time.Duration {
		var seconds float64

		require.NoError(t, testDB.Raw(
			"SELECT extract(epoch FROM retry_at - ?::timestamptz) FROM email_verifications WHERE user_id = ?::uuid", now, id,
		).Scan(&seconds).Error)

		return time.Duration(seconds * float64(time.Second))
	}

	t.Run("each failure waits longer, 1, 5 and 15 minutes and then an hour, and after six tries there is no more", func(t *testing.T) {
		for _, want := range []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, time.Hour, time.Hour} {
			require.NoError(t, repo.MarkMailFailed(ctx, userID, "code-1", now))
			require.Equal(t, want, retryAfter())
		}

		require.NoError(t, repo.MarkMailFailed(ctx, userID, "code-1", now))

		var due int64

		require.NoError(t, testDB.Raw("SELECT count(*) FROM email_verifications WHERE user_id = ?::uuid AND retry_at IS NOT NULL", id).Scan(&due).Error)
		require.Zero(t, due)

		retry := rule
		retry.OnlyRetry = true

		_, err := repo.StoreVerification(ctx, userID, "code-retry", now.Add(time.Second), retry)
		require.ErrorIs(t, err, authrepository.ErrNoRetry)
	})

	t.Run("a failure of another code is not noted", func(t *testing.T) {
		require.NoError(t, repo.ClearMailRetry(ctx, userID))
		require.NoError(t, repo.MarkMailFailed(ctx, userID, "an-older-code", now))

		var due int64

		require.NoError(t, testDB.Raw("SELECT count(*) FROM email_verifications WHERE user_id = ?::uuid AND retry_at IS NOT NULL", id).Scan(&due).Error)
		require.Zero(t, due)
	})

	t.Run("a mail that is due is claimed once, and not before its time", func(t *testing.T) {
		require.NoError(t, repo.MarkMailFailed(ctx, userID, "code-1", now))

		early, err := repo.ClaimMailRetries(ctx, now.Add(30*time.Second), 5*time.Minute, 10)
		require.NoError(t, err)
		require.NotContains(t, early, userID)

		claimed, err := repo.ClaimMailRetries(ctx, now.Add(2*time.Minute), 5*time.Minute, 10)
		require.NoError(t, err)
		require.Contains(t, claimed, userID)

		again, err := repo.ClaimMailRetries(ctx, now.Add(2*time.Minute), 5*time.Minute, 10)
		require.NoError(t, err)
		require.NotContains(t, again, userID, "the lease keeps a second copy away")

		later, err := repo.ClaimMailRetries(ctx, now.Add(8*time.Minute), 5*time.Minute, 10)
		require.NoError(t, err)
		require.Contains(t, later, userID, "a copy that died leaves the retry for after the lease")
	})

	t.Run("an account that is verified is never claimed and clearing stops the retries", func(t *testing.T) {
		require.NoError(t, testDB.Exec("UPDATE users SET email_verification_pending = FALSE WHERE id = ?::uuid", id).Error)

		claimed, err := repo.ClaimMailRetries(ctx, now.Add(48*time.Hour), time.Minute, 10)
		require.NoError(t, err)
		require.NotContains(t, claimed, userID)

		require.NoError(t, repo.ClearMailRetry(ctx, userID))

		var failures int64

		require.NoError(t, testDB.Raw("SELECT mail_failures FROM email_verifications WHERE user_id = ?::uuid", id).Scan(&failures).Error)
		require.Zero(t, failures)
	})
}
