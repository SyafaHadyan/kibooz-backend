package e2e

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type enrollmentRow struct {
	ID                string
	ClassID           string
	Status            string
	ActivatedAt       *string
	EndedAt           *string
	FinalPoints       *int
	FinalSeasonPoints *int
	FinalRank         *int
	FinalScans        *int
}

func enrollmentsOf(t *testing.T, studentID string) []enrollmentRow {
	t.Helper()

	var rows []enrollmentRow

	require.NoError(t, testDB.Raw(
		"SELECT id::text AS id, class_id::text AS class_id, status, activated_at::text AS activated_at, ended_at::text AS ended_at, "+
			"final_points, final_season_points, final_rank, final_scans FROM enrollments WHERE student_id = ? ORDER BY requested_at, id",
		studentID,
	).Scan(&rows).Error)

	return rows
}

func TestEveryChildStartsWithOneActiveEnrollmentInItsClass(t *testing.T) {
	guru := registerGuru(t, "Cempaka")
	classID, joinCode := classOf(t, guru)

	parent, firstID := registerWali(t, joinCode, "Anak Pertama")

	secondName := "Anak Kedua " + suffix()
	added := addChildTo(t, parent.Token, joinCode, secondName)
	require.Equal(t, http.StatusCreated, added.Status, "body %v", added.Body)

	var secondID string

	require.NoError(t, testDB.Raw("SELECT id::text FROM students WHERE full_name = ?", secondName).Scan(&secondID).Error)
	require.NotEmpty(t, secondID)

	for _, id := range []string{firstID, secondID} {
		rows := enrollmentsOf(t, id)
		require.Len(t, rows, 1)
		require.Equal(t, "active", rows[0].Status)
		require.Equal(t, classID, rows[0].ClassID)
		require.NotNil(t, rows[0].ActivatedAt)
		require.Nil(t, rows[0].EndedAt)
		require.Nil(t, rows[0].FinalPoints)
	}

	t.Run("a child cannot have two active enrollments or two requests that wait", func(t *testing.T) {
		other, _ := classOf(t, registerGuru(t, "Dahlia"))

		active := testDB.Exec(
			"INSERT INTO enrollments (student_id, class_id, status, activated_at) VALUES (?, ?, 'active', now())", firstID, other,
		).Error
		require.ErrorContains(t, active, "enrollments_active_child_key")

		require.NoError(t, testDB.Exec("INSERT INTO enrollments (student_id, class_id, status) VALUES (?, ?, 'pending')", firstID, other).Error)

		second := testDB.Exec("INSERT INTO enrollments (student_id, class_id, status) VALUES (?, ?, 'pending')", firstID, other).Error
		require.ErrorContains(t, second, "enrollments_pending_child_key")

		require.NoError(t, testDB.Exec("DELETE FROM enrollments WHERE student_id = ? AND status = 'pending'", firstID).Error)
	})

	t.Run("the shape of a row is checked", func(t *testing.T) {
		other, _ := classOf(t, registerGuru(t, "Edelweis"))

		for statement, constraint := range map[string]string{
			"INSERT INTO enrollments (student_id, class_id, status) VALUES (?, ?, 'active')":                                      "enrollments_activated_check",
			"INSERT INTO enrollments (student_id, class_id, status, activated_at, ended_at) VALUES (?, ?, 'ended', now(), now())": "enrollments_ended_check",
			"INSERT INTO enrollments (student_id, class_id, status, final_points, final_season_points, final_rank, final_scans, ended_at) " +
				"VALUES (?, ?, 'pending', 1, 1, 1, 1, now())": "enrollments_final_only_when_ended_check",
			"INSERT INTO enrollments (student_id, class_id, status) VALUES (?, ?, 'maybe')": "enrollments_status_check",
		} {
			require.ErrorContains(t, testDB.Exec(statement, secondID, other).Error, constraint)
		}
	})
}

func TestDeletingAChildEndsItsEnrollmentWithWhatItHad(t *testing.T) {
	guru := registerGuru(t, "Flamboyan")
	classID, joinCode := classOf(t, guru)
	parent, childID := registerWali(t, joinCode, "Anak Berpoin")
	_, otherID := registerWali(t, joinCode, "Anak Lain")

	claim := call(t, http.MethodPost, "/api/v1/trash/scan-claim", parent.Token, map[string]any{
		"studentId": childID, "trashType": "ORGANIK", "confidenceScore": 0.8, "photoBase64": "data:image/png;base64," + tinyPNG,
	})
	require.Equal(t, http.StatusOK, claim.Status, "body %v", claim.Body)

	// the sweep test counts every key in the queue, so what this test queues must not outlive it
	t.Cleanup(func() {
		_ = testDB.Exec("DELETE FROM file_cleanups WHERE object_key LIKE ?", "trash-scans/"+childID+"/%").Error
	})

	var before struct {
		CurrentPoints int
		SeasonPoints  int
		RankPosition  int
	}

	require.NoError(t, testDB.Raw(
		"SELECT current_points, season_points, rank_position FROM students WHERE id = ?", childID,
	).Scan(&before).Error)
	require.Positive(t, before.CurrentPoints)

	// a request that waits is dropped together with the child
	require.NoError(t, testDB.Exec(
		"INSERT INTO enrollments (student_id, class_id, status) VALUES (?, ?, 'pending')", childID, classIDOf(t, registerGuru(t, "Gardenia")),
	).Error)

	res := call(t, http.MethodDelete, "/api/v1/wali/children/"+childID, parent.Token, nil)
	require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

	rows := enrollmentsOf(t, childID)
	require.Len(t, rows, 1, "the request that waited is gone")
	require.Equal(t, "ended", rows[0].Status)
	require.Equal(t, classID, rows[0].ClassID)
	require.NotNil(t, rows[0].EndedAt)
	require.Equal(t, before.CurrentPoints, *rows[0].FinalPoints)
	require.Equal(t, before.SeasonPoints, *rows[0].FinalSeasonPoints)
	require.Equal(t, before.RankPosition, *rows[0].FinalRank, "the rank it had before the class was renumbered")
	require.Equal(t, 1, *rows[0].FinalScans)

	require.Len(t, enrollmentsOf(t, otherID), 1)
	require.Equal(t, "active", enrollmentsOf(t, otherID)[0].Status)
}

// TestTheMigrationFillsTheEnrollmentsOfExistingChildren runs the backfill of the migration again on children that lost their enrollment,
// which is what a database that was filled before the table existed looks like
func TestTheMigrationFillsTheEnrollmentsOfExistingChildren(t *testing.T) {
	app(t) // makes sure the database is ready

	script, err := os.ReadFile("../infra/db/migrations/000021_enrollments.up.sql")
	require.NoError(t, err)

	_, backfill, found := strings.Cut(string(script), "-- backfill\n")
	require.True(t, found, "the migration marks where the backfill starts")

	guru := registerGuru(t, "Hortensia")
	classID, joinCode := classOf(t, guru)
	_, liveID := registerWali(t, joinCode, "Anak Masih Ada")
	goneParent, goneID := registerWali(t, joinCode, "Anak Dihapus")

	require.NoError(t, testDB.Exec("UPDATE students SET current_points = 40, season_points = 25, rank_position = 2 WHERE id = ?", goneID).Error)
	require.Equal(t, http.StatusOK, call(t, http.MethodDelete, "/api/v1/wali/children/"+goneID, goneParent.Token, nil).Status)

	require.NoError(t, testDB.Exec("DELETE FROM enrollments WHERE student_id IN (?, ?)", liveID, goneID).Error)
	// the plain connection pool sends the script as it is, while the GORM handle prepares a statement and refuses two of them in one
	sqlDB, err := testDB.DB()
	require.NoError(t, err)

	_, err = sqlDB.ExecContext(t.Context(), backfill)
	require.NoError(t, err)

	live := enrollmentsOf(t, liveID)
	require.Len(t, live, 1)
	require.Equal(t, "active", live[0].Status)
	require.Equal(t, classID, live[0].ClassID)

	gone := enrollmentsOf(t, goneID)
	require.Len(t, gone, 1)
	require.Equal(t, "ended", gone[0].Status)
	require.NotNil(t, gone[0].EndedAt)
	require.Equal(t, 40, *gone[0].FinalPoints)
	require.Equal(t, 25, *gone[0].FinalSeasonPoints)
	require.Equal(t, 2, *gone[0].FinalRank)
	require.Zero(t, *gone[0].FinalScans)

	_, err = sqlDB.ExecContext(t.Context(), backfill)
	require.NoError(t, err, "a second run changes nothing")
	require.Len(t, enrollmentsOf(t, liveID), 1)
	require.Len(t, enrollmentsOf(t, goneID), 1)

	var children, withoutEnrollment int64

	require.NoError(t, testDB.Raw("SELECT count(*) FROM students").Scan(&children).Error)
	require.NoError(t, testDB.Raw(
		"SELECT count(*) FROM students s WHERE NOT EXISTS (SELECT 1 FROM enrollments e WHERE e.student_id = s.id)",
	).Scan(&withoutEnrollment).Error)
	require.Positive(t, children)
	require.Zero(t, withoutEnrollment, "every child has at least one enrollment")
}

func classIDOf(t *testing.T, guru account) string {
	t.Helper()

	id, _ := classOf(t, guru)

	return id
}
