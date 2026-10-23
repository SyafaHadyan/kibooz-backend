package db

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
)

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

// TestMigrationsRollBackAndApplyAgain runs the migrations on a database of its own. It goes through them one by one, and each
// down file has to bring the columns and indexes back to exactly what they were before its up file. Then it runs every one of
// them down, which has to leave no table, and up again, which has to give the same result as the first time. The database is
// empty, so a down file that only fails when rows exist, such as one that refuses to drop a column with data, is not caught. It
// needs the same DB_ settings as the end to end tests and a role that may create a database. It is skipped without them, and
// fails in GitHub Actions, so a job that lost its settings cannot pass by skipping.
func TestMigrationsRollBackAndApplyAgain(t *testing.T) {
	host := os.Getenv("DB_HOST")
	if host == "" {
		if os.Getenv("GITHUB_ACTIONS") == "true" {
			t.Fatal("DB_HOST is not set in GitHub Actions, so the migrations were not run")
		}

		t.Skip("DB_HOST is not set, so there is no PostgreSQL to run the migrations on")
	}

	if !strings.HasSuffix(os.Getenv("DB_NAME"), "_test") {
		t.Skip("DB_NAME does not end in _test, so this server is not one that tests may create databases on")
	}

	portText := os.Getenv("DB_PORT")
	if portText == "" {
		portText = "5432"
	}

	port, err := strconv.ParseUint(portText, 10, 16)
	require.NoError(t, err, "DB_PORT")

	cfg := &env.Env{
		DBHost: host, DBPort: uint(port), DBName: os.Getenv("DB_NAME"), DBUsername: os.Getenv("DB_USERNAME"),
		DBPassword: os.Getenv("DB_PASSWORD"), DBSSLMode: "disable",
	}

	admin, err := sql.Open("pgx", buildDSN(cfg))
	require.NoError(t, err)

	t.Cleanup(func() { _ = admin.Close() })

	// a name of its own for each run, so two runs on one server never drop each other's database
	roundTripDatabase := fmt.Sprintf("kibooz_rt_%d_test", time.Now().UnixNano())

	_, err = admin.Exec("CREATE DATABASE " + roundTripDatabase)
	require.NoError(t, err)

	t.Cleanup(func() { _, _ = admin.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", roundTripDatabase)) })

	own := *cfg
	own.DBName = roundTripDatabase

	sqlDB, err := sql.Open("pgx", buildDSN(&own))
	require.NoError(t, err)

	t.Cleanup(func() { _ = sqlDB.Close() })

	source, err := iofs.New(migrationFiles, "migrations")
	require.NoError(t, err)

	driver, err := migratepgx.WithInstance(sqlDB, &migratepgx.Config{})
	require.NoError(t, err)

	migrator, err := migrate.NewWithInstance("iofs", source, "pgx5", driver)
	require.NoError(t, err)

	entries, err := migrationFiles.ReadDir("migrations")
	require.NoError(t, err)

	count := 0

	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".up.sql") {
			count++
		}
	}

	require.NotZero(t, count)

	for number := 1; number <= count; number++ {
		before := shape(t, sqlDB)

		require.NoError(t, migrator.Steps(1), "running migration %d up", number)
		require.NoError(t, migrator.Steps(-1), "running migration %d down", number)
		require.Equal(t, before, shape(t, sqlDB), "the down file of migration %d does not bring the columns and indexes back to what they were", number)
		require.NoError(t, migrator.Steps(1), "running migration %d up again", number)
	}

	first := shape(t, sqlDB)
	require.NotEmpty(t, first)

	require.NoError(t, migrator.Down(), "running every migration down")
	require.Empty(t, tablesLeft(t, sqlDB), "a down file left a table behind")

	require.NoError(t, Migrate(sqlDB), "running up again after every migration was rolled back")
	require.Equal(t, first, shape(t, sqlDB), "the columns and indexes after the second run up differ from the first")
}
