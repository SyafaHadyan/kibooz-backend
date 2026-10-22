package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/season"
)

// recordingCache notes which keys the job dropped
type recordingCache struct{ deleted []string }

func (c *recordingCache) Set(context.Context, string, string, time.Duration) error { return nil }

func (c *recordingCache) Get(context.Context, string) (string, bool, error) { return "", false, nil }

func (c *recordingCache) Del(_ context.Context, keys ...string) error {
	c.deleted = append(c.deleted, keys...)

	return nil
}

func (c *recordingCache) MarkUsed(context.Context, string, time.Duration) (bool, error) {
	return false, nil
}

func (c *recordingCache) Available() bool { return true }

func (c *recordingCache) Ping(context.Context) error { return nil }

func TestLeaderboardStartsOverWithANewSemester(t *testing.T) {
	guru := registerGuru(t, "Jasmine")
	classID, joinCode := classOf(t, guru)
	amelia, ameliaID := registerWali(t, joinCode, "Amelia Siti")
	_, zahraID := registerWali(t, joinCode, "Zahra Putri")

	// a second class, which the tests below treat as one that the semester change has not reached yet
	other := registerGuru(t, "Kenanga")
	otherClassID, otherCode := classOf(t, other)
	_, bungaID := registerWali(t, otherCode, "Bunga Citra")

	// the claim adds to the points of all semesters and to the ones of this semester
	claim := call(t, http.MethodPost, "/api/v1/trash/scan-claim", amelia.Token, map[string]any{
		"studentId": ameliaID, "trashType": "ANORGANIK", "confidenceScore": 0.9,
	})
	require.Equal(t, http.StatusOK, claim.Status, "body %v", claim.Body)
	require.EqualValues(t, 15, claim.data("seasonPoints"))
	require.EqualValues(t, 15, claim.data("totalPoints"))

	require.NoError(t, testDB.Exec("UPDATE students SET season_points = 30, current_points = 120 WHERE id = ?::uuid", zahraID).Error)

	// the job runs inside a transaction that is rolled back, so no other test sees the reset of every class
	tx := testDB.Begin()
	require.NoError(t, tx.Error)

	t.Cleanup(func() { _ = tx.Rollback().Error })

	cache := &recordingCache{}
	clock := time.Date(2030, 2, 10, 9, 0, 0, 0, time.UTC)
	job := season.New(tx, cache, time.UTC, []int{1, 7}).WithClock(func() time.Time { return clock })

	// the first semester of the run is only recorded when the table was empty, or started when another label was last
	_, err := job.Run(context.Background())
	require.NoError(t, err)

	// the points of the leaderboard are set after that, so the test does not depend on what the first run did
	require.NoError(t, tx.Exec("UPDATE students SET season_points = 15 WHERE id = ?::uuid", ameliaID).Error)
	require.NoError(t, tx.Exec("UPDATE students SET season_points = 30 WHERE id = ?::uuid", zahraID).Error)

	t.Run("a run in the same semester changes nothing", func(t *testing.T) {
		reset, err := job.Run(context.Background())
		require.NoError(t, err)
		require.False(t, reset)

		var points int
		require.NoError(t, tx.Raw("SELECT season_points FROM students WHERE id = ?::uuid", zahraID).Scan(&points).Error)
		require.Equal(t, 30, points)
	})

	clock = time.Date(2030, 7, 1, 0, 5, 0, 0, time.UTC)

	t.Run("a new semester saves the standings and starts every class from zero", func(t *testing.T) {
		reset, err := job.Run(context.Background())
		require.NoError(t, err)
		require.True(t, reset)

		type standing struct {
			SeasonPoints  int
			CurrentPoints int
			RankPosition  int
		}

		var kids []standing

		require.NoError(t, tx.Raw(
			"SELECT season_points, current_points, rank_position FROM students WHERE id IN (?::uuid, ?::uuid) ORDER BY full_name",
			ameliaID, zahraID,
		).Scan(&kids).Error)
		require.Len(t, kids, 2)
		require.Zero(t, kids[0].SeasonPoints)
		require.Zero(t, kids[1].SeasonPoints)
		require.Equal(t, 15, kids[0].CurrentPoints, "the points of all semesters stay")
		require.Equal(t, 120, kids[1].CurrentPoints)
		require.Equal(t, 1, kids[0].RankPosition, "with equal points the ranking falls back to the name")
		require.Equal(t, 2, kids[1].RankPosition)

		type saved struct {
			StudentName string
			SeasonLabel string
			Points      int
		}

		var history []saved

		require.NoError(t, tx.Raw(
			"SELECT student_name, season_label, points FROM leaderboard_history "+
				"WHERE class_id = ?::uuid AND season_label = ? ORDER BY points DESC",
			classID, "2030-01",
		).Scan(&history).Error)
		require.Len(t, history, 2)
		require.Equal(t, saved{"Zahra Putri", "2030-01", 30}, history[0], "saved under the semester that ended")
		require.Equal(t, saved{"Amelia Siti", "2030-01", 15}, history[1])
		require.Contains(t, cache.deleted, constants.LeaderboardKeyPrefix+classID, "the cached leaderboard of the class was dropped")
	})

	t.Run("a second run in the new semester does not reset again", func(t *testing.T) {
		require.NoError(t, tx.Exec("UPDATE students SET season_points = 5 WHERE id = ?::uuid", ameliaID).Error)

		reset, err := job.Run(context.Background())
		require.NoError(t, err)
		require.False(t, reset)

		var points int
		require.NoError(t, tx.Raw("SELECT season_points FROM students WHERE id = ?::uuid", ameliaID).Scan(&points).Error)
		require.Equal(t, 5, points)
	})

	clock = time.Date(2031, 1, 5, 9, 0, 0, 0, time.UTC)

	t.Run("a class that another instance already started is left alone and the rest are started", func(t *testing.T) {
		// as if an instance had started the new semester for the first class and then stopped before it reached the others
		require.NoError(t, tx.Exec("INSERT INTO leaderboard_seasons (label) VALUES ('2031-01')").Error)
		require.NoError(t, tx.Exec("UPDATE class_seasons SET season_label = '2031-01' WHERE class_id = ?::uuid", classID).Error)
		require.NoError(t, tx.Exec("UPDATE class_seasons SET season_label = '2030-07' WHERE class_id = ?::uuid", otherClassID).Error)
		require.NoError(t, tx.Exec("UPDATE students SET season_points = 7 WHERE id = ?::uuid", ameliaID).Error)
		require.NoError(t, tx.Exec("UPDATE students SET season_points = 11 WHERE id = ?::uuid", bungaID).Error)

		reset, err := job.Run(context.Background())
		require.NoError(t, err)
		require.True(t, reset)

		var kept, cleared int
		require.NoError(t, tx.Raw("SELECT season_points FROM students WHERE id = ?::uuid", ameliaID).Scan(&kept).Error)
		require.NoError(t, tx.Raw("SELECT season_points FROM students WHERE id = ?::uuid", bungaID).Scan(&cleared).Error)
		require.Equal(t, 7, kept, "the points of the new semester of a class that was started stay")
		require.Zero(t, cleared)

		var savedOther, savedFirst int
		require.NoError(t, tx.Raw(
			"SELECT COALESCE(SUM(points), 0) FROM leaderboard_history WHERE class_id = ?::uuid AND season_label = '2030-07'", otherClassID,
		).Scan(&savedOther).Error)
		require.NoError(t, tx.Raw(
			"SELECT COUNT(*) FROM leaderboard_history WHERE class_id = ?::uuid AND season_label = '2030-07'", classID,
		).Scan(&savedFirst).Error)
		require.Contains(t, cache.deleted, constants.LeaderboardKeyPrefix+otherClassID, "the cached leaderboard of the class that was started was dropped")
		require.Equal(t, 11, savedOther, "saved under the semester that the class was in")
		require.Zero(t, savedFirst, "nothing is saved a second time for the class that was started")

		reset, err = job.Run(context.Background())
		require.NoError(t, err)
		require.False(t, reset, "every class is started now, so a repeated run has nothing left")
	})

	t.Run("a class that is created after the semester began belongs to it", func(t *testing.T) {
		var created string
		require.NoError(t, tx.Raw(
			"INSERT INTO classes (join_code, name) VALUES ('SEASON' || floor(random() * 1000000)::int, 'Kelas Baru') RETURNING id",
		).Scan(&created).Error)

		var label string
		require.NoError(t, tx.Raw("SELECT season_label FROM class_seasons WHERE class_id = ?::uuid", created).Scan(&label).Error)
		require.Equal(t, "2031-01", label)

		reset, err := job.Run(context.Background())
		require.NoError(t, err)
		require.False(t, reset)
	})

	t.Run("an instance with a clock that is behind changes nothing", func(t *testing.T) {
		// 2030-01 sorts before 2031-01, which is the latest semester, so this instance must not take any class back to it
		behind := time.Date(2030, 3, 1, 9, 0, 0, 0, time.UTC)
		late := season.New(tx, cache, time.UTC, []int{1, 7}).WithClock(func() time.Time { return behind })

		require.NoError(t, tx.Exec("UPDATE students SET season_points = 4 WHERE id = ?::uuid", ameliaID).Error)

		var before int
		require.NoError(t, tx.Raw("SELECT COUNT(*) FROM leaderboard_history").Scan(&before).Error)

		reset, err := late.Run(context.Background())
		require.NoError(t, err)
		require.False(t, reset)

		var points, after int
		require.NoError(t, tx.Raw("SELECT season_points FROM students WHERE id = ?::uuid", ameliaID).Scan(&points).Error)
		require.NoError(t, tx.Raw("SELECT COUNT(*) FROM leaderboard_history").Scan(&after).Error)
		require.Equal(t, 4, points, "the points of the semester under way stay")
		require.Equal(t, before, after, "no history is saved")

		var label string
		require.NoError(t, tx.Raw("SELECT season_label FROM class_seasons WHERE class_id = ?::uuid", classID).Scan(&label).Error)
		require.Equal(t, "2031-01", label)
	})
}
