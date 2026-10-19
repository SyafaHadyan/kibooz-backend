package e2e

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// the health check has a connection of its own, so a busy API still tells the monitor that the database is up
func TestTheHealthCheckDoesNotQueueBehindAFullPool(t *testing.T) {
	app(t)

	sqlDB, err := testDB.DB()
	require.NoError(t, err)

	// every connection of the pool that the requests use is held by a transaction
	for range 25 {
		tx := testDB.Begin()
		require.NoError(t, tx.Error)
		require.NoError(t, tx.Exec("SELECT 1").Error)

		t.Cleanup(func() { _ = tx.Rollback().Error })
	}

	require.EqualValues(t, 25, sqlDB.Stats().InUse, "the pool is full")

	res := call(t, http.MethodGet, "/healthz", "", nil)
	require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
	require.Equal(t, "ok", dig(res.Body, "checks", "database"))
}
