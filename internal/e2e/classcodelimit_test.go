package e2e

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/bootstrap"
)

// TestGuessingClassCodesIsLimitedForTheWholeAPI shows that the budget of class codes belongs to the API and not to an
// email, because a guesser can send every guess from another account.
func TestGuessingClassCodesIsLimitedForTheWholeAPI(t *testing.T) {
	app(t) // makes sure the shared environment defaults are set

	t.Setenv("CLASS_CODE_LIMITER_MAX", "3")
	// the budget is one key for everybody, so this test counts under a prefix of its own
	t.Setenv("REDIS_KEY_PREFIX", "e2e-classcode-"+suffix()+":")

	started, err := bootstrap.Start("e2e-classcode")
	require.NoError(t, err)

	t.Cleanup(started.Close)

	limited := started.App.Fiber

	guru := callApp(t, limited, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email": fmt.Sprintf("guru.%s@example.com", suffix()), "password": testPassword, "fullName": "Code Teacher",
		"role": "GURU", "class": map[string]any{"name": "Codes"},
	})
	require.Equal(t, http.StatusCreated, guru.Status, "body %v", guru.Body)

	dashboard := callApp(t, limited, http.MethodGet, "/api/v1/guru/dashboard", guru.data("token").(string), nil)
	require.Equal(t, http.StatusOK, dashboard.Status, "body %v", dashboard.Body)

	code := dashboard.data("classOverview", "joinCode").(string)
	require.Len(t, code, 8)

	join := func(classCode string) result {
		return callApp(t, limited, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
			"email": fmt.Sprintf("wali.%s@example.com", suffix()), "password": testPassword, "fullName": "Guessing Parent",
			"role": "WALI", "classCode": classCode, "student": map[string]any{"nisn": nisn(), "fullName": "Guessing Child"},
		})
	}

	for range 3 {
		join("ZZZZZZZZ").requireError(t, http.StatusNotFound, "CLASS_CODE_NOT_FOUND")
	}

	t.Run("the right code is refused as well once the budget is gone, so the answer tells nothing", func(t *testing.T) {
		join(code).requireError(t, http.StatusTooManyRequests, "RATE_LIMITED")
	})

	t.Run("a teacher can still register because no class code is looked up", func(t *testing.T) {
		res := callApp(t, limited, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
			"email": fmt.Sprintf("guru.more.%s@example.com", suffix()), "password": testPassword, "fullName": "More Teacher",
			"role": "GURU", "class": map[string]any{"name": "More"},
		})
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)
	})
}
