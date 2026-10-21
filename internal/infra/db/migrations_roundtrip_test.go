package db

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
)

const roundTripDatabase = "kibooz_migrations_roundtrip_test"

// shape lists every column and index of the public schema, so two databases can be compared
func shape(t *testing.T, sqlDB *sql.DB) []string {
	t.Helper()

	var lines []string

	columns, err := sqlDB.Query(`SELECT 'column ' || table_name || '.' || column_name || ' ' || data_type || ' ' || is_nullable || ' ' ||
		coalesce(column_default, '') FROM information_schema.columns WHERE table_schema = 'public' ORDER BY 1`)
	require.NoError(t, err)

	defer func() { _ = columns.Close() }()

	for columns.Next() {
		var line string

		require.NoError(t, columns.Scan(&line))

		lines = append(lines, line)
	}

	require.NoError(t, columns.Err())

	indexes, err := sqlDB.Query(`SELECT 'index ' || indexdef FROM pg_indexes WHERE schemaname = 'public' ORDER BY 1`)
	require.NoError(t, err)

	defer func() { _ = indexes.Close() }()

	for indexes.Next() {
		var line string

		require.NoError(t, indexes.Scan(&line))

		lines = append(lines, line)
	}

	require.NoError(t, indexes.Err())

	return lines
}

func tablesLeft(t *testing.T, sqlDB *sql.DB) []string {
	t.Helper()

	rows, err := sqlDB.Query(`SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name <> 'schema_migrations' ORDER BY 1`)
	require.NoError(t, err)

	defer func() { _ = rows.Close() }()

	var names []string

	for rows.Next() {
		var name string

		require.NoError(t, rows.Scan(&name))

		names = append(names, name)
	}

	require.NoError(t, rows.Err())

	return names
}

// TestMigrationsRollBackAndApplyAgain runs every migration up, every one of them down and every one up again on a database of
// its own. A down file that does not undo its up file, or an up file that cannot run on the result, fails here and not
// during a rollback in production. It needs the same DB_ settings as the end to end tests and is skipped without them.
func TestMigrationsRollBackAndApplyAgain(t *testing.T) {
	host := os.Getenv("DB_HOST")
	if host == "" {
		t.Skip("DB_HOST is not set, so there is no PostgreSQL to run the migrations on")
	}

	port, err := strconv.ParseUint(os.Getenv("DB_PORT"), 10, 16)
	require.NoError(t, err, "DB_PORT")

	cfg := &env.Env{
		DBHost: host, DBPort: uint(port), DBName: os.Getenv("DB_NAME"), DBUsername: os.Getenv("DB_USERNAME"),
		DBPassword: os.Getenv("DB_PASSWORD"), DBSSLMode: "disable",
	}

	admin, err := sql.Open("pgx", buildDSN(cfg))
	require.NoError(t, err)

	t.Cleanup(func() { _ = admin.Close() })

	_, err = admin.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", roundTripDatabase))
	require.NoError(t, err)

	_, err = admin.Exec("CREATE DATABASE " + roundTripDatabase)
	require.NoError(t, err)

	t.Cleanup(func() { _, _ = admin.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", roundTripDatabase)) })

	own := *cfg
	own.DBName = roundTripDatabase

	sqlDB, err := sql.Open("pgx", buildDSN(&own))
	require.NoError(t, err)

	t.Cleanup(func() { _ = sqlDB.Close() })

	require.NoError(t, Migrate(sqlDB), "the first run up")

	first := shape(t, sqlDB)
	require.NotEmpty(t, first)

	migrator, err := newMigrator(sqlDB)
	require.NoError(t, err)

	require.NoError(t, migrator.Down(), "running every migration down")
	require.Empty(t, tablesLeft(t, sqlDB), "a down file left a table behind")

	require.NoError(t, Migrate(sqlDB), "running up again after every migration was rolled back")
	require.Equal(t, first, shape(t, sqlDB), "the columns and indexes after the second run up differ from the first")
}
