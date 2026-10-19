package fiber_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	gofiber "github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/fiber"
	"github.com/SyafaHadyan/kibooz-backend/internal/logging"
)

// syncBuffer is a log destination that a test reads after the requests, and the handler may write from another goroutine
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) lines(t *testing.T) []map[string]any {
	t.Helper()

	b.mu.Lock()
	defer b.mu.Unlock()

	var lines []map[string]any

	for _, raw := range bytes.Split(bytes.TrimSpace(b.buf.Bytes()), []byte("\n")) {
		if len(raw) == 0 {
			continue
		}

		var line map[string]any
		require.NoError(t, json.Unmarshal(raw, &line), "every line is JSON, got %s", raw)

		lines = append(lines, line)
	}

	return lines
}

// logged serves the requests with the JSON logger of the process replaced by one that a test can read
func logged(t *testing.T) (*gofiber.App, *syncBuffer) {
	t.Helper()

	out := new(syncBuffer)
	previous := slog.Default()

	slog.SetDefault(logging.New(out))
	t.Cleanup(func() { slog.SetDefault(previous) })

	server := fiber.New(&env.Env{UserLimiterMax: 100, LimiterExpirationSeconds: 60, AuthLimiterMax: 10, BodyLimitMB: 1}, nil)

	server.Fiber.Get("/healthz", func(c gofiber.Ctx) error { return c.SendString("ok") })
	server.Router.Get("/fine", func(c gofiber.Ctx) error { return c.SendString("ok") })
	server.Router.Get("/invalid", func(c gofiber.Ctx) error {
		return apperror.Validation(map[string]string{"password": "must be at least 8 characters", "email": "invalid format"})
	})
	server.Router.Get("/busy", func(c gofiber.Ctx) error { return apperror.ErrBusy })
	server.Router.Get("/broken", func(c gofiber.Ctx) error { return apperror.Internal(assertError("database is down")) })
	server.Router.Get("/panic", func(c gofiber.Ctx) error { panic("handler bug") })

	return server.Fiber, out
}

type assertError string

func (e assertError) Error() string { return string(e) }

func request(t *testing.T, app *gofiber.App, path string) *http.Response {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)

	res, err := app.Test(req, gofiber.TestConfig{Timeout: 10 * time.Second})
	require.NoError(t, err)

	require.NoError(t, res.Body.Close())

	return res
}

func TestTheAccessLogIsOneJSONLineForEachRequest(t *testing.T) {
	app, out := logged(t)

	request(t, app, "/api/v1/fine?token=secret")

	lines := out.lines(t)
	require.Len(t, lines, 1)

	line := lines[0]
	require.Equal(t, "request", line["msg"])
	require.Equal(t, "INFO", line["level"])
	require.Equal(t, "GET", line["method"])
	require.Equal(t, "/api/v1/fine", line["path"], "the query string is not logged")
	require.Equal(t, "/api/v1/fine", line["route"])
	require.EqualValues(t, 200, line["status"])
	require.NotEmpty(t, line["request_id"])
	require.NotEmpty(t, line["ip"])
	require.Contains(t, line, "latency_ms")
	require.Contains(t, line, "bytes_out")
	require.Contains(t, line["time"], "T", "the time carries the date")
	require.NotContains(t, line, "error_code", "a request that worked has no error")
}

func TestAFailedRequestSaysWhyWithoutTheValuesThatWereRefused(t *testing.T) {
	app, out := logged(t)

	require.Equal(t, http.StatusBadRequest, request(t, app, "/api/v1/invalid").StatusCode)

	lines := out.lines(t)
	require.Len(t, lines, 1)

	line := lines[0]
	require.Equal(t, "WARN", line["level"])
	require.EqualValues(t, 400, line["status"])
	require.Equal(t, apperror.Validation(nil).Code, line["error_code"])
	require.Equal(t, "email,password", line["error_fields"], "the names of the fields, in order")
	require.NotContains(t, line["error_message"], "8 characters", "the line holds the message of the error and not the detail of a field")
}

func TestARateLimitedRequestLogsItsCodeAndWhenToRetry(t *testing.T) {
	app, out := logged(t)

	require.Equal(t, http.StatusTooManyRequests, request(t, app, "/api/v1/busy").StatusCode)

	line := out.lines(t)[0]
	require.Equal(t, "WARN", line["level"])
	require.EqualValues(t, 429, line["status"])
	require.Equal(t, "RATE_LIMITED", line["error_code"])
	require.Equal(t, "1", line["retry_after"])
}

func TestAServerErrorIsLoggedAtErrorLevel(t *testing.T) {
	app, out := logged(t)

	require.Equal(t, http.StatusInternalServerError, request(t, app, "/api/v1/broken").StatusCode)

	var access map[string]any

	for _, line := range out.lines(t) {
		if line["msg"] == "request" {
			access = line
		}
	}

	require.NotNil(t, access)
	require.Equal(t, "ERROR", access["level"])
	require.EqualValues(t, 500, access["status"])
}

func TestAPanicLogsItsStackAndTheRequestStillGetsALine(t *testing.T) {
	app, out := logged(t)

	require.Equal(t, http.StatusInternalServerError, request(t, app, "/api/v1/panic").StatusCode)

	var panicked, access map[string]any

	for _, line := range out.lines(t) {
		switch line["msg"] {
		case "panic recovered":
			panicked = line
		case "request":
			access = line
		}
	}

	require.NotNil(t, panicked, "the stack is logged")
	require.Equal(t, "ERROR", panicked["level"])
	require.Equal(t, "handler bug", panicked["panic"])
	require.Contains(t, panicked["stack"], "goroutine")
	require.Contains(t, panicked["stack"], "accesslog_test.go", "the stack names the handler that panicked")
	require.Equal(t, "/api/v1/panic", panicked["path"])
	require.NotEmpty(t, panicked["request_id"])

	require.NotNil(t, access, "the request that panicked is in the access log too")
	require.EqualValues(t, 500, access["status"])
	require.Equal(t, panicked["request_id"], access["request_id"], "the two lines can be joined")
}

func TestAHealthCheckThatWorksIsBelowTheDefaultLevel(t *testing.T) {
	app, out := logged(t)

	require.Equal(t, http.StatusOK, request(t, app, "/healthz").StatusCode)
	require.Empty(t, out.lines(t), "a monitor that asks every few seconds would drown the log")

	require.Equal(t, http.StatusNotFound, request(t, app, "/healthz/other").StatusCode)
	require.Len(t, out.lines(t), 1, "anything that is not a plain success is logged")
	require.True(t, strings.HasPrefix(out.lines(t)[0]["path"].(string), "/healthz"))
}
