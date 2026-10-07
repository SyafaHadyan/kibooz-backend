package healthcheck_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/healthcheck"
)

func serving(t *testing.T, status int) *httptest.Server {
	t.Helper()

	// any other path answers 418, so a probe that calls the wrong path cannot pass
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			w.WriteHeader(http.StatusTeapot)

			return
		}

		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)

	return server
}

func portOf(t *testing.T, server *httptest.Server) string {
	t.Helper()

	parsed, err := url.Parse(server.URL)
	require.NoError(t, err)

	return parsed.Port()
}

func TestURLUsesThePortWhenItIsANumber(t *testing.T) {
	require.Equal(t, "http://127.0.0.1:9000/healthz", healthcheck.URL("9000"))
	require.Equal(t, "http://127.0.0.1:65535/healthz", healthcheck.URL("65535"))
}

func TestURLFallsBackToTheDefaultPort(t *testing.T) {
	for name, port := range map[string]string{
		"empty":        "",
		"not a number": "abc",
		"negative":     "-1",
		"too large":    "65536",
		"injection":    "80/../admin",
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, "http://127.0.0.1:8080/healthz", healthcheck.URL(port))
		})
	}
}

func TestCheckPassesOn200(t *testing.T) {
	server := serving(t, http.StatusOK)

	require.NoError(t, healthcheck.Check(t.Context(), server.Client(), server.URL+"/healthz"))
}

func TestCheckFailsOnAnyOtherStatus(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusMovedPermanently, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		server := serving(t, status)

		err := healthcheck.Check(t.Context(), &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}}, server.URL+"/healthz")

		require.Error(t, err, "status %d", status)
	}
}

func TestCheckFailsWhenNothingListens(t *testing.T) {
	server := serving(t, http.StatusOK)
	address := server.URL
	server.Close()

	require.Error(t, healthcheck.Check(t.Context(), server.Client(), address+"/healthz"))
}

func TestCheckFailsWhenTheServerIsTooSlow(t *testing.T) {
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-release
	}))

	t.Cleanup(func() {
		close(release)
		slow.Close()
	})

	client := &http.Client{Timeout: 50 * time.Millisecond}

	require.Error(t, healthcheck.Check(t.Context(), client, slow.URL+"/healthz"))
}

func TestCheckFailsOnAnInvalidURL(t *testing.T) {
	require.Error(t, healthcheck.Check(t.Context(), http.DefaultClient, "http://127.0.0.1:bad/healthz"))
}

func TestRunExitsZeroWhenHealthy(t *testing.T) {
	require.Equal(t, 0, healthcheck.Run(portOf(t, serving(t, http.StatusOK))))
}

func TestRunExitsOneWhenUnhealthy(t *testing.T) {
	require.Equal(t, 1, healthcheck.Run(portOf(t, serving(t, http.StatusServiceUnavailable))))
}

func TestRunExitsOneWhenNothingListens(t *testing.T) {
	server := serving(t, http.StatusOK)
	port := portOf(t, server)
	server.Close()

	require.Equal(t, 1, healthcheck.Run(port))
}
