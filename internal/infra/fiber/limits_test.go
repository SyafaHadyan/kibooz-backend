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

// publicServer has two public routes limited per account, with the given number of requests per account
func publicServer(accountMax int, storage gofiber.Storage) *gofiber.App {
	server := fiber.New(&env.Env{UserLimiterMax: 100, LimiterExpirationSeconds: 60, AuthLimiterMax: accountMax, BodyLimitMB: 1}, storage)

	ok := func(c gofiber.Ctx) error { return c.JSON(map[string]string{"ok": "yes"}) }
	limit := server.AccountLimiter()

	server.Router.Post("/login", limit, ok)
	server.Router.Post("/refresh", limit, ok)

	return server.Fiber
}

func status(t *testing.T, app *gofiber.App, path string, body string) int {
	t.Helper()

	res := post(t, app, "/api/v1"+path, body)

	return res.StatusCode
}

func TestAccountLimiterCountsRequestsPerAccount(t *testing.T) {
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

	t.Run("another account is not affected", func(t *testing.T) {
		require.Equal(t, http.StatusOK, status(t, app, "/login", `{"email":"b@example.com"}`))
	})
}

func TestAccountLimiterNormalizesTheEmail(t *testing.T) {
	app := publicServer(1, nil)

	require.Equal(t, http.StatusOK, status(t, app, "/login", `{"email":"Teacher@Example.com"}`))
	require.Equal(t, http.StatusTooManyRequests, status(t, app, "/login", `{"email":"  teacher@example.com "}`))
	require.Equal(t, http.StatusTooManyRequests, status(t, app, "/login", `{"email":"TEACHER@EXAMPLE.COM"}`))
}

func TestAccountLimiterKeepsEveryRouteSeparate(t *testing.T) {
	app := publicServer(1, nil)

	require.Equal(t, http.StatusOK, status(t, app, "/login", `{"email":"a@example.com"}`))
	require.Equal(t, http.StatusOK, status(t, app, "/refresh", `{"email":"a@example.com"}`))
	require.Equal(t, http.StatusTooManyRequests, status(t, app, "/login", `{"email":"a@example.com"}`))
}

func TestAccountLimiterLimitsARefreshTokenButNotOtherTokens(t *testing.T) {
	app := publicServer(1, nil)

	require.Equal(t, http.StatusOK, status(t, app, "/refresh", `{"refreshToken":"token-one"}`))
	require.Equal(t, http.StatusTooManyRequests, status(t, app, "/refresh", `{"refreshToken":"token-one"}`))
	require.Equal(t, http.StatusOK, status(t, app, "/refresh", `{"refreshToken":"token-two"}`))
}

func TestAccountLimiterIgnoresRequestsThatNameNoAccount(t *testing.T) {
	app := publicServer(1, nil)

	for _, body := range []string{``, `{}`, `not json`, `{"email":""}`, `{"email":"   "}`, `[]`} {
		for range 3 {
			require.Equal(t, http.StatusOK, status(t, app, "/login", body), "body %q", body)
		}
	}
}

func TestAccountLimiterNeverLimitsByAddress(t *testing.T) {
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
func TestAccountLimiterKeysAreTheRouteAndAHashOfTheAccount(t *testing.T) {
	storage := newRecordingStorage()
	app := publicServer(5, storage)

	status(t, app, "/login", `{"email":"Private.Person@example.com"}`)
	status(t, app, "/refresh", `{"refreshToken":"secret-refresh-token"}`)

	require.ElementsMatch(t, []string{
		"account:/api/v1/login:" + sha("private.person@example.com"),
		"account:/api/v1/refresh:" + sha("secret-refresh-token"),
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

	require.ElementsMatch(t, []string{"user:user-1", "password:user-1"}, storage.keys())
}

func TestAccountLimiterIsSharedThroughTheStorage(t *testing.T) {
	storage := newRecordingStorage()

	first := publicServer(1, storage)
	second := publicServer(1, storage)

	require.Equal(t, http.StatusOK, status(t, first, "/login", `{"email":"a@example.com"}`))
	require.Equal(t, http.StatusTooManyRequests, status(t, second, "/login", `{"email":"a@example.com"}`), "a second instance must see the first one's requests")
}
