package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gofiber "github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/fiber"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/jwt"
	"github.com/SyafaHadyan/kibooz-backend/internal/middleware"
)

type fixture struct {
	app *gofiber.App
	jwt *jwt.JWT
}

// newFixture has a route that needs a signed-in user and one that also confirms a password, with the given limits
func newFixture(t *testing.T, userMax int, passwordMax int, limited bool) fixture {
	t.Helper()

	cfg := &env.Env{
		UserLimiterMax: userMax, AuthLimiterMax: passwordMax, LimiterExpirationSeconds: 60, BodyLimitMB: 1,
		JWTSecretKey: "a-secret-key-that-is-long-enough-for-the-tests", JWTAccessExpiredMinutes: 60,
	}

	server := fiber.New(cfg, nil)
	tokens := jwt.New(cfg)

	var userLimit gofiber.Handler
	if limited {
		userLimit = server.UserLimiter(middleware.UserKey)
	}

	mw := middleware.NewMiddleware(tokens, userLimit)
	ok := func(c gofiber.Ctx) error { return c.JSON(map[string]string{"user": middleware.UserKey(c)}) }

	server.Router.Get("/me", mw.Authentication, mw.RequireRole(constants.RoleGuru, constants.RoleWali), ok)
	server.Router.Delete("/me", mw.Authentication, server.PasswordLimiter(middleware.UserKey), ok)

	return fixture{app: server.Fiber, jwt: tokens}
}

func (f fixture) token(t *testing.T, id uuid.UUID, role constants.Role) string {
	t.Helper()

	token, err := f.jwt.GenerateToken(id, role)
	require.NoError(t, err)

	return token
}

func (f fixture) do(t *testing.T, method string, token string) int {
	t.Helper()

	req := httptest.NewRequest(method, "/api/v1/me", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	res, err := f.app.Test(req, gofiber.TestConfig{Timeout: 10 * time.Second})
	require.NoError(t, err)

	defer func() { _ = res.Body.Close() }()

	return res.StatusCode
}

func TestEachUserHasTheirOwnLimit(t *testing.T) {
	f := newFixture(t, 3, 100, true)

	busy := f.token(t, uuid.New(), constants.RoleWali)
	quiet := f.token(t, uuid.New(), constants.RoleGuru)

	for range 3 {
		require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, busy))
	}

	require.Equal(t, http.StatusTooManyRequests, f.do(t, http.MethodGet, busy))

	// they share one address in this test, and the busy user must not use up the share of the quiet one
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, quiet))
}

func TestOneUserIsLimitedAcrossTokens(t *testing.T) {
	f := newFixture(t, 2, 100, true)
	id := uuid.New()

	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, f.token(t, id, constants.RoleWali)))
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, f.token(t, id, constants.RoleWali)))
	require.Equal(t, http.StatusTooManyRequests, f.do(t, http.MethodGet, f.token(t, id, constants.RoleWali)),
		"a new token of the same user must not reset the count")
}

func TestRequestsWithoutAValidTokenAreNotCounted(t *testing.T) {
	f := newFixture(t, 2, 100, true)

	for range 10 {
		require.Equal(t, http.StatusUnauthorized, f.do(t, http.MethodGet, ""))
		require.Equal(t, http.StatusUnauthorized, f.do(t, http.MethodGet, "not-a-token"))
	}

	token := f.token(t, uuid.New(), constants.RoleWali)

	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, token))
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, token))
}

func TestWithoutALimitEveryAuthenticatedRequestPasses(t *testing.T) {
	f := newFixture(t, 1, 1, false)
	token := f.token(t, uuid.New(), constants.RoleWali)

	for range 20 {
		require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, token))
	}
}

func TestPasswordLimitIsPerUserAndSeparateFromTheUserLimit(t *testing.T) {
	f := newFixture(t, 100, 2, true)

	first := f.token(t, uuid.New(), constants.RoleWali)
	second := f.token(t, uuid.New(), constants.RoleGuru)

	require.Equal(t, http.StatusOK, f.do(t, http.MethodDelete, first))
	require.Equal(t, http.StatusOK, f.do(t, http.MethodDelete, first))
	require.Equal(t, http.StatusTooManyRequests, f.do(t, http.MethodDelete, first))

	// the stricter limit does not touch the ordinary requests of the same user, or the attempts of another user
	require.Equal(t, http.StatusOK, f.do(t, http.MethodGet, first))
	require.Equal(t, http.StatusOK, f.do(t, http.MethodDelete, second))
}
