package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const changePasswordPath = "/api/v1/users/me/password"

func changePassword(t *testing.T, token string, current string, next string) result {
	t.Helper()

	return call(t, http.MethodPut, changePasswordPath, token, map[string]any{"currentPassword": current, "newPassword": next})
}

func loginAs(t *testing.T, email string, password string, role string) result {
	t.Helper()

	return call(t, http.MethodPost, "/api/v1/auth/login", "", map[string]any{"email": email, "password": password, "role": role})
}

func TestChangePassword(t *testing.T) {
	guru := registerGuru(t, "Sandi Guru")
	_, joinCode := classOf(t, guru)
	wali, _ := registerWali(t, joinCode, "Sandi Anak")

	next := "Next-" + suffix() + "!"

	t.Run("a refused change leaves the password and the sessions alone", func(t *testing.T) {
		changePassword(t, wali.Token, "not-the-password", next).requireError(t, http.StatusForbidden, "AUTH_PASSWORD_INCORRECT")
		changePassword(t, wali.Token, wali.Password, wali.Password).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		changePassword(t, wali.Token, wali.Password, "short").requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		changePassword(t, wali.Token, wali.Password, "Password123").requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		changePassword(t, wali.Token, wali.Password, strings.Repeat("é", 40)).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodPut, changePasswordPath, wali.Token, map[string]any{"newPassword": next}).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		changePassword(t, "", wali.Password, next).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")

		require.Equal(t, http.StatusOK, loginAs(t, wali.Email, wali.Password, "WALI").Status)

		refreshed := call(t, http.MethodPost, "/api/v1/auth/refresh-token", "", map[string]any{"refreshToken": wali.RefreshToken})
		require.Equal(t, http.StatusOK, refreshed.Status, "body %v", refreshed.Body)
	})

	t.Run("a parent changes the password and every session ends", func(t *testing.T) {
		second := loginAs(t, wali.Email, wali.Password, "WALI")
		require.Equal(t, http.StatusOK, second.Status, "body %v", second.Body)

		secondRefresh, _ := second.data("refreshToken").(string)

		res := changePassword(t, wali.Token, wali.Password, next)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, "Password changed", res.Body["message"])

		loginAs(t, wali.Email, wali.Password, "WALI").requireError(t, http.StatusUnauthorized, "AUTH_INVALID_CREDENTIALS")
		require.Equal(t, http.StatusOK, loginAs(t, wali.Email, next, "WALI").Status)

		call(t, http.MethodPost, "/api/v1/auth/refresh-token", "", map[string]any{"refreshToken": secondRefresh}).
			requireError(t, http.StatusUnauthorized, "AUTH_REFRESH_INVALID")

		// the old password is no longer the current one
		changePassword(t, wali.Token, wali.Password, "Third-"+suffix()+"!").requireError(t, http.StatusForbidden, "AUTH_PASSWORD_INCORRECT")
	})

	t.Run("a teacher changes the password too", func(t *testing.T) {
		res := changePassword(t, guru.Token, guru.Password, next)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		loginAs(t, guru.Email, guru.Password, "GURU").requireError(t, http.StatusUnauthorized, "AUTH_INVALID_CREDENTIALS")
		require.Equal(t, http.StatusOK, loginAs(t, guru.Email, next, "GURU").Status)

		call(t, http.MethodPost, "/api/v1/auth/refresh-token", "", map[string]any{"refreshToken": guru.RefreshToken}).
			requireError(t, http.StatusUnauthorized, "AUTH_REFRESH_INVALID")
	})
}

// A password change and an account deletion of the same account end its sessions and touch the user row, and a sign in reads
// that row. They have to take their locks in the same order, which is the user row first, or PostgreSQL aborts one of them
// with a deadlock and the caller gets a 500.
func TestChangingThePasswordWhileDeletingTheAccountNeverDeadlocks(t *testing.T) {
	guru := registerGuru(t, "Sandi Balapan")
	_, joinCode := classOf(t, guru)

	for round := range 6 {
		wali, _ := registerWali(t, joinCode, fmt.Sprintf("Sandi Anak %d", round))

		var (
			wg       sync.WaitGroup
			changed  result
			removed  result
			signedIn result
		)

		wg.Add(3)

		// a sign in reads the user row under a share lock, so it joins the race as well
		go func() {
			defer wg.Done()

			signedIn = loginAs(t, wali.Email, wali.Password, "WALI")
		}()

		go func() {
			defer wg.Done()

			changed = changePassword(t, wali.Token, wali.Password, "Race-"+suffix()+"!")
		}()

		go func() {
			defer wg.Done()

			removed = deleteAccount(t, wali.Token, wali.Password)
		}()

		wg.Wait()

		// the change can commit first, and then the deletion with the old password is refused because that password is gone
		require.Contains(t, []int{http.StatusOK, http.StatusForbidden}, removed.Status, "round %d, body %v", round, removed.Body)

		if removed.Status == http.StatusForbidden {
			removed.requireError(t, http.StatusForbidden, "AUTH_PASSWORD_INCORRECT")
			require.Equal(t, http.StatusOK, changed.Status, "round %d, the deletion was refused for the old password, so the change must have gone through, body %v", round, changed.Body)
		}

		require.NotEqual(t, http.StatusInternalServerError, changed.Status, "round %d, body %v", round, changed.Body)
		require.NotEqual(t, http.StatusInternalServerError, signedIn.Status, "round %d, body %v", round, signedIn.Body)
	}
}

// A sign in checks the password and stores its session in a transaction of its own. A password change that has updated the
// row and not yet committed makes the sign in wait, and it is refused when the change commits, so no session is left with
// the old password.
func TestASignInThatWaitsForAPasswordChangeIsRefused(t *testing.T) {
	guru := registerGuru(t, "Sandi Berubah")

	var userID string

	require.NoError(t, testDB.Raw("SELECT id::text FROM users WHERE email = ?", guru.Email).Scan(&userID).Error)

	change := testDB.Begin()
	require.NoError(t, change.Error)

	t.Cleanup(func() { _ = change.Rollback().Error })

	require.NoError(t, change.Exec("UPDATE users SET password_hash = 'changed' WHERE id = ?::uuid", userID).Error)

	before := tokensWhere(t, "user_id", userID)
	done := make(chan result, 1)

	go func() { done <- loginAs(t, guru.Email, guru.Password, "GURU") }()

	require.Eventually(t, func() bool {
		var waiting int64

		err := testDB.Raw(
			"SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND pid <> pg_backend_pid() " +
				"AND query LIKE '%password_hash FROM users%FOR SHARE%'",
		).Scan(&waiting).Error

		return err == nil && waiting > 0
	}, 10*time.Second, 25*time.Millisecond, "the sign in has to wait for the change, or the test proves nothing")

	require.NoError(t, change.Commit().Error)

	select {
	case res := <-done:
		res.requireError(t, http.StatusUnauthorized, "AUTH_INVALID_CREDENTIALS")
	case <-time.After(10 * time.Second):
		t.Fatal("the sign in did not finish after the change committed")
	}

	require.Equal(t, before, tokensWhere(t, "user_id", userID), "the sign in must not leave a session behind")
}
