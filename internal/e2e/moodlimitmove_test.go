package e2e

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
)

// The limit of a day is for one class, so the moods that a child got in the class that it left do not use up the room that it
// has in the new class.
func TestTheMoodLimitOfADayStartsAgainInTheNewClass(t *testing.T) {
	teacherA := registerGuru(t, "Batas Lama")
	_, codeA := classOf(t, teacherA)
	teacherB := registerGuru(t, "Batas Baru")
	classB, codeB := classOf(t, teacherB)

	parent, childID := registerWali(t, codeA, "Anak Batas Pindah")

	var guruID string

	require.NoError(t, testDB.Raw("SELECT g.id::text FROM gurus AS g JOIN users AS u ON u.id = g.user_id WHERE u.email = ?", teacherA.Email).Scan(&guruID).Error)

	// the old class used the whole day of the child, written into the table because the limit has its own test
	require.NoError(t, testDB.Exec(
		`INSERT INTO mood_logs (student_id, recorded_by_guru_id, mood_type, recorded_at)
		SELECT ?::uuid, ?::uuid, 'SENANG'::mood_enum, now() - interval '1 second' * n FROM generate_series(1, ?) AS n`,
		childID, guruID, constants.MoodLogsPerStudentPerDay,
	).Error)

	require.EqualValues(t, constants.MoodLogsPerStudentPerDay, moodLogCount(t, childID))

	asked := requestMove(t, parent.Token, childID, codeB)
	require.Equal(t, http.StatusCreated, asked.Status, "body %v", asked.Body)

	accepted := call(t, http.MethodPost, "/api/v1/guru/classes/"+classB+"/move-requests/"+asked.data("id").(string)+"/accept", teacherB.Token, nil)
	require.Equal(t, http.StatusOK, accepted.Status, "body %v", accepted.Body)

	t.Run("the new teacher can still write the moods of the day", func(t *testing.T) {
		for i := 0; i < 2; i++ {
			res := call(t, http.MethodPost, moodLogRoute, teacherB.Token, map[string]any{"studentId": childID, "moodType": "SEDIH"})
			require.Equal(t, http.StatusCreated, res.Status, "log %d, body %v", i+1, res.Body)
		}
	})
}
