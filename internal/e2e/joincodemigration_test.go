package e2e

import (
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTheMigrationReplacesShortJoinCodes runs the SQL of the migration again on classes that still have a short code,
// which is what a database that was filled before the codes grew looks like
func TestTheMigrationReplacesShortJoinCodes(t *testing.T) {
	app(t) // makes sure the database is ready

	script, err := os.ReadFile("../infra/db/migrations/000010_longer_join_codes.up.sql")
	require.NoError(t, err)

	token := suffix()
	name := "Short code " + token
	shortCodes := []string{"S" + token[:5], "T" + token[:5], "U" + token[:3]}
	keptCode := "KEEP" + token[:4]

	for _, code := range append(shortCodes, keptCode) {
		require.NoError(t, testDB.Exec("INSERT INTO classes (join_code, name) VALUES (?, ?)", code, name).Error)
	}

	require.NoError(t, testDB.Exec(string(script)).Error)

	var kept int64
	require.NoError(t, testDB.Raw("SELECT count(*) FROM classes WHERE join_code = ?", keptCode).Scan(&kept).Error)
	require.EqualValues(t, 1, kept, "a code with eight characters stays")

	var replaced []string

	require.NoError(t, testDB.Raw(
		"SELECT join_code FROM classes WHERE name = ? AND join_code <> ? ORDER BY join_code", name, keptCode,
	).Scan(&replaced).Error)
	require.Len(t, replaced, len(shortCodes))

	valid := regexp.MustCompile(`^[A-HJ-NP-Z2-9]{8}$`)
	seen := map[string]bool{}

	for _, code := range replaced {
		require.Regexp(t, valid, code, "replacement %s", code)
		require.False(t, seen[code], "every class gets its own code")
		seen[code] = true
	}

	for _, old := range shortCodes {
		var left int64
		require.NoError(t, testDB.Raw("SELECT count(*) FROM classes WHERE join_code = ?", old).Scan(&left).Error)
		require.Zero(t, left, "the old code %s stops working", old)
	}
}
