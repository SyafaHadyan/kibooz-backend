package e2e

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLeaderboardHistoryShowsTheSemestersThatEnded(t *testing.T) {
	guru := registerGuru(t, "Mawar")
	classID, joinCode := classOf(t, guru)
	parent, amelia := registerWali(t, joinCode, "Amelia Siti")
	_, zahra := registerWali(t, joinCode, "Zahra Putri")
	_, kenzo := registerWali(t, joinCode, "Kenzo Aditya")
	_, rizky := registerWali(t, joinCode, "Rizky Pratama")
	_, gone := registerWali(t, joinCode, "Dimas Gone")

	save := func(label string, student string, name string, points int, rank int) {
		require.NoError(t, testDB.Exec(
			"INSERT INTO leaderboard_history (class_id, student_id, student_name, season_label, points, rank_position) "+
				"VALUES (?::uuid, ?::uuid, ?, ?, ?, ?)",
			classID, student, name, label, points, rank,
		).Error)
	}

	save("2029-01", amelia, "Amelia Siti", 40, 1)
	save("2029-07", zahra, "Zahra Putri", 90, 1)
	save("2029-07", amelia, "Amelia Siti", 80, 2)
	save("2029-07", kenzo, "Kenzo Aditya", 70, 3)
	save("2029-07", rizky, "Rizky Pratama", 60, 4)
	save("2029-07", gone, "Dimas Gone", 50, 5)

	// the account of this child is deleted after the semester ended, so its name must not outlive it
	require.NoError(t, testDB.Exec("UPDATE students SET deleted_at = now() WHERE id = ?::uuid", gone).Error)

	t.Run("a teacher sees the newest semester first with the podium and the rest", func(t *testing.T) {
		res := call(t, http.MethodGet, "/api/v1/leaderboard/history?classId="+classID, guru.Token, nil)

		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.EqualValues(t, 2, res.data("total"))
		require.EqualValues(t, 1, res.data("page"))

		seasons, ok := res.data("seasons").([]any)
		require.True(t, ok)
		require.Len(t, seasons, 2)
		require.Equal(t, "2029-07", dig(seasons[0], "label"))
		require.Equal(t, "2029-01", dig(seasons[1], "label"))

		podium, ok := dig(seasons[0], "podium").([]any)
		require.True(t, ok)
		require.Len(t, podium, 3)
		require.Equal(t, "Zahra Putri", dig(podium[0], "studentName"))
		require.EqualValues(t, 90, dig(podium[0], "points"))
		require.EqualValues(t, 3, dig(podium[2], "rank"))

		rankings, ok := dig(seasons[0], "rankings").([]any)
		require.True(t, ok)
		require.Len(t, rankings, 1, "the deleted child is left out")
		require.Equal(t, "Rizky Pratama", dig(rankings[0], "studentName"))
		require.EqualValues(t, 4, dig(rankings[0], "rank"))

		older, ok := dig(seasons[1], "rankings").([]any)
		require.True(t, ok, "an empty list and not null")
		require.Empty(t, older)
	})

	t.Run("a parent sees the class of the child without naming it", func(t *testing.T) {
		res := call(t, http.MethodGet, "/api/v1/leaderboard/history", parent.Token, nil)

		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.EqualValues(t, 2, res.data("total"))
	})

	t.Run("a page holds that many semesters", func(t *testing.T) {
		res := call(t, http.MethodGet, "/api/v1/leaderboard/history?classId="+classID+"&page=2&limit=1", guru.Token, nil)

		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.EqualValues(t, 2, res.data("total"))

		seasons, ok := res.data("seasons").([]any)
		require.True(t, ok)
		require.Len(t, seasons, 1)
		require.Equal(t, "2029-01", dig(seasons[0], "label"))

		past := call(t, http.MethodGet, "/api/v1/leaderboard/history?classId="+classID+"&page=3&limit=1", guru.Token, nil)

		require.Equal(t, http.StatusOK, past.Status)
		require.Empty(t, past.data("seasons"))
	})

	t.Run("a class without a finished semester has an empty list", func(t *testing.T) {
		fresh := registerGuru(t, "Melati")

		res := call(t, http.MethodGet, "/api/v1/leaderboard/history", fresh.Token, nil)

		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.EqualValues(t, 0, res.data("total"))
		require.Empty(t, res.data("seasons"))
	})

	t.Run("only the people of the class can read it", func(t *testing.T) {
		outsiderGuru := registerGuru(t, "Anggrek")
		_, otherCode := classOf(t, outsiderGuru)
		outsider, _ := registerWali(t, otherCode, "Putri Lain")

		call(t, http.MethodGet, "/api/v1/leaderboard/history?classId="+classID, outsiderGuru.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodGet, "/api/v1/leaderboard/history?classId="+classID, outsider.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodGet, "/api/v1/leaderboard/history", "", nil).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
	})

	t.Run("a bad query is refused", func(t *testing.T) {
		call(t, http.MethodGet, "/api/v1/leaderboard/history?classId=not-a-uuid", guru.Token, nil).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodGet, "/api/v1/leaderboard/history?limit=0", guru.Token, nil).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
	})
}
