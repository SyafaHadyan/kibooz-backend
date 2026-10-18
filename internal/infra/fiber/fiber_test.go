package fiber_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gofiber "github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/fiber"
)

// newServer has one public route that is limited per account, with the given number of requests per account
func newServer(accountMax int) *gofiber.App {
	server := fiber.New(&env.Env{UserLimiterMax: 100, LimiterExpirationSeconds: 60, AuthLimiterMax: accountMax, BodyLimitMB: 1}, nil)

	ok := func(c gofiber.Ctx) error { return c.JSON(map[string]string{"ok": "yes"}) }

	server.Fiber.Get("/ping", ok)
	server.Router.Post("/public", server.EmailLimiter(), ok)

	return server.Fiber
}

func get(t *testing.T, app *gofiber.App, path string, headers map[string]string) *http.Response {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	for name, value := range headers {
		req.Header.Set(name, value)
	}

	res, err := app.Test(req, gofiber.TestConfig{Timeout: 10 * time.Second})
	require.NoError(t, err)

	t.Cleanup(func() { _ = res.Body.Close() })

	return res
}

func post(t *testing.T, app *gofiber.App, path string, body string) *http.Response {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	res, err := app.Test(req, gofiber.TestConfig{Timeout: 10 * time.Second})
	require.NoError(t, err)

	t.Cleanup(func() { _ = res.Body.Close() })

	return res
}

func requireSecurityHeaders(t *testing.T, res *http.Response) {
	t.Helper()

	want := map[string]string{
		"X-Content-Type-Options":       "nosniff",
		"X-Frame-Options":              "DENY",
		"Content-Security-Policy":      "default-src 'none'; frame-ancestors 'none'",
		"Referrer-Policy":              "no-referrer",
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
	}

	for name, value := range want {
		require.Equal(t, value, res.Header.Get(name), name)
	}
}

func TestSecurityHeadersOnEveryKindOfResponse(t *testing.T) {
	t.Run("successful response", func(t *testing.T) {
		res := get(t, newServer(100), "/ping", nil)

		require.Equal(t, http.StatusOK, res.StatusCode)
		requireSecurityHeaders(t, res)
	})

	t.Run("unknown route", func(t *testing.T) {
		res := get(t, newServer(100), "/nothing-here", nil)

		require.Equal(t, http.StatusNotFound, res.StatusCode)
		requireSecurityHeaders(t, res)
	})

	t.Run("rate limited response", func(t *testing.T) {
		app := newServer(1)
		post(t, app, "/api/v1/public", `{"email":"someone@example.com"}`)

		res := post(t, app, "/api/v1/public", `{"email":"someone@example.com"}`)

		require.Equal(t, http.StatusTooManyRequests, res.StatusCode)
		requireSecurityHeaders(t, res)
	})
}

// HSTS is left to the proxy that terminates TLS, because includeSubDomains would reach every subdomain of the host
func TestStrictTransportSecurityIsNotSet(t *testing.T) {
	res := get(t, newServer(100), "/ping", map[string]string{"X-Forwarded-Proto": "https"})

	require.Empty(t, res.Header.Get("Strict-Transport-Security"))
}

// Fiber decodes a body by its Content-Encoding when a handler reads it, and the size limit counts only the bytes that were
// sent, so an encoded body is refused before anything reads it
func TestARequestWithAnEncodedBodyIsRefused(t *testing.T) {
	app := newServer(100)

	// the start of a zstd frame, which is never read because the request is refused first
	zstd := "\x28\xb5\x2f\xfd\x20\x00\x01\x00\x00"

	for _, encoding := range []string{"zstd", "gzip", "br", "deflate", "GZIP", " zstd ", "gzip, zstd", "unknown"} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/public", strings.NewReader(zstd))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Content-Encoding", encoding)

		res, err := app.Test(req, gofiber.TestConfig{Timeout: 10 * time.Second})
		require.NoError(t, err)

		t.Cleanup(func() { _ = res.Body.Close() })

		require.Equal(t, http.StatusUnsupportedMediaType, res.StatusCode, "encoding %q", encoding)
		requireSecurityHeaders(t, res)
	}

	t.Run("a repeated header is read line by line", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/public", strings.NewReader(zstd))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Add("Content-Encoding", "identity")
		req.Header.Add("Content-Encoding", "gzip")

		res, err := app.Test(req, gofiber.TestConfig{Timeout: 10 * time.Second})
		require.NoError(t, err)

		t.Cleanup(func() { _ = res.Body.Close() })

		require.Equal(t, http.StatusUnsupportedMediaType, res.StatusCode)
		requireSecurityHeaders(t, res)
	})

	for _, encoding := range []string{"", "identity", "Identity"} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/public", strings.NewReader(`{"email":"a@example.com"}`))
		req.Header.Set("Content-Type", "application/json")

		if encoding != "" {
			req.Header.Set("Content-Encoding", encoding)
		}

		res, err := app.Test(req, gofiber.TestConfig{Timeout: 10 * time.Second})
		require.NoError(t, err)

		t.Cleanup(func() { _ = res.Body.Close() })

		require.Equal(t, http.StatusOK, res.StatusCode, "encoding %q", encoding)
	}
}
