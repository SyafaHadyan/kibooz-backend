package db

import (
	"fmt"
	"regexp"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

var migrationName = regexp.MustCompile(`^(\d{6})_[a-z0-9_]+\.(up|down)\.sql$`)

// Two branches that each add the next migration end up with the same number, and the one that merges second would
// apply on top of the other only by luck. This fails the pull request that has to be renumbered.
func TestMigrationNumbersAreUniqueAndWithoutGaps(t *testing.T) {
	entries, err := migrationFiles.ReadDir("migrations")
	require.NoError(t, err)

	directions := map[int]map[string]string{}

	for _, entry := range entries {
		match := migrationName.FindStringSubmatch(entry.Name())
		require.NotNil(t, match, "%s must be named 000001_what_it_does.up.sql or .down.sql", entry.Name())

		number, err := strconv.Atoi(match[1])
		require.NoError(t, err)

		if directions[number] == nil {
			directions[number] = map[string]string{}
		}

		previous, taken := directions[number][match[2]]
		require.False(t, taken, "%s and %s are both the %s migration number %06d", previous, entry.Name(), match[2], number)

		directions[number][match[2]] = entry.Name()
	}

	require.NotEmpty(t, directions)

	for number := 1; number <= len(directions); number++ {
		pair, found := directions[number]
		require.True(t, found, "migration number %06d is missing, numbers must run from 000001 without gaps", number)
		require.Contains(t, pair, "up", fmt.Sprintf("migration %06d has no up file", number))
		require.Contains(t, pair, "down", fmt.Sprintf("migration %06d has no down file", number))
	}
}
