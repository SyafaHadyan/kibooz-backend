package e2e

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// scansOf reads the number of scans of one category out of the breakdown of the trash stats
func scansOf(t *testing.T, breakdown any, trashType string) float64 {
	t.Helper()

	items, ok := breakdown.([]any)
	require.True(t, ok)

	for _, item := range items {
		if dig(item, "trashType") == trashType {
			count, ok := dig(item, "scans").(float64)
			require.True(t, ok)

			return count
		}
	}

	require.FailNow(t, "the breakdown has no entry for "+trashType)

	return 0
}

// pointsByName lists the points of every child of a leaderboard answer, the podium and the rest together
func pointsByName(t *testing.T, res result) map[string]float64 {
	t.Helper()

	points := map[string]float64{}

	for _, key := range []string{"podium", "rankings"} {
		items, ok := res.data(key).([]any)
		require.True(t, ok)

		for _, item := range items {
			value, ok := dig(item, "points").(float64)
			require.True(t, ok)

			points[dig(item, "studentName").(string)] = value
		}
	}

	return points
}

func TestLeaderboardAndTrashStatsCanBeLimitedToAPeriod(t *testing.T) {
	guru := registerGuru(t, "Melati")
	classID, joinCode := classOf(t, guru)
	amelia, ameliaID := registerWali(t, joinCode, "Amelia Period")
	farhan, farhanID := registerWali(t, joinCode, "Farhan Period")
	_, zedID := registerWali(t, joinCode, "Zed Idle")
	_, goneID := registerWali(t, joinCode, "Gone Period")

	otherGuru := registerGuru(t, "Anggrek")
	_, otherCode := classOf(t, otherGuru)
	_, otherID := registerWali(t, otherCode, "Other Class Period")

	claim := func(token string, studentID string, trashType string) {
		res := call(t, http.MethodPost, "/api/v1/trash/scan-claim", token, map[string]any{
			"studentId": studentID, "trashType": trashType, "confidenceScore": 0.9,
		})
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
	}

	claim(amelia.Token, ameliaID, "ANORGANIK")
	claim(amelia.Token, ameliaID, "ORGANIK")
	claim(farhan.Token, farhanID, "ORGANIK")

	// the scans below are dated before the children were registered, and the stats only count the scans made since a child joined its
	// class, so the children join the class 60 days ago
	require.NoError(t, testDB.Exec(
		"UPDATE enrollments SET activated_at = now() - interval '60 days' WHERE student_id IN (?::uuid, ?::uuid, ?::uuid, ?::uuid, ?::uuid)",
		ameliaID, farhanID, zedID, goneID, otherID,
	).Error)

	// the scans below are written straight into the table, so they never touch the points of the semester board
	insert := func(studentID string, trashType string, points int, when string) {
		require.NoError(t, testDB.Exec(
			"INSERT INTO trash_scans (student_id, trash_type, points_awarded, scanned_at) VALUES (?::uuid, ?::trash_category_enum, ?, "+when+")",
			studentID, trashType, points,
		).Error)
	}

	const dayStart = "date_trunc('day', now() AT TIME ZONE 'Asia/Jakarta') AT TIME ZONE 'Asia/Jakarta'"

	// 40 days back is outside every day, week and month
	insert(farhanID, "ANORGANIK", 100, "now() - interval '40 days'")
	// the start of the local day is inside it and the second before is not
	insert(zedID, "ORGANIK", 7, dayStart)
	insert(zedID, "ANORGANIK", 500, dayStart+" - interval '1 second'")
	// a child of another class and a child who was removed must not show up
	insert(otherID, "ORGANIK", 900, "now()")
	insert(goneID, "ORGANIK", 800, "now()")
	require.NoError(t, testDB.Exec("UPDATE students SET deleted_at = now() WHERE id = ?::uuid", goneID).Error)

	board := func(token string, query string) result {
		res := call(t, http.MethodGet, "/api/v1/leaderboard?classId="+classID+query, token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		return res
	}

	t.Run("the board of the day ranks by the scans of the day", func(t *testing.T) {
		res := board(guru.Token, "&period=today")

		podium, ok := res.data("podium").([]any)
		require.True(t, ok)
		require.Len(t, podium, 3)
		require.Equal(t, "Amelia Period", dig(podium[0], "studentName"))
		require.EqualValues(t, 25, dig(podium[0], "points"))
		require.EqualValues(t, 1, dig(podium[0], "rank"))
		require.Equal(t, "Farhan Period", dig(podium[1], "studentName"))
		require.EqualValues(t, 10, dig(podium[1], "points"), "the scan of 40 days ago must not count")
		require.Equal(t, "Zed Idle", dig(podium[2], "studentName"))
		require.EqualValues(t, 7, dig(podium[2], "points"), "the scan at the start of the day counts and the one a second before does not")
		require.Empty(t, res.data("rankings"), "a child of another class or a removed child must not be listed")
	})

	t.Run("the boards of the week and the month leave out the old scan and the other class", func(t *testing.T) {
		for _, period := range []string{"week", "month"} {
			points := pointsByName(t, board(guru.Token, "&period="+period))

			require.Len(t, points, 3, period)
			require.EqualValues(t, 25, points["Amelia Period"], period)
			require.EqualValues(t, 10, points["Farhan Period"], period)
			require.GreaterOrEqual(t, points["Zed Idle"], float64(7), period)
		}
	})

	t.Run("a parent sees the same board", func(t *testing.T) {
		require.Equal(t, board(guru.Token, "&period=today").Body, board(amelia.Token, "&period=today").Body)
	})

	t.Run("without a period the board of the semester stays", func(t *testing.T) {
		points := pointsByName(t, board(guru.Token, ""))

		require.Len(t, points, 3)
		require.EqualValues(t, 25, points["Amelia Period"])
		require.EqualValues(t, 10, points["Farhan Period"])
		require.EqualValues(t, 0, points["Zed Idle"], "the scans written into the table do not move the points of the semester")
	})

	t.Run("an unknown period is refused", func(t *testing.T) {
		for _, period := range []string{"year", "Today", "all"} {
			call(t, http.MethodGet, "/api/v1/leaderboard?classId="+classID+"&period="+period, guru.Token, nil).
				requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
			call(t, http.MethodGet, "/api/v1/trash/stats?period="+period, farhan.Token, nil).
				requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		}
	})

	t.Run("trash stats count only the period", func(t *testing.T) {
		all := call(t, http.MethodGet, "/api/v1/trash/stats?studentId="+farhanID, farhan.Token, nil)
		require.Equal(t, http.StatusOK, all.Status, "body %v", all.Body)
		require.EqualValues(t, 2, all.data("totalScans"))
		require.EqualValues(t, 1, scansOf(t, all.data("breakdown"), "ANORGANIK"))
		require.Nil(t, all.data("period"), "the default view has no period fields")
		require.Nil(t, all.data("periodPoints"))

		for _, period := range []string{"today", "week", "month"} {
			res := call(t, http.MethodGet, "/api/v1/trash/stats?studentId="+farhanID+"&period="+period, farhan.Token, nil)
			require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
			require.Equal(t, period, res.data("period"))
			require.EqualValues(t, 10, res.data("periodPoints"))
			require.EqualValues(t, 1, res.data("totalScans"))
			require.EqualValues(t, 1, scansOf(t, res.data("breakdown"), "ORGANIK"))
			require.EqualValues(t, 0, scansOf(t, res.data("breakdown"), "ANORGANIK"), "the old scan is outside the period")
			require.EqualValues(t, 10, res.data("totalPoints"), "the total of the child does not depend on the period")
		}
	})
}
