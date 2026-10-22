package fiber_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gofiber "github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/fiber"
)

// photo is a body that is larger than a JSON request may be, so it can only be a photo
func photo() *strings.Reader {
	return strings.NewReader(strings.Repeat("a", constants.JSONBodyMaxBytes+1))
}

// wait fails the test instead of hanging it when the handler of the first request never starts
func wait(t *testing.T, ch <-chan struct{}) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler did not start")
	}
}

func TestOnlyTheConfiguredNumberOfUploadsRunAtOnce(t *testing.T) {
	server := fiber.New(&env.Env{UserLimiterMax: 100, LimiterExpirationSeconds: 60, AuthLimiterMax: 10, BodyLimitMB: 1, UploadConcurrency: 1}, nil)

	started := make(chan struct{})
	release := make(chan struct{})

	server.Router.Post("/users/avatar", server.UploadGate(), func(c gofiber.Ctx) error {
		started <- struct{}{}
		<-release

		return c.SendStatus(http.StatusOK)
	})

	first := make(chan int, 1)

	go func() {
		res, err := server.Fiber.Test(httptest.NewRequest(http.MethodPost, "/api/v1/users/avatar", photo()), gofiber.TestConfig{Timeout: 10 * time.Second})
		if err != nil {
			first <- -1

			return
		}

		defer res.Body.Close()

		first <- res.StatusCode
	}()

	wait(t, started)

	res, err := server.Fiber.Test(httptest.NewRequest(http.MethodPost, "/api/v1/users/avatar", photo()), gofiber.TestConfig{Timeout: 10 * time.Second})
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

		again, err := server.Fiber.Test(httptest.NewRequest(http.MethodPost, "/api/v1/users/avatar", photo()), gofiber.TestConfig{Timeout: 10 * time.Second})
		require.NoError(t, err)

		defer again.Body.Close()

		require.Equal(t, http.StatusOK, again.StatusCode)
	})
}

// a scan claim has an optional photo, and one without it is a few hundred bytes that holds no photo in memory
func TestARequestThatCannotHoldAPhotoNeverTakesASlot(t *testing.T) {
	server := fiber.New(&env.Env{UserLimiterMax: 100, LimiterExpirationSeconds: 60, AuthLimiterMax: 10, BodyLimitMB: 1, UploadConcurrency: 1}, nil)

	started := make(chan struct{})
	release := make(chan struct{})

	server.Router.Post("/users/avatar", server.UploadGate(), func(c gofiber.Ctx) error {
		if len(c.Body()) > constants.JSONBodyMaxBytes {
			started <- struct{}{}
			<-release
		}

		return c.SendStatus(http.StatusOK)
	})

	done := make(chan struct{})

	go func() {
		defer close(done)

		res, err := server.Fiber.Test(httptest.NewRequest(http.MethodPost, "/api/v1/users/avatar", photo()), gofiber.TestConfig{Timeout: 10 * time.Second})
		if err == nil {
			_ = res.Body.Close()
		}
	}()

	wait(t, started)

	for range 5 {
		res, err := server.Fiber.Test(httptest.NewRequest(http.MethodPost, "/api/v1/users/avatar", strings.NewReader(`{"studentId":"x"}`)), gofiber.TestConfig{Timeout: 10 * time.Second})
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, res.StatusCode, "a small body is not held back while the only slot is taken")
		_ = res.Body.Close()
	}

	close(release)
	<-done
}

func TestASlotIsGivenBackWhenTheHandlerPanics(t *testing.T) {
	server := fiber.New(&env.Env{UserLimiterMax: 100, LimiterExpirationSeconds: 60, AuthLimiterMax: 10, BodyLimitMB: 1, UploadConcurrency: 1}, nil)

	calls := 0

	server.Router.Post("/users/avatar", server.UploadGate(), func(c gofiber.Ctx) error {
		calls++
		if calls == 1 {
			panic("the handler broke")
		}

		return c.SendStatus(http.StatusOK)
	})

	first, err := server.Fiber.Test(httptest.NewRequest(http.MethodPost, "/api/v1/users/avatar", photo()), gofiber.TestConfig{Timeout: 10 * time.Second})
	require.NoError(t, err)
	require.Equal(t, http.StatusInternalServerError, first.StatusCode)
	_ = first.Body.Close()

	second, err := server.Fiber.Test(httptest.NewRequest(http.MethodPost, "/api/v1/users/avatar", photo()), gofiber.TestConfig{Timeout: 10 * time.Second})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, second.StatusCode, "the slot of the request that panicked was given back")
	_ = second.Body.Close()
}
