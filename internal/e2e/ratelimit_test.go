package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/bootstrap"
)

// TestRateLimitsFollowUsersAndAccounts starts a second app with tiny limits. The requests of the whole test come from
// one address, so every answer that differs between the people in it proves that the address is not what is counted.
func TestRateLimitsFollowUsersAndAccounts(t *testing.T) {
	app(t) // makes sure the shared environment defaults are set

	t.Setenv("USER_LIMITER_MAX", "8")
	t.Setenv("AUTH_LIMITER_MAX", "3")

	started, err := bootstrap.Start("e2e-limits")
	require.NoError(t, err)

	t.Cleanup(started.Close)

	limited := started.App.Fiber

	register := func(body map[string]any) result {
		t.Helper()

		res := callApp(t, limited, http.MethodPost, "/api/v1/auth/register", "", body)
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

		return res
	}

	guruEmail := fmt.Sprintf("guru.%s@example.com", suffix())
	waliEmail := fmt.Sprintf("wali.%s@example.com", suffix())

	guru := register(map[string]any{
		"email": guruEmail, "password": testPassword, "fullName": "Limited Teacher",
		"role": "GURU", "class": map[string]any{"name": "Limits"},
	})
	guruToken := guru.data("token").(string)

	dashboard := callApp(t, limited, http.MethodGet, "/api/v1/guru/dashboard", guruToken, nil)
	require.Equal(t, http.StatusOK, dashboard.Status, "body %v", dashboard.Body)

	wali := register(map[string]any{
		"email": waliEmail, "password": testPassword, "fullName": "Limited Parent",
		"role": "WALI", "classCode": dashboard.data("classOverview", "joinCode"),
		"student": map[string]any{"nisn": nisn(), "fullName": "Limited Child"},
	})
	waliToken := wali.data("token").(string)

	t.Run("a busy user is limited and a quiet user on the same address is not", func(t *testing.T) {
		for i := range 8 {
			res := callApp(t, limited, http.MethodGet, "/api/v1/wali/dashboard", waliToken, nil)
			require.Equal(t, http.StatusOK, res.Status, "request %d body %v", i+1, res.Body)
		}

		callApp(t, limited, http.MethodGet, "/api/v1/wali/dashboard", waliToken, nil).requireError(t, http.StatusTooManyRequests, "RATE_LIMITED")

		quiet := callApp(t, limited, http.MethodGet, "/api/v1/guru/dashboard", guruToken, nil)
		require.Equal(t, http.StatusOK, quiet.Status, "body %v", quiet.Body)
	})

	t.Run("requests without a valid token are not counted against anyone", func(t *testing.T) {
		for range 12 {
			res := callApp(t, limited, http.MethodGet, "/api/v1/guru/dashboard", "", nil)
			require.Equal(t, http.StatusUnauthorized, res.Status, "body %v", res.Body)
		}
	})

	t.Run("login is limited per account whatever the letter case", func(t *testing.T) {
		wrong := func(email string) result {
			return callApp(t, limited, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
				"email": email, "password": testPassword + "x", "role": "WALI",
			})
		}

		for i := range 3 {
			res := wrong(waliEmail)
			require.Equal(t, http.StatusUnauthorized, res.Status, "attempt %d body %v", i+1, res.Body)
		}

		wrong(waliEmail).requireError(t, http.StatusTooManyRequests, "RATE_LIMITED")
		wrong(strings.ToUpper(waliEmail)).requireError(t, http.StatusTooManyRequests, "RATE_LIMITED")
		wrong(" "+waliEmail+" ").requireError(t, http.StatusTooManyRequests, "RATE_LIMITED")

		// another account behind the same address still signs in
		other := callApp(t, limited, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
			"email": guruEmail, "password": testPassword, "role": "GURU",
		})
		require.Equal(t, http.StatusOK, other.Status, "body %v", other.Body)
	})

	t.Run("a refresh token cannot get a fresh budget by adding an email", func(t *testing.T) {
		token := "00000000-0000-4000-8000-000000000000"

		for i := range 3 {
			res := callApp(t, limited, http.MethodPost, "/api/v1/auth/refresh-token", "", map[string]any{
				"refreshToken": token, "email": fmt.Sprintf("other.%d.%s@example.com", i, suffix()),
			})
			require.Equal(t, http.StatusUnauthorized, res.Status, "attempt %d body %v", i+1, res.Body)
		}

		callApp(t, limited, http.MethodPost, "/api/v1/auth/refresh-token", "", map[string]any{
			"refreshToken": token, "email": fmt.Sprintf("another.%s@example.com", suffix()),
		}).requireError(t, http.StatusTooManyRequests, "RATE_LIMITED")
	})

	t.Run("confirming the password to delete an account has its own limit per user", func(t *testing.T) {
		attempt := func() result {
			return callApp(t, limited, http.MethodDelete, "/api/v1/users/me", guruToken, map[string]any{"password": testPassword + "x"})
		}

		for i := range 3 {
			res := attempt()
			require.NotEqual(t, http.StatusTooManyRequests, res.Status, "attempt %d body %v", i+1, res.Body)
			require.GreaterOrEqual(t, res.Status, http.StatusBadRequest)
		}

		attempt().requireError(t, http.StatusTooManyRequests, "RATE_LIMITED")
	})

	t.Run("the health check is never limited", func(t *testing.T) {
		for range 30 {
			require.Equal(t, http.StatusOK, callApp(t, limited, http.MethodGet, "/healthz", "", nil).Status)
		}
	})
}
