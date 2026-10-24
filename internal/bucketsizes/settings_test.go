package bucketsizes

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func environment(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func complete() map[string]string {
	return map[string]string{
		"BUCKET_SIZES_BUCKET_NAMES": "media,chat=5GB", "BUCKET_SIZES_ENDPOINT": "https://store.example.test",
		"BUCKET_SIZES_ACCESS_KEY_ID": "id", "BUCKET_SIZES_ACCESS_KEY_SECRET": "secret",
	}
}

func TestLoadSettingsFillsTheDefaults(t *testing.T) {
	settings, err := LoadSettings(environment(complete()))
	require.NoError(t, err)

	require.Equal(t, "https://store.example.test", settings.S3.Endpoint)
	require.Equal(t, "auto", settings.S3.Region)
	require.True(t, settings.S3.PathStyle)
	require.Equal(t, time.Hour, settings.Interval)
	require.Equal(t, ":9101", settings.Address)
	require.Equal(t, 15*time.Minute, settings.ScanTimeout)
	require.Len(t, settings.Targets, 2)
}

func TestLoadSettingsReadsTheOptionalOnes(t *testing.T) {
	values := complete()
	values["BUCKET_SIZES_REGION"] = "eu-west-1"
	values["BUCKET_SIZES_INTERVAL"] = "30m"
	values["BUCKET_SIZES_SCAN_TIMEOUT"] = "45m"
	values["BUCKET_SIZES_PORT"] = "9200"
	values["BUCKET_SIZES_PATH_STYLE"] = "false"

	settings, err := LoadSettings(environment(values))
	require.NoError(t, err)

	require.Equal(t, "eu-west-1", settings.S3.Region)
	require.Equal(t, 30*time.Minute, settings.Interval)
	require.Equal(t, 45*time.Minute, settings.ScanTimeout)
	require.Equal(t, ":9200", settings.Address)
	require.False(t, settings.S3.PathStyle)
}

func TestLoadSettingsRefusesWhatCannotWork(t *testing.T) {
	for name, change := range map[string]map[string]string{
		"no buckets":       {"BUCKET_SIZES_BUCKET_NAMES": ""},
		"a bad limit":      {"BUCKET_SIZES_BUCKET_NAMES": "media=lots"},
		"no endpoint":      {"BUCKET_SIZES_ENDPOINT": " "},
		"no key id":        {"BUCKET_SIZES_ACCESS_KEY_ID": ""},
		"no key secret":    {"BUCKET_SIZES_ACCESS_KEY_SECRET": ""},
		"a short interval": {"BUCKET_SIZES_INTERVAL": "10s"},
		"a bad interval":   {"BUCKET_SIZES_INTERVAL": "hourly"},
		"a bad path style": {"BUCKET_SIZES_PATH_STYLE": "maybe"},
		"no scheme":        {"BUCKET_SIZES_ENDPOINT": "acct.r2.cloudflarestorage.com"},
		"a wrong scheme":   {"BUCKET_SIZES_ENDPOINT": "ftp://store.example.test"},
		"no host":          {"BUCKET_SIZES_ENDPOINT": "https://"},
		"a short timeout":  {"BUCKET_SIZES_SCAN_TIMEOUT": "10s"},
		"a bad port":       {"BUCKET_SIZES_PORT": "99999"},
		"a bad timeout":    {"BUCKET_SIZES_SCAN_TIMEOUT": "long"},
	} {
		values := complete()
		for key, value := range change {
			values[key] = value
		}

		_, err := LoadSettings(environment(values))
		require.Error(t, err, name)
	}
}

func TestListenPort(t *testing.T) {
	get := func(value string) func(string) string {
		return func(name string) string {
			if name == "BUCKET_SIZES_PORT" {
				return value
			}

			return ""
		}
	}

	port, err := ListenPort(get(""))
	require.NoError(t, err)
	require.Equal(t, "9101", port)

	port, err = ListenPort(get(" 9200 "))
	require.NoError(t, err)
	require.Equal(t, "9200", port)

	for _, bad := range []string{"0", "65536", "-1", "http", "9101:9102", "127.0.0.1:9101"} {
		_, err = ListenPort(get(bad))
		require.Error(t, err, bad)
	}

}

// status makes one request to the server and answers the status code
func status(t *testing.T, method string, url string) int {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), method, url, http.NoBody)
	require.NoError(t, err)

	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())

	return res.StatusCode
}

func TestTheHandlerServesMetricsAndHealth(t *testing.T) {
	registry := prometheus.NewRegistry()
	scanner := NewScanner(&fakeLister{}, []Target{{Bucket: "media"}}, time.Second, registry)
	require.NotNil(t, scanner)

	server := httptest.NewServer(Handler(registry))
	t.Cleanup(server.Close)

	require.Equal(t, http.StatusOK, status(t, http.MethodGet, server.URL+"/healthz"))
	require.Equal(t, http.StatusOK, status(t, http.MethodGet, server.URL+"/metrics"))
	require.Equal(t, http.StatusMethodNotAllowed, status(t, http.MethodPost, server.URL+"/metrics"))
	require.Equal(t, http.StatusNotFound, status(t, http.MethodGet, server.URL+"/other"))
}

func TestServeAnswersUntilTheContextEndsAndRefusesATakenAddress(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())

	taken, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	t.Cleanup(func() { _ = taken.Close() })

	require.Error(t, Serve(ctx, taken.Addr().String(), http.NotFoundHandler()), "an address that is in use stops the start")

	free, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	address := free.Addr().String()
	require.NoError(t, free.Close())

	done := make(chan error, 1)

	go func() { done <- Serve(ctx, address, Handler(prometheus.NewRegistry())) }()

	require.Eventually(t, func() bool {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+"/healthz", http.NoBody)
		if err != nil {
			return false
		}

		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}

		_ = res.Body.Close()

		return res.StatusCode == http.StatusOK
	}, 2*time.Second, 10*time.Millisecond)

	cancel()
	require.NoError(t, <-done)
}
