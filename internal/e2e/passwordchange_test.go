package e2e

import (
	"net/http"
	"strings"
	"testing"

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
