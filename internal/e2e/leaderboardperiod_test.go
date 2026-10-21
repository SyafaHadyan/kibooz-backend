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

func TestLeaderboardAndTrashStatsCanBeLimitedToAPeriod(t *testing.T) {
	guru := registerGuru(t, "Melati")
	classID, joinCode := classOf(t, guru)
	amelia, ameliaID := registerWali(t, joinCode, "Amelia Period")
	farhan, farhanID := registerWali(t, joinCode, "Farhan Period")
	registerWali(t, joinCode, "Zed Idle")

	claim := func(token string, studentID string, trashType string) {
		res := call(t, http.MethodPost, "/api/v1/trash/scan-claim", token, map[string]any{
			"studentId": studentID, "trashType": trashType, "confidenceScore": 0.9,
		})
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
	}

	claim(amelia.Token, ameliaID, "ANORGANIK")
	claim(amelia.Token, ameliaID, "ORGANIK")
	claim(farhan.Token, farhanID, "ORGANIK")

	// a scan from 40 days ago is outside every day, week and month, and it does not touch the points of the semester board
	require.NoError(t, testDB.Exec(
		"INSERT INTO trash_scans (student_id, trash_type, points_awarded, scanned_at) VALUES (?::uuid, 'ANORGANIK', 100, now() - interval '40 days')",
		farhanID,
	).Error)

	board := func(token string, query string) result {
		res := call(t, http.MethodGet, "/api/v1/leaderboard?classId="+classID+query, token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		return res
	}

	requireOrder := func(res result) {
		t.Helper()

		podium, ok := res.data("podium").([]any)
		require.True(t, ok)
		require.Len(t, podium, 3)

		require.Equal(t, "Amelia Period", dig(podium[0], "studentName"))
		require.EqualValues(t, 25, dig(podium[0], "points"))
		require.EqualValues(t, 1, dig(podium[0], "rank"))
		require.Equal(t, "Farhan Period", dig(podium[1], "studentName"))
		require.EqualValues(t, 10, dig(podium[1], "points"), "the scan of 40 days ago must not count")
		require.Equal(t, "Zed Idle", dig(podium[2], "studentName"), "a child without a scan is still on the board")
		require.EqualValues(t, 0, dig(podium[2], "points"))
		require.Empty(t, res.data("rankings"))
	}

	t.Run("every period ranks by the scans inside it", func(t *testing.T) {
		for _, period := range []string{"today", "week", "month"} {
			requireOrder(board(guru.Token, "&period="+period))
		}
	})

	t.Run("a parent sees the same board", func(t *testing.T) {
		teacher := board(guru.Token, "&period=today")
		parent := board(amelia.Token, "&period=today")

		require.Equal(t, teacher.Body, parent.Body)
	})

	t.Run("without a period the board of the semester stays", func(t *testing.T) {
		requireOrder(board(guru.Token, ""))
		require.Equal(t, board(guru.Token, "").Body, board(guru.Token, "&period=").Body)
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
