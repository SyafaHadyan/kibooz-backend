package e2e

import (
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/bootstrap"
)

func TestMetricsAreServedOnAListenerOfTheirOwn(t *testing.T) {
	app(t) // makes sure the shared environment defaults are set

	// a free port, found by listening on it once
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	address := probe.Addr().String()
	require.NoError(t, probe.Close())

	t.Setenv("METRICS_ADDRESS", address)

	started, err := bootstrap.Start("e2e-metrics")
	require.NoError(t, err)

	t.Cleanup(started.Close)

	api := started.App.Fiber

	scrape := func() string {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+address+"/metrics", nil)
		require.NoError(t, err)

		res, err := http.DefaultClient.Do(req)
		require.NoError(t, err)

		defer res.Body.Close()

		require.Equal(t, http.StatusOK, res.StatusCode)

		text, err := io.ReadAll(res.Body)
		require.NoError(t, err)

		return string(text)
	}

	require.Equal(t, http.StatusOK, callApp(t, api, http.MethodGet, "/healthz", "", nil).Status)
	callApp(t, api, http.MethodGet, "/api/v1/no/such/route", "", nil).requireError(t, http.StatusNotFound, "NOT_FOUND")
	callApp(t, api, http.MethodGet, "/api/v1/wali/children", "", nil).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")

	// a body that the size check refuses never reaches a route, and it is still counted
	callApp(t, api, http.MethodPost, "/api/v1/auth/login", "", map[string]any{"email": strings.Repeat("a", 70000)}).
		requireError(t, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE")

	text := scrape()

	require.Contains(t, text, `kibooz_http_requests_total{method="GET",route="/healthz",status="200"} 1`)
	require.Contains(t, text, `kibooz_http_requests_total{method="GET",route="unmatched",status="404"} 1`)
	require.Contains(t, text, `kibooz_http_requests_total{method="GET",route="/api/v1/wali/children",status="401"} 1`)
	require.Contains(t, text, `kibooz_http_requests_total{method="POST",route="unmatched",status="413"} 1`, "a refusal before routing has no route")
	require.NotContains(t, text, "no/such/route", "the path of a request that matched nothing is not a label")
	require.Contains(t, text, "go_sql_open_connections", "the pool of the database is measured")
	require.Contains(t, text, "go_goroutines")

	// the semester check runs once at start-up, and its result is a metric
	require.Eventually(t, func() bool {
		return strings.Contains(scrape(), `kibooz_job_runs_total{job="semester_check",result="success"} 1`)
	}, 10*time.Second, 100*time.Millisecond)

	t.Run("the public listener does not serve them", func(t *testing.T) {
		callApp(t, api, http.MethodGet, "/metrics", "", nil).requireError(t, http.StatusNotFound, "NOT_FOUND")
	})

	t.Run("a metrics address that cannot be used stops the start-up", func(t *testing.T) {
		t.Setenv("METRICS_ADDRESS", address)

		_, err := bootstrap.Start("e2e-metrics-taken")
		require.Error(t, err)
		require.Contains(t, err.Error(), "metrics")
	})
}
