package fiber_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	gofiber "github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/fiber"
)

// recordingStorage keeps what the limiter stores in memory and remembers every key it was asked for
type recordingStorage struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newRecordingStorage() *recordingStorage {
	return &recordingStorage{data: map[string][]byte{}}
}

func (s *recordingStorage) keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	keys := make([]string, 0, len(s.data))
	for key := range s.data {
		keys = append(keys, key)
	}

	return keys
}

func (s *recordingStorage) GetWithContext(_ context.Context, key string) ([]byte, error) {
	return s.Get(key)
}

func (s *recordingStorage) Get(key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.data[key], nil
}

func (s *recordingStorage) SetWithContext(_ context.Context, key string, val []byte, exp time.Duration) error {
	return s.Set(key, val, exp)
}

func (s *recordingStorage) Set(key string, val []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[key] = val

	return nil
}

func (s *recordingStorage) DeleteWithContext(_ context.Context, key string) error {
	return s.Delete(key)
}

func (s *recordingStorage) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, key)

	return nil
}

func (s *recordingStorage) ResetWithContext(context.Context) error {
	return s.Reset()
}

func (s *recordingStorage) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data = map[string][]byte{}

	return nil
}

func (s *recordingStorage) Close() error {
	return nil
}

// publicServer has two routes limited per email and two limited per refresh token, with the given number of requests each
func publicServer(accountMax int, storage gofiber.Storage) *gofiber.App {
	server := fiber.New(&env.Env{UserLimiterMax: 100, LimiterExpirationSeconds: 60, AuthLimiterMax: accountMax, BodyLimitMB: 1}, storage)

	ok := func(c gofiber.Ctx) error { return c.JSON(map[string]string{"ok": "yes"}) }
	byEmail := server.EmailLimiter()
	byToken := server.TokenLimiter()

	server.Router.Post("/login", byEmail, ok)
	server.Router.Post("/register", byEmail, ok)
	server.Router.Post("/refresh", byToken, ok)
	server.Router.Post("/logout", byToken, ok)

	return server.Fiber
}

func status(t *testing.T, app *gofiber.App, path string, body string) int {
	t.Helper()

	res := post(t, app, "/api/v1"+path, body)

	return res.StatusCode
}

func TestEmailLimiterCountsRequestsPerEmail(t *testing.T) {
	app := publicServer(2, nil)

	require.Equal(t, http.StatusOK, status(t, app, "/login", `{"email":"a@example.com"}`))
	require.Equal(t, http.StatusOK, status(t, app, "/login", `{"email":"a@example.com"}`))
	require.Equal(t, http.StatusTooManyRequests, status(t, app, "/login", `{"email":"a@example.com"}`))

	t.Run("the answer names the error code", func(t *testing.T) {
		res := post(t, app, "/api/v1/login", `{"email":"a@example.com"}`)

		var body map[string]any

		raw, err := io.ReadAll(res.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, &body))
		require.Equal(t, "RATE_LIMITED", body["errorCode"])
		require.NotEmpty(t, res.Header.Get("Retry-After"))
	})

	t.Run("another email is not affected", func(t *testing.T) {
		require.Equal(t, http.StatusOK, status(t, app, "/login", `{"email":"b@example.com"}`))
	})
}

func TestEmailLimiterNormalizesTheEmail(t *testing.T) {
	app := publicServer(1, nil)

	require.Equal(t, http.StatusOK, status(t, app, "/login", `{"email":"Teacher@Example.com"}`))
	require.Equal(t, http.StatusTooManyRequests, status(t, app, "/login", `{"email":"  teacher@example.com "}`))
	require.Equal(t, http.StatusTooManyRequests, status(t, app, "/login", `{"email":"TEACHER@EXAMPLE.COM"}`))
}

func TestEveryRouteKeepsItsOwnBudget(t *testing.T) {
	app := publicServer(1, nil)

	require.Equal(t, http.StatusOK, status(t, app, "/login", `{"email":"a@example.com"}`))
	require.Equal(t, http.StatusOK, status(t, app, "/register", `{"email":"a@example.com"}`))
	require.Equal(t, http.StatusTooManyRequests, status(t, app, "/login", `{"email":"a@example.com"}`))

	require.Equal(t, http.StatusOK, status(t, app, "/refresh", `{"refreshToken":"token-one"}`))
	require.Equal(t, http.StatusOK, status(t, app, "/logout", `{"refreshToken":"token-one"}`))
	require.Equal(t, http.StatusTooManyRequests, status(t, app, "/refresh", `{"refreshToken":"token-one"}`))
}

func TestTokenLimiterLimitsARefreshTokenButNotOtherTokens(t *testing.T) {
	app := publicServer(1, nil)

	require.Equal(t, http.StatusOK, status(t, app, "/refresh", `{"refreshToken":"token-one"}`))
	require.Equal(t, http.StatusTooManyRequests, status(t, app, "/refresh", `{"refreshToken":"token-one"}`))
	require.Equal(t, http.StatusOK, status(t, app, "/refresh", `{"refreshToken":"token-two"}`))
}

// The refresh and logout routes read only the token, so an email in the same body must not buy a fresh budget
func TestTokenLimiterIgnoresAnEmailInTheBody(t *testing.T) {
	app := publicServer(1, nil)

	require.Equal(t, http.StatusOK, status(t, app, "/refresh", `{"refreshToken":"token-one","email":"first@example.com"}`))

	for _, email := range []string{"second@example.com", "third@example.com", "first@example.com", ""} {
		body := `{"refreshToken":"token-one","email":"` + email + `"}`

		require.Equal(t, http.StatusTooManyRequests, status(t, app, "/refresh", body), "email %q", email)
	}

	// logout keeps its own budget of one for the same token, and a different email does not renew it either
	require.Equal(t, http.StatusOK, status(t, app, "/logout", `{"refreshToken":"token-one","email":"first@example.com"}`))
	require.Equal(t, http.StatusTooManyRequests, status(t, app, "/logout", `{"refreshToken":"token-one","email":"second@example.com"}`))

	t.Run("a body with only an email names no token", func(t *testing.T) {
		for range 3 {
			require.Equal(t, http.StatusOK, status(t, app, "/refresh", `{"email":"someone@example.com"}`))
		}
	})
}

// The login and register routes read only the email, so a refresh token in the same body must not buy a fresh budget
func TestEmailLimiterIgnoresARefreshTokenInTheBody(t *testing.T) {
	app := publicServer(1, nil)

	require.Equal(t, http.StatusOK, status(t, app, "/login", `{"email":"a@example.com","refreshToken":"one"}`))
	require.Equal(t, http.StatusTooManyRequests, status(t, app, "/login", `{"email":"a@example.com","refreshToken":"two"}`))
	require.Equal(t, http.StatusTooManyRequests, status(t, app, "/login", `{"email":"a@example.com"}`))
}

func TestLimitersIgnoreRequestsThatNameNoAccount(t *testing.T) {
	app := publicServer(1, nil)

	for _, path := range []string{"/login", "/refresh"} {
		for _, body := range []string{``, `{}`, `not json`, `{"email":""}`, `{"email":"   "}`, `{"refreshToken":""}`, `[]`} {
			for range 3 {
				require.Equal(t, http.StatusOK, status(t, app, path, body), "%s body %q", path, body)
			}
		}
	}
}

func TestEmailLimiterNeverLimitsByAddress(t *testing.T) {
	app := publicServer(1, nil)

	// many different accounts behind one address, such as a whole class on the school network
	for i := range 25 {
		body := `{"email":"parent` + strings.Repeat("x", i) + `@example.com"}`

		require.Equal(t, http.StatusOK, status(t, app, "/login", body), "account %d", i)
	}
}

func sha(value string) string {
	sum := sha256.Sum256([]byte(value))

	return hex.EncodeToString(sum[:])
}

// The keys are pinned exactly. Anything added to them, such as the address of the client, or anything dropped from
// them, such as the lower casing of the email, changes who shares a budget and has to fail here.
func TestLimiterKeysAreTheRouteAndAHashOfTheIdentifier(t *testing.T) {
	storage := newRecordingStorage()
	app := publicServer(5, storage)

	status(t, app, "/login", `{"email":"Private.Person@example.com"}`)
	status(t, app, "/refresh", `{"refreshToken":"secret-refresh-token","email":"ignored@example.com"}`)

	require.ElementsMatch(t, []string{
		"email:/api/v1/login:" + sha("private.person@example.com"),
		"token:/api/v1/refresh:" + sha("secret-refresh-token"),
	}, storage.keys(), "neither an email, a token nor an address may be in a key")
}

func TestUserAndPasswordLimitersKeyOnlyTheUser(t *testing.T) {
	storage := newRecordingStorage()
	server := fiber.New(&env.Env{UserLimiterMax: 5, AuthLimiterMax: 5, LimiterExpirationSeconds: 60, BodyLimitMB: 1}, storage)

	who := func(gofiber.Ctx) string { return "user-1" }
	ok := func(c gofiber.Ctx) error { return c.JSON(map[string]string{"ok": "yes"}) }

	server.Router.Get("/ordinary", server.UserLimiter(who), ok)
	server.Router.Delete("/sensitive", server.PasswordLimiter(who), ok)

	get(t, server.Fiber, "/api/v1/ordinary", nil)

	res, err := server.Fiber.Test(httptest.NewRequest(http.MethodDelete, "/api/v1/sensitive", nil), gofiber.TestConfig{Timeout: 10 * time.Second})
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())

	require.ElementsMatch(t, []string{"user:user-1", "confirm:user-1"}, storage.keys())
}

func TestLimitersAreSharedThroughTheStorage(t *testing.T) {
	storage := newRecordingStorage()

	first := publicServer(1, storage)
	second := publicServer(1, storage)

	require.Equal(t, http.StatusOK, status(t, first, "/login", `{"email":"a@example.com"}`))
	require.Equal(t, http.StatusTooManyRequests, status(t, second, "/login", `{"email":"a@example.com"}`), "a second instance must see the first one's requests")
}
