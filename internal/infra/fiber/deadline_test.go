package fiber_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gofiber "github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/fiber"
)

func deadlineServer(timeoutSeconds int) *gofiber.App {
	server := fiber.New(&env.Env{UserLimiterMax: 100, LimiterExpirationSeconds: 60, AuthLimiterMax: 10, BodyLimitMB: 1, RequestTimeoutSeconds: timeoutSeconds}, nil)

	server.Router.Get("/deadline", func(c gofiber.Ctx) error {
		at, ok := c.Context().Deadline()
		if !ok {
			return c.SendString("none")
		}

		if time.Until(at) > time.Duration(timeoutSeconds)*time.Second {
			return c.SendString("too late")
		}

		return c.SendString("set")
	})

	// waits the way a stalled query or storage call does, until the context ends
	server.Router.Get("/stall", func(c gofiber.Ctx) error {
		select {
		case <-c.Context().Done():
			return c.Context().Err()
		case <-time.After(5 * time.Second):
			return c.SendString("finished")
		}
	})

	return server.Fiber
}

func fetch(t *testing.T, app *gofiber.App, path string) (int, string) {
	t.Helper()

	res, err := app.Test(httptest.NewRequest(http.MethodGet, path, nil), gofiber.TestConfig{Timeout: 10 * time.Second})
	require.NoError(t, err)

	t.Cleanup(func() { _ = res.Body.Close() })

	buf := make([]byte, 64)
	n, _ := res.Body.Read(buf)

	return res.StatusCode, string(buf[:n])
}

func TestEveryRequestGetsADeadline(t *testing.T) {
	status, body := fetch(t, deadlineServer(10), "/api/v1/deadline")

	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "set", body)
}

func TestAStalledRequestIsCancelledWhenTheTimeIsUp(t *testing.T) {
	started := time.Now()

	status, _ := fetch(t, deadlineServer(1), "/api/v1/stall")

	require.Equal(t, http.StatusInternalServerError, status)
	require.Less(t, time.Since(started), 4*time.Second, "the handler waited for its own timer and not for the deadline")
}

func TestATimeoutThatIsNotPositiveLeavesTheContextAlone(t *testing.T) {
	status, body := fetch(t, deadlineServer(0), "/api/v1/deadline")

	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "none", body)
}
