package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

func ok(context.Context) error   { return nil }
func down(context.Context) error { return errors.New("down") }

func probe(t *testing.T, pingDB pinger, pingRedis pinger, storage bool) (int, map[string]any) {
	t.Helper()

	app := fiber.New()
	app.Get("/healthz", healthHandler(pingDB, pingRedis, storage, "test"))

	res, err := app.Test(httptest.NewRequest(http.MethodGet, "/healthz", nil))
	require.NoError(t, err)

	defer func() { _ = res.Body.Close() }()

	var body map[string]any

	require.NoError(t, json.NewDecoder(res.Body).Decode(&body))

	return res.StatusCode, body
}

func checks(body map[string]any) map[string]any {
	return body["checks"].(map[string]any)
}

func TestHealthEverythingUp(t *testing.T) {
	status, body := probe(t, ok, ok, true)

	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "ok", body["status"])
	require.Equal(t, "test", body["version"])
	require.Equal(t, map[string]any{"database": "ok", "redis": "ok", "storage": "ok"}, checks(body))
}

func TestHealthReportsDisabledStorageWithoutChangingStatus(t *testing.T) {
	status, body := probe(t, ok, ok, false)

	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "ok", body["status"])
	require.Equal(t, "disabled", checks(body)["storage"])
}

func TestHealthRedisDownIsDegraded(t *testing.T) {
	status, body := probe(t, ok, down, true)

	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "degraded", body["status"])
	require.Equal(t, "down", checks(body)["redis"])
}

func TestHealthDatabaseDownFails(t *testing.T) {
	status, body := probe(t, down, ok, true)

	require.Equal(t, http.StatusServiceUnavailable, status)
	require.Equal(t, false, body["success"])
	require.Equal(t, "down", body["status"])
	require.Equal(t, "down", checks(body)["database"])

	// a database outage stays "down" even when Redis is also gone
	_, both := probe(t, down, down, false)
	require.Equal(t, "down", both["status"])
}
