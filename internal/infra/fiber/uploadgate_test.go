package fiber_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gofiber "github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/fiber"
)

func TestOnlyTheConfiguredNumberOfUploadsRunAtOnce(t *testing.T) {
	server := fiber.New(&env.Env{UserLimiterMax: 100, LimiterExpirationSeconds: 60, AuthLimiterMax: 10, BodyLimitMB: 1, UploadConcurrency: 1}, nil)

	started := make(chan struct{})
	release := make(chan struct{})

	server.Router.Post("/photo", server.UploadGate(), func(c gofiber.Ctx) error {
		started <- struct{}{}
		<-release

		return c.SendStatus(http.StatusOK)
	})

	first := make(chan int, 1)

	go func() {
		res, err := server.Fiber.Test(httptest.NewRequest(http.MethodPost, "/api/v1/photo", nil), gofiber.TestConfig{Timeout: 10 * time.Second})
		if err != nil {
			first <- -1

			return
		}

		defer res.Body.Close()

		first <- res.StatusCode
	}()

	<-started

	res, err := server.Fiber.Test(httptest.NewRequest(http.MethodPost, "/api/v1/photo", nil), gofiber.TestConfig{Timeout: 10 * time.Second})
	require.NoError(t, err)

	defer res.Body.Close()

	require.Equal(t, http.StatusTooManyRequests, res.StatusCode, "the second upload finds the only slot taken")
	require.Equal(t, "1", res.Header.Get("Retry-After"))

	var body map[string]any

	require.NoError(t, json.NewDecoder(res.Body).Decode(&body))
	require.Equal(t, "RATE_LIMITED", body["errorCode"])

	close(release)
	require.Equal(t, http.StatusOK, <-first)

	t.Run("a slot that is given back is used by the next upload", func(t *testing.T) {
		go func() { <-started }()

		again, err := server.Fiber.Test(httptest.NewRequest(http.MethodPost, "/api/v1/photo", nil), gofiber.TestConfig{Timeout: 10 * time.Second})
		require.NoError(t, err)

		defer again.Body.Close()

		require.Equal(t, http.StatusOK, again.StatusCode)
	})
}
