package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authrepository "github.com/SyafaHadyan/kibooz-backend/internal/app/auth/repository"
)

// The queries that find an account that waited for its code, and the cap of codes, run against the real database.
func TestPendingAccountQueriesAndTheCodeCap(t *testing.T) {
	ctx := context.Background()
	repo := authrepository.NewAuthDB(testDB)

	guru := registerGuru(t, "Menunggu Kode")

	var id, nip string

	require.NoError(t, testDB.Raw("SELECT id::text FROM users WHERE email = ?", guru.Email).Scan(&id).Error)
	require.NoError(t, testDB.Raw("SELECT nip FROM gurus WHERE user_id = ?::uuid", id).Scan(&nip).Error)
	require.NotEmpty(t, nip)

	// the account waits for its code, since a day and a half ago
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
		require.NotEmpty(t, byEmail[0].PasswordHash)

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

		// a rule without a cap sends on
		_, err = repo.StoreVerification(ctx, userID, "hash-5", now.Add(6*time.Second), authrepository.SendRule{TTL: time.Hour, PerHour: 100})
		require.NoError(t, err)
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
