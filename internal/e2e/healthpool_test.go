package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// the health check has a connection of its own, so a busy API still tells the monitor that the database is up
func TestTheHealthCheckDoesNotQueueBehindAFullPool(t *testing.T) {
	app(t)

	sqlDB, err := testDB.DB()
	require.NoError(t, err)

	// Every connection of the pool that the requests use is taken. A background job may hold one for a moment, so the loop
	// takes what becomes free and stops at the first wait that runs out of time, which is when the pool is full.
	for sqlDB.Stats().InUse < sqlDB.Stats().MaxOpenConnections {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		conn, err := sqlDB.Conn(ctx)

		cancel()

		if err != nil {
			break
		}

		t.Cleanup(func() { _ = conn.Close() })
	}

	stats := sqlDB.Stats()
	require.Equal(t, stats.MaxOpenConnections, stats.InUse, "the pool is full")

	res := call(t, http.MethodGet, "/healthz", "", nil)
	require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
	require.Equal(t, "ok", dig(res.Body, "checks", "database"))
}
