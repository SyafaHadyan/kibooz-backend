package e2e

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func claimScan(t *testing.T, token string, studentID string, trashType string) {
	t.Helper()

	res := call(t, http.MethodPost, "/api/v1/trash/scan-claim", token, map[string]any{
		"studentId": studentID, "trashType": trashType, "confidenceScore": 0.9,
	})
	require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
}

func requestMove(t *testing.T, token string, studentID string, code string) result {
	t.Helper()

	return call(t, http.MethodPost, "/api/v1/wali/children/"+studentID+"/move-requests", token, map[string]any{"classCode": code})
}

func TestAParentAsksForAMoveAndATeacherOfTheNewClassDecides(t *testing.T) {
	teacherA := registerGuru(t, "Anyelir")
	classA, codeA := classOf(t, teacherA)
	teacherB := registerGuru(t, "Bakung")
	classB, codeB := classOf(t, teacherB)

	parent, childID := registerWali(t, codeA, "Anak Pindah")
	_, stayID := registerWali(t, codeA, "Anak Tinggal")
	stranger, _ := registerWali(t, codeB, "Anak Orang Lain")

	listOf := func(token string, studentID string) []any {
		res := call(t, http.MethodGet, "/api/v1/wali/children/"+studentID+"/move-requests", token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		requests, ok := res.data("requests").([]any)
		require.True(t, ok)

		return requests
	}

	waiting := func(token string, classID string) []any {
		res := call(t, http.MethodGet, "/api/v1/guru/classes/"+classID+"/move-requests", token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		requests, ok := res.data("requests").([]any)
		require.True(t, ok)

		return requests
	}

	decide := func(token string, classID string, requestID string, verb string) result {
		return call(t, http.MethodPost, "/api/v1/guru/classes/"+classID+"/move-requests/"+requestID+"/"+verb, token, nil)
	}

	t.Run("a request is refused for the reasons that make it pointless", func(t *testing.T) {
		requestMove(t, parent.Token, childID, "ZZZZ9999").requireError(t, http.StatusNotFound, "CLASS_CODE_NOT_FOUND")
		requestMove(t, parent.Token, childID, codeA).requireError(t, http.StatusConflict, "ALREADY_IN_CLASS")
		requestMove(t, stranger.Token, childID, codeB).requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")
		requestMove(t, parent.Token, uuid.NewString(), codeB).requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")
		requestMove(t, parent.Token, "not-a-uuid", codeB).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		requestMove(t, teacherA.Token, childID, codeB).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		requestMove(t, "", childID, codeB).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")

		bad := call(t, http.MethodPost, "/api/v1/wali/children/"+childID+"/move-requests", parent.Token, map[string]any{"classCode": ""})
		require.Equal(t, http.StatusBadRequest, bad.Status, "body %v", bad.Body)

		require.Empty(t, listOf(parent.Token, childID))
	})

	t.Run("a parent can withdraw a request and a withdrawn request cannot be decided", func(t *testing.T) {
		res := requestMove(t, parent.Token, childID, codeB)
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

		requestID := res.data("id").(string)

		require.Len(t, listOf(parent.Token, childID), 1)
		require.Len(t, waiting(teacherB.Token, classB), 1)

		cancel := func(token string, id string) result {
			return call(t, http.MethodDelete, "/api/v1/wali/children/"+childID+"/move-requests/"+id, token, nil)
		}

		cancel(stranger.Token, requestID).requireError(t, http.StatusNotFound, "MOVE_REQUEST_NOT_FOUND")
		cancel(parent.Token, uuid.NewString()).requireError(t, http.StatusNotFound, "MOVE_REQUEST_NOT_FOUND")
		require.Equal(t, http.StatusOK, cancel(parent.Token, requestID).Status)
		cancel(parent.Token, requestID).requireError(t, http.StatusNotFound, "MOVE_REQUEST_NOT_FOUND")

		require.Empty(t, listOf(parent.Token, childID))
		require.Empty(t, waiting(teacherB.Token, classB))
		decide(teacherB.Token, classB, requestID, "accept").requireError(t, http.StatusNotFound, "MOVE_REQUEST_NOT_FOUND")
	})

	t.Run("only one request waits and only a teacher of the new class decides", func(t *testing.T) {
		res := requestMove(t, parent.Token, childID, codeB)
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

		requestID := res.data("id").(string)

		requestMove(t, parent.Token, childID, codeB).requireError(t, http.StatusConflict, "MOVE_REQUEST_PENDING")

		list := waiting(teacherB.Token, classB)
		require.Len(t, list, 1)
		require.Equal(t, "Anak Pindah", dig(list[0], "studentName"))
		require.Equal(t, childID, dig(list[0], "studentId"))

		// the teacher of the old class has no say in the new one, and the parent has no say at all
		call(t, http.MethodGet, "/api/v1/guru/classes/"+classB+"/move-requests", teacherA.Token, nil).
			requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")
		decide(teacherA.Token, classB, requestID, "accept").requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")
		decide(parent.Token, classB, requestID, "accept").requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		decide(teacherB.Token, classB, "not-a-uuid", "accept").requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		decide(teacherB.Token, classB, uuid.NewString(), "accept").requireError(t, http.StatusNotFound, "MOVE_REQUEST_NOT_FOUND")
		decide(teacherB.Token, classA, requestID, "accept").requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")

		// a request belongs to the class that it names
		other := registerGuru(t, "Cempaka Dua")
		otherClass, _ := classOf(t, other)
		decide(other.Token, otherClass, requestID, "accept").requireError(t, http.StatusNotFound, "MOVE_REQUEST_NOT_FOUND")

		t.Run("a rejection keeps the child where it is and the request in the list", func(t *testing.T) {
			require.Equal(t, http.StatusOK, decide(teacherB.Token, classB, requestID, "reject").Status)

			decide(teacherB.Token, classB, requestID, "reject").requireError(t, http.StatusNotFound, "MOVE_REQUEST_NOT_FOUND")
			decide(teacherB.Token, classB, requestID, "accept").requireError(t, http.StatusNotFound, "MOVE_REQUEST_NOT_FOUND")

			requests := listOf(parent.Token, childID)
			require.Len(t, requests, 1)
			require.Equal(t, "rejected", dig(requests[0], "status"))
			require.NotNil(t, dig(requests[0], "decidedAt"))
			require.Empty(t, waiting(teacherB.Token, classB))

			dashboard := call(t, http.MethodGet, "/api/v1/wali/dashboard?studentId="+childID, parent.Token, nil)
			require.Equal(t, classA, dashboard.data("student", "classId"))
		})
	})

	t.Run("closing joining rejects what waits and a closed class takes no request", func(t *testing.T) {
		res := requestMove(t, parent.Token, childID, codeB)
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

		closeJoining := call(t, http.MethodPut, "/api/v1/guru/classes/"+classB+"/joining", teacherB.Token, map[string]any{"joinOpen": false})
		require.Equal(t, http.StatusOK, closeJoining.Status, "body %v", closeJoining.Body)

		require.Empty(t, waiting(teacherB.Token, classB))
		decide(teacherB.Token, classB, res.data("id").(string), "accept").requireError(t, http.StatusNotFound, "MOVE_REQUEST_NOT_FOUND")
		requestMove(t, parent.Token, childID, codeB).requireError(t, http.StatusConflict, "CLASS_JOINING_CLOSED")

		reopen := call(t, http.MethodPut, "/api/v1/guru/classes/"+classB+"/joining", teacherB.Token, map[string]any{"joinOpen": true})
		require.Equal(t, http.StatusOK, reopen.Status, "body %v", reopen.Body)
	})

	t.Run("an accepted move starts the child from zero in the new class and saves what it had", func(t *testing.T) {
		claimScan(t, parent.Token, childID, "ORGANIK")
		claimScan(t, parent.Token, childID, "ANORGANIK")

		before := call(t, http.MethodGet, "/api/v1/wali/dashboard?studentId="+childID, parent.Token, nil)
		require.EqualValues(t, 25, before.data("pointsSummary", "totalPoints"))
		require.EqualValues(t, 1, before.data("pointsSummary", "classRank"))

		boardA := call(t, http.MethodGet, "/api/v1/leaderboard?classId="+classA, teacherA.Token, nil)
		require.EqualValues(t, 25, pointsByName(t, boardA)["Anak Pindah"])

		res := requestMove(t, parent.Token, childID, codeB)
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

		requestID := res.data("id").(string)

		require.Equal(t, http.StatusOK, decide(teacherB.Token, classB, requestID, "accept").Status)
		decide(teacherB.Token, classB, requestID, "accept").requireError(t, http.StatusNotFound, "MOVE_REQUEST_NOT_FOUND")

		after := call(t, http.MethodGet, "/api/v1/wali/dashboard?studentId="+childID, parent.Token, nil)
		require.Equal(t, classB, after.data("student", "classId"))
		require.EqualValues(t, 0, after.data("pointsSummary", "totalPoints"))
		require.EqualValues(t, 0, after.data("pointsSummary", "seasonPoints"))
		require.EqualValues(t, 1, after.data("pointsSummary", "classRank"))
		require.EqualValues(t, 0, after.data("pointsSummary", "organicCount"))

		stats := call(t, http.MethodGet, "/api/v1/trash/stats?studentId="+childID, parent.Token, nil)
		require.Equal(t, http.StatusOK, stats.Status, "body %v", stats.Body)
		require.EqualValues(t, 0, stats.data("totalScans"))
		require.EqualValues(t, 0, scansOf(t, stats.data("breakdown"), "ORGANIK"))

		// the old class lost the child and renumbered, the new class has it with nothing
		oldBoard := pointsByName(t, call(t, http.MethodGet, "/api/v1/leaderboard?classId="+classA, teacherA.Token, nil))
		require.NotContains(t, oldBoard, "Anak Pindah")
		require.Contains(t, oldBoard, "Anak Tinggal")

		newBoard := pointsByName(t, call(t, http.MethodGet, "/api/v1/leaderboard?classId="+classB, teacherB.Token, nil))
		require.EqualValues(t, 0, newBoard["Anak Pindah"])

		for _, period := range []string{"today", "week", "month"} {
			board := pointsByName(t, call(t, http.MethodGet, "/api/v1/leaderboard?classId="+classB+"&period="+period, teacherB.Token, nil))
			require.EqualValues(t, 0, board["Anak Pindah"], "the scans of the old class do not count in the %s board", period)
		}

		rows := enrollmentsOf(t, childID)
		require.Len(t, rows, 4, "two rejected requests, the ended enrollment and the active one, a cancelled request is gone")

		var ended, active *enrollmentRow

		for i := range rows {
			switch rows[i].Status {
			case "ended":
				ended = &rows[i]
			case "active":
				active = &rows[i]
			}
		}

		require.NotNil(t, ended)
		require.Equal(t, classA, ended.ClassID)
		require.Equal(t, 25, *ended.FinalPoints)
		require.Equal(t, 25, *ended.FinalSeasonPoints)
		require.Equal(t, 1, *ended.FinalRank)
		require.Equal(t, 2, *ended.FinalScans)

		require.NotNil(t, active)
		require.Equal(t, classB, active.ClassID)
		require.Equal(t, requestID, active.ID)

		// a scan in the new class counts there and only there
		claimScan(t, parent.Token, childID, "ORGANIK")

		counted := call(t, http.MethodGet, "/api/v1/trash/stats?studentId="+childID, parent.Token, nil)
		require.EqualValues(t, 1, counted.data("totalScans"))
		require.EqualValues(t, 10, counted.data("totalPoints"))

		board := pointsByName(t, call(t, http.MethodGet, "/api/v1/leaderboard?classId="+classB+"&period=today", teacherB.Token, nil))
		require.EqualValues(t, 10, board["Anak Pindah"], "only the scan made in this class is in the board of the day")

		require.NotZero(t, stayID)
	})

	t.Run("deleting the child later saves only the scans of the class it was in", func(t *testing.T) {
		res := call(t, http.MethodDelete, "/api/v1/wali/children/"+childID, parent.Token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		var ended []enrollmentRow

		for _, row := range enrollmentsOf(t, childID) {
			if row.Status == "ended" && row.ClassID == classB {
				ended = append(ended, row)
			}
		}

		require.Len(t, ended, 1)
		require.Equal(t, 10, *ended[0].FinalPoints)
		require.Equal(t, 1, *ended[0].FinalScans, "the two scans of the first class are not counted again")
	})
}
