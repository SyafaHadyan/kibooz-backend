package metrics_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/metrics"
)

// server answers like the API does, an apperror gets its own status and anything else is a 500
func server(t *testing.T, m *metrics.Metrics) *fiber.App {
	t.Helper()

	app := fiber.New(fiber.Config{ErrorHandler: func(c fiber.Ctx, err error) error {
		var appErr *apperror.Error
		if errors.As(err, &appErr) {
			return c.Status(appErr.Status).JSON(fiber.Map{"code": appErr.Code})
		}

		var fiberErr *fiber.Error
		if errors.As(err, &fiberErr) {
			return c.Status(fiberErr.Code).SendString(fiberErr.Message)
		}

		return c.Status(http.StatusInternalServerError).SendString("boom")
	}})

	app.Use(recover.New(), m.Middleware())

	app.Get("/api/v1/children/:id", func(c fiber.Ctx) error { return c.SendString("ok") })
	app.Get("/limited", func(c fiber.Ctx) error { return apperror.ErrRateLimited })
	app.Get("/busy", func(c fiber.Ctx) error { return apperror.ErrBusy })
	app.Get("/wrapped", func(c fiber.Ctx) error { return errors.Join(errors.New("context"), apperror.ErrRateLimited) })
	app.Get("/broken", func(c fiber.Ctx) error { return errors.New("database is down") })
	app.Get("/panic", func(c fiber.Ctx) error { panic("handler bug") })

	m.AllowRoutes(app.GetRoutes(true))

	return app
}

func get(t *testing.T, app *fiber.App, method string, path string) int {
	t.Helper()

	res, err := app.Test(httptest.NewRequest(method, path, nil))
	require.NoError(t, err)

	defer res.Body.Close()

	_, _ = io.Copy(io.Discard, res.Body)

	return res.StatusCode
}

// sample returns the value of the series of a metric whose labels are exactly want, and false when there is no such series
func sample(t *testing.T, m *metrics.Metrics, name string, want map[string]string) (float64, bool) {
	t.Helper()

	families, err := m.Registry().Gather()
	require.NoError(t, err)

	for _, family := range families {
		if family.GetName() != name {
			continue
		}

		for _, metric := range family.GetMetric() {
			labels := map[string]string{}
			for _, pair := range metric.GetLabel() {
				labels[pair.GetName()] = pair.GetValue()
			}

			if len(labels) != len(want) {
				continue
			}

			match := true

			for key, value := range want {
				match = match && labels[key] == value
			}

			if !match {
				continue
			}

			switch {
			case metric.Counter != nil:
				return metric.GetCounter().GetValue(), true
			case metric.Gauge != nil:
				return metric.GetGauge().GetValue(), true
			case metric.Histogram != nil:
				return float64(metric.GetHistogram().GetSampleCount()), true
			}
		}
	}

	return 0, false
}

func TestMiddlewareCountsARequestByItsRouteAndStatus(t *testing.T) {
	m := metrics.New()
	app := server(t, m)

	require.Equal(t, http.StatusOK, get(t, app, http.MethodGet, "/api/v1/children/11111111-1111-1111-1111-111111111111"))
	require.Equal(t, http.StatusOK, get(t, app, http.MethodGet, "/api/v1/children/22222222-2222-2222-2222-222222222222"))

	value, ok := sample(t, m, "kibooz_http_requests_total", map[string]string{"method": "GET", "route": "/api/v1/children/:id", "status": "200"})
	require.True(t, ok, "both ids are one series, the route and not the path is the label")
	require.EqualValues(t, 2, value)

	count, ok := sample(t, m, "kibooz_http_request_duration_seconds", map[string]string{"method": "GET", "route": "/api/v1/children/:id"})
	require.True(t, ok)
	require.EqualValues(t, 2, count)

	inFlight, ok := sample(t, m, "kibooz_http_requests_in_flight", map[string]string{})
	require.True(t, ok)
	require.Zero(t, inFlight, "nothing is running once the requests are answered")
}

func TestMiddlewareCountsTheStatusTheClientGets(t *testing.T) {
	m := metrics.New()
	app := server(t, m)

	require.Equal(t, http.StatusTooManyRequests, get(t, app, http.MethodGet, "/limited"))
	require.Equal(t, http.StatusInternalServerError, get(t, app, http.MethodGet, "/broken"))
	require.Equal(t, http.StatusInternalServerError, get(t, app, http.MethodGet, "/panic"))

	for path, status := range map[string]string{"/limited": "429", "/broken": "500", "/panic": "500"} {
		value, ok := sample(t, m, "kibooz_http_requests_total", map[string]string{"method": "GET", "route": path, "status": status})
		require.True(t, ok, path)
		require.EqualValues(t, 1, value, path)
	}

	inFlight, _ := sample(t, m, "kibooz_http_requests_in_flight", map[string]string{})
	require.Zero(t, inFlight, "a panic does not leave a request counted as running")
}

func TestMiddlewareDoesNotLetTheClientPickTheLabels(t *testing.T) {
	m := metrics.New()
	app := server(t, m)

	get(t, app, http.MethodGet, "/no/such/route/"+strings.Repeat("a", 40))
	// Fiber answers a method it does not know with 501 before any middleware, so TRACE is one that reaches the counter
	get(t, app, http.MethodTrace, "/no/such/route")

	value, ok := sample(t, m, "kibooz_http_requests_total", map[string]string{"method": "GET", "route": metrics.Unknown, "status": "404"})
	require.True(t, ok, "a path that no route has is counted as %q", metrics.Unknown)
	require.EqualValues(t, 1, value)

	value, ok = sample(t, m, "kibooz_http_requests_total", map[string]string{"method": "OTHER", "route": metrics.Unknown, "status": "404"})
	require.True(t, ok, "a method that has no label of its own is counted as OTHER")
	require.EqualValues(t, 1, value)
}

func TestRefusalsAreCountedApart(t *testing.T) {
	m := metrics.New()
	app := server(t, m)

	get(t, app, http.MethodGet, "/limited")
	get(t, app, http.MethodGet, "/limited")
	get(t, app, http.MethodGet, "/wrapped")
	get(t, app, http.MethodGet, "/busy")

	limited, ok := sample(t, m, "kibooz_rate_limited_total", map[string]string{"route": "/limited"})
	require.True(t, ok)
	require.EqualValues(t, 2, limited)

	wrapped, ok := sample(t, m, "kibooz_rate_limited_total", map[string]string{"route": "/wrapped"})
	require.True(t, ok, "a limit that was wrapped in another error still counts")
	require.EqualValues(t, 1, wrapped)

	_, ok = sample(t, m, "kibooz_rate_limited_total", map[string]string{"route": "/busy"})
	require.False(t, ok, "a busy password pool is not a rate limiter")

	busy, ok := sample(t, m, "kibooz_password_hash_busy_total", map[string]string{})
	require.True(t, ok)
	require.EqualValues(t, 1, busy)
}

func TestJobRunsAreCountedAndTheLastSuccessIsKept(t *testing.T) {
	m := metrics.New()
	observe := m.Job("sweep")

	_, ok := sample(t, m, "kibooz_job_runs_total", map[string]string{"job": "sweep", "result": "success"})
	require.True(t, ok, "the series exists before the first run, so a job that never runs can be seen")

	_, ok = sample(t, m, "kibooz_job_last_success_timestamp_seconds", map[string]string{"job": "sweep"})
	require.False(t, ok, "there is no success time before a success")

	observe(errors.New("bucket refused"))
	observe(nil)
	observe(nil)

	success, _ := sample(t, m, "kibooz_job_runs_total", map[string]string{"job": "sweep", "result": "success"})
	failure, _ := sample(t, m, "kibooz_job_runs_total", map[string]string{"job": "sweep", "result": "failure"})
	require.EqualValues(t, 2, success)
	require.EqualValues(t, 1, failure)

	last, ok := sample(t, m, "kibooz_job_last_success_timestamp_seconds", map[string]string{"job": "sweep"})
	require.True(t, ok)
	require.InDelta(t, float64(time.Now().Unix()), last, 5)
}

func TestListenerServesOnlyTheMetricsPage(t *testing.T) {
	m := metrics.New()
	m.Job("sweep")(nil)

	off, err := m.Serve("")
	require.NoError(t, err)
	require.Empty(t, off.Addr(), "an empty address serves nothing")
	off.Close()

	listener, err := m.Serve("127.0.0.1:0")
	require.NoError(t, err)

	defer listener.Close()

	body := func(method string, path string) (int, string) {
		req, err := http.NewRequestWithContext(t.Context(), method, "http://"+listener.Addr()+path, nil)
		require.NoError(t, err)

		res, err := http.DefaultClient.Do(req)
		require.NoError(t, err)

		defer res.Body.Close()

		text, err := io.ReadAll(res.Body)
		require.NoError(t, err)

		return res.StatusCode, string(text)
	}

	status, text := body(http.MethodGet, "/metrics")
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, text, "kibooz_job_runs_total")
	require.Contains(t, text, "go_goroutines")
	require.Contains(t, text, "process_resident_memory_bytes")

	status, _ = body(http.MethodGet, "/")
	require.Equal(t, http.StatusNotFound, status)

	status, _ = body(http.MethodGet, "/healthz")
	require.Equal(t, http.StatusNotFound, status)

	status, _ = body(http.MethodPost, "/metrics")
	require.Equal(t, http.StatusMethodNotAllowed, status)

	_, err = m.Serve(listener.Addr())
	require.Error(t, err, "an address that is taken stops the start-up")
}
