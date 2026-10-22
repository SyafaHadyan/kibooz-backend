package e2e

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// A teacher who gets a child by a move must not read what the parent applied and wrote while the child was in another class
func TestTheGuidanceReportsOfAClassOnlyShowWhatWasAppliedSinceTheChildJoined(t *testing.T) {
	teacherA := registerGuru(t, "Laporan A")
	classA, codeA := classOf(t, teacherA)
	teacherB := registerGuru(t, "Laporan B")
	classB, codeB := classOf(t, teacherB)

	parent, childID := registerWali(t, codeA, "Anak Laporan")

	apply := func(notes string) {
		res := call(t, http.MethodPost, "/api/v1/wali/guidance/apply", parent.Token, map[string]any{
			"guidanceId": "guidance-bingung-4step", "studentId": childID, "parentNotes": notes,
		})
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
	}

	total := func(token string, classID string) any {
		res := call(t, http.MethodGet, "/api/v1/classes/"+classID+"/guidance-reports", token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		return res.data("total")
	}

	apply("Written in the first class.")
	require.EqualValues(t, 1, total(teacherA.Token, classA), "the teacher of the first class reads it")

	moved := requestMove(t, parent.Token, childID, codeB)
	require.Equal(t, http.StatusCreated, moved.Status, "body %v", moved.Body)

	accepted := call(t, http.MethodPost, "/api/v1/guru/classes/"+classB+"/move-requests/"+moved.data("id").(string)+"/accept", teacherB.Token, nil)
	require.Equal(t, http.StatusOK, accepted.Status, "body %v", accepted.Body)

	require.Zero(t, total(teacherB.Token, classB), "the new teacher does not read what was written before the move")

	apply("Written in the second class.")
	require.EqualValues(t, 1, total(teacherB.Token, classB), "only the application since the move")
}
