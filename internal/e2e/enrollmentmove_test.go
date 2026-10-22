package e2e

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/enrollment"
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

		oldMood := call(t, http.MethodPost, "/api/v1/guru/mood/log", teacherA.Token, map[string]any{
			"studentId": childID, "moodType": "SENANG", "confidenceScore": 0.9, "notes": "Suka menggambar",
		})
		require.Equal(t, http.StatusCreated, oldMood.Status, "body %v", oldMood.Body)

		before := call(t, http.MethodGet, "/api/v1/wali/dashboard?studentId="+childID, parent.Token, nil)
		require.EqualValues(t, 25, before.data("pointsSummary", "totalPoints"))
		require.EqualValues(t, 1, before.data("pointsSummary", "classRank"))

		boardA := call(t, http.MethodGet, "/api/v1/leaderboard?classId="+classA, teacherA.Token, nil)
		require.EqualValues(t, 25, pointsByName(t, boardA)["Anak Pindah"])

		res := requestMove(t, parent.Token, childID, codeB)
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

		requestID := res.data("id").(string)

		// while the request waits the child is still in the old class, so the new teacher reads nothing about it
		call(t, http.MethodGet, "/api/v1/guru/classes/"+classB+"/students/"+childID+"/history", teacherB.Token, nil).
			requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")

		require.Equal(t, http.StatusOK, decide(teacherB.Token, classB, requestID, "accept").Status)
		decide(teacherB.Token, classB, requestID, "accept").requireError(t, http.StatusNotFound, "MOVE_REQUEST_NOT_FOUND")

		after := call(t, http.MethodGet, "/api/v1/wali/dashboard?studentId="+childID, parent.Token, nil)
		require.Equal(t, classB, after.data("student", "classId"))
		require.EqualValues(t, 0, after.data("pointsSummary", "totalPoints"))
		require.EqualValues(t, 0, after.data("pointsSummary", "seasonPoints"))
		require.EqualValues(t, 2, after.data("pointsSummary", "classRank"), "with no points the other child of the class comes first by name")
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

		previous := call(t, http.MethodGet, "/api/v1/wali/children/"+childID+"/previous-classes", parent.Token, nil)
		require.Equal(t, http.StatusOK, previous.Status, "body %v", previous.Body)

		classes, ok := previous.data("classes").([]any)
		require.True(t, ok)
		require.Len(t, classes, 1, "the first class is the only one that the child left")
		require.Equal(t, classA, dig(classes[0], "classId"))
		require.EqualValues(t, 25, dig(classes[0], "totalPoints"))
		require.EqualValues(t, 25, dig(classes[0], "seasonPoints"))
		require.EqualValues(t, 1, dig(classes[0], "classRank"))
		require.EqualValues(t, 2, dig(classes[0], "totalScans"))
		require.NotEmpty(t, dig(classes[0], "className"))
		require.NotEmpty(t, dig(classes[0], "startedAt"))
		require.NotEmpty(t, dig(classes[0], "endedAt"))

		history := func(teacher account, class string, student string) result {
			return call(t, http.MethodGet, "/api/v1/guru/classes/"+class+"/students/"+student+"/history", teacher.Token, nil)
		}

		// a mood dated before the child joined this class would land in the history of the old class, so it is refused
		early := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)

		call(t, http.MethodPost, "/api/v1/guru/mood/log", teacherB.Token, map[string]any{
			"studentId": childID, "moodType": "SEDIH", "confidenceScore": 0.8, "notes": "Dicatat mundur", "recordedAt": early,
		}).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodPost, "/api/v1/guru/mood/log/batch", teacherB.Token, map[string]any{
			"classId": classB, "recordedAt": early, "entries": []map[string]any{{"studentId": childID, "moodType": "SEDIH", "confidenceScore": 0.8}},
		}).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")

		// the mood that the old teacher wrote today is not the mood of today in the new class
		roster := call(t, http.MethodGet, "/api/v1/classes/"+classB+"/students", teacherB.Token, nil)
		require.Equal(t, http.StatusOK, roster.Status, "body %v", roster.Body)

		for _, row := range roster.data("students").([]any) {
			if dig(row, "id") == childID {
				require.Nil(t, dig(row, "todayMood"), "nothing was recorded for the child in this class yet")
			}
		}

		newMood := call(t, http.MethodPost, "/api/v1/guru/mood/log", teacherB.Token, map[string]any{
			"studentId": childID, "moodType": "SEDIH", "confidenceScore": 0.8, "notes": "Hari pertama di kelas baru",
		})
		require.Equal(t, http.StatusCreated, newMood.Status, "body %v", newMood.Body)

		seen := history(teacherB, classB, childID)
		require.Equal(t, http.StatusOK, seen.Status, "body %v", seen.Body)

		earlier, ok := seen.data("previousClasses").([]any)
		require.True(t, ok)
		require.Len(t, earlier, 1)
		require.Equal(t, classA, dig(earlier[0], "classId"))
		require.EqualValues(t, 25, dig(earlier[0], "totalPoints"))

		breakdown, ok := dig(earlier[0], "breakdown").([]any)
		require.True(t, ok)
		require.Len(t, breakdown, 3)
		require.Equal(t, "ORGANIK", dig(breakdown[0], "trashType"))
		require.EqualValues(t, 1, dig(breakdown[0], "scans"))
		require.EqualValues(t, 10, dig(breakdown[0], "points"))
		require.EqualValues(t, 15, dig(breakdown[1], "points"))
		require.EqualValues(t, 0, dig(breakdown[2], "scans"))

		moods, ok := dig(earlier[0], "moods").([]any)
		require.True(t, ok)
		require.Len(t, moods, 1, "only the mood of the old class, not the one that the new teacher wrote")
		require.Equal(t, "SENANG", dig(moods[0], "moodType"))
		require.Equal(t, "Suka menggambar", dig(moods[0], "notes"))
		require.Equal(t, false, dig(earlier[0], "moodsTruncated"))

		// the teacher of the old class no longer has the child, and nobody else has a say
		history(teacherA, classA, childID).requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")
		history(teacherA, classB, childID).requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")
		history(teacherB, classB, uuid.NewString()).requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")
		history(teacherB, classB, "not-a-uuid").requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		history(teacherB, "not-a-uuid", childID).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		history(parent, classB, childID).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")

		// a child that never moved has nothing to show
		stayed := history(teacherA, classA, stayID)
		require.Equal(t, http.StatusOK, stayed.Status, "body %v", stayed.Body)

		none, ok := stayed.data("previousClasses").([]any)
		require.True(t, ok)
		require.Empty(t, none)

		// a scan in the new class counts there and only there
		claimScan(t, parent.Token, childID, "ORGANIK")

		counted := call(t, http.MethodGet, "/api/v1/trash/stats?studentId="+childID, parent.Token, nil)
		require.EqualValues(t, 1, counted.data("totalScans"))
		require.EqualValues(t, 10, counted.data("totalPoints"))

		board := pointsByName(t, call(t, http.MethodGet, "/api/v1/leaderboard?classId="+classB+"&period=today", teacherB.Token, nil))
		require.EqualValues(t, 10, board["Anak Pindah"], "only the scan made in this class is in the board of the day")

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

func TestMoveRoutesRefuseBadIdsStrangersAndClassesThatCannotTakeAChild(t *testing.T) {
	teacher := registerGuru(t, "Dandelion")
	_, joinCode := classOf(t, teacher)
	parent, childID := registerWali(t, joinCode, "Anak Ditolak")
	stranger, strangerChild := registerWali(t, joinCode, "Anak Orang Asing")

	someID := uuid.NewString()

	t.Run("an id that is not a UUID is a validation error", func(t *testing.T) {
		call(t, http.MethodGet, "/api/v1/wali/children/not-a-uuid/move-requests", parent.Token, nil).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodDelete, "/api/v1/wali/children/not-a-uuid/move-requests/"+someID, parent.Token, nil).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodDelete, "/api/v1/wali/children/"+childID+"/move-requests/not-a-uuid", parent.Token, nil).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodGet, "/api/v1/guru/classes/not-a-uuid/move-requests", teacher.Token, nil).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodPost, "/api/v1/guru/classes/not-a-uuid/move-requests/"+someID+"/reject", teacher.Token, nil).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
	})

	t.Run("the previous classes of a child need a valid id and the parent of the child", func(t *testing.T) {
		call(t, http.MethodGet, "/api/v1/wali/children/not-a-uuid/previous-classes", parent.Token, nil).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodGet, "/api/v1/wali/children/"+childID+"/previous-classes", stranger.Token, nil).
			requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")

		never := call(t, http.MethodGet, "/api/v1/wali/children/"+childID+"/previous-classes", parent.Token, nil)
		require.Equal(t, http.StatusOK, never.Status, "body %v", never.Body)

		none, ok := never.data("classes").([]any)
		require.True(t, ok, "the list is an array and not null")
		require.Empty(t, none, "a child that never moved has no previous class")

		call(t, http.MethodGet, "/api/v1/wali/children/"+childID+"/previous-classes", teacher.Token, nil).
			requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
	})

	t.Run("a parent cannot read the requests of another parent's child", func(t *testing.T) {
		call(t, http.MethodGet, "/api/v1/wali/children/"+childID+"/move-requests", stranger.Token, nil).
			requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")
	})

	t.Run("a class without an active teacher takes no request", func(t *testing.T) {
		orphan := registerGuru(t, "Edelweiss Kosong")
		orphanClass, orphanCode := classOf(t, orphan)

		require.NoError(t, testDB.Exec("UPDATE gurus SET deleted_at = now() WHERE id IN (SELECT guru_id FROM class_teachers WHERE class_id = ?::uuid)", orphanClass).Error)

		requestMove(t, parent.Token, childID, orphanCode).requireError(t, http.StatusConflict, "CLASS_NO_ACTIVE_TEACHER")
	})

	t.Run("a full class takes no request and accepts no move", func(t *testing.T) {
		fullTeacher := registerGuru(t, "Flamboyan Penuh")
		fullClass, fullCode := classOf(t, fullTeacher)
		holder, _ := registerWali(t, fullCode, "Anak Pertama Penuh")

		// the request is made while there is room
		res := requestMove(t, parent.Token, childID, fullCode)
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

		var waliID string

		require.NoError(t, testDB.Raw(
			"SELECT w.id FROM walis w JOIN users u ON u.id = w.user_id WHERE u.email = ?", holder.Email,
		).Scan(&waliID).Error)

		// the children are written straight to the table, which opens their enrollments through the trigger
		require.NoError(t, testDB.Exec(
			`INSERT INTO students (id, wali_id, class_id, nisn, full_name)
			SELECT gen_random_uuid(), ?::uuid, ?::uuid, 'mv' || substr(md5(random()::text || n::text), 1, 20), 'Anak Massal ' || n
			FROM generate_series(1, ?) AS n`,
			waliID, fullClass, constants.MaxChildrenPerClass-1,
		).Error)

		// the class filled up since, so a new request and the accept of the old one are both refused
		requestMove(t, stranger.Token, strangerChild, fullCode).requireError(t, http.StatusConflict, "CLASS_FULL")
		call(t, http.MethodPost, "/api/v1/guru/classes/"+fullClass+"/move-requests/"+res.data("id").(string)+"/accept", fullTeacher.Token, nil).
			requireError(t, http.StatusConflict, "CLASS_FULL")
	})
}

func TestAChildCanMoveBackAndForthBetweenTwoClasses(t *testing.T) {
	teacherA := registerGuru(t, "Gardenia")
	classA, codeA := classOf(t, teacherA)
	teacherB := registerGuru(t, "Hortensia Dua")
	classB, codeB := classOf(t, teacherB)
	parent, childID := registerWali(t, codeA, "Anak Bolak Balik")

	move := func(teacher account, classID string, code string) {
		res := requestMove(t, parent.Token, childID, code)
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

		accepted := call(t, http.MethodPost, "/api/v1/guru/classes/"+classID+"/move-requests/"+res.data("id").(string)+"/accept", teacher.Token, nil)
		require.Equal(t, http.StatusOK, accepted.Status, "body %v", accepted.Body)
	}

	move(teacherB, classB, codeB)
	move(teacherA, classA, codeA)

	dashboard := call(t, http.MethodGet, "/api/v1/wali/dashboard?studentId="+childID, parent.Token, nil)
	require.Equal(t, classA, dashboard.data("student", "classId"))

	rows := enrollmentsOf(t, childID)
	require.Len(t, rows, 3, "the first class, the second class and the first class again")

	statuses := map[string]int{}
	for _, row := range rows {
		statuses[row.Status]++
	}

	require.Equal(t, map[string]int{"ended": 2, "active": 1}, statuses)

	previous := call(t, http.MethodGet, "/api/v1/wali/children/"+childID+"/previous-classes", parent.Token, nil)
	require.Equal(t, http.StatusOK, previous.Status, "body %v", previous.Body)

	classes, ok := previous.data("classes").([]any)
	require.True(t, ok)
	require.Len(t, classes, 2)
	require.Equal(t, classB, dig(classes[0], "classId"), "the class that the child left last comes first")
	require.Equal(t, classA, dig(classes[1], "classId"))
	require.NotEmpty(t, dig(classes[0], "schoolName"))
	require.NotEmpty(t, dig(classes[0], "gradeLevel"))
	require.NotEmpty(t, dig(classes[0], "academicYear"))
}

// TestEnrollmentRulesHoldWhenCalledDirectly reaches the refusals of Accept that the routes only meet when two requests race, such as
// a class that was archived, a child that was deleted or a teacher who left between the check of the route and the move
func TestEnrollmentRulesHoldWhenCalledDirectly(t *testing.T) {
	teacherA := registerGuru(t, "Iris")
	classA, codeA := classOf(t, teacherA)
	teacherB := registerGuru(t, "Jasmine Dua")
	classB, _ := classOf(t, teacherB)
	_, childID := registerWali(t, codeA, "Anak Langsung")

	var guruB, guruA string

	require.NoError(t, testDB.Raw("SELECT g.id::text FROM gurus g JOIN users u ON u.id = g.user_id WHERE u.email = ?", teacherB.Email).Scan(&guruB).Error)
	require.NoError(t, testDB.Raw("SELECT g.id::text FROM gurus g JOIN users u ON u.id = g.user_id WHERE u.email = ?", teacherA.Email).Scan(&guruA).Error)

	accept := func(classID string, requestID string, guruID string) error {
		return testDB.Transaction(func(tx *gorm.DB) error {
			_, err := enrollment.Accept(tx, uuid.MustParse(classID), uuid.MustParse(requestID), uuid.MustParse(guruID))

			return err
		})
	}

	// the trigger only reacts to an update of the class, so a request written afterwards waits in a class that is closed already
	insertPending := func(studentID string, classID string) string {
		id := uuid.NewString()
		require.NoError(t, testDB.Exec(
			"INSERT INTO enrollments (id, student_id, class_id, status) VALUES (?::uuid, ?::uuid, ?::uuid, 'pending')", id, studentID, classID,
		).Error)

		return id
	}

	removePending := func(id string) {
		require.NoError(t, testDB.Exec("DELETE FROM enrollments WHERE id = ?::uuid", id).Error)
	}

	requireCode := func(err error, want *apperror.Error) {
		var got *apperror.Error

		require.ErrorAs(t, err, &got)
		require.Equal(t, want.Code, got.Code)
	}

	t.Run("a teacher that does not exist or does not teach the class cannot decide", func(t *testing.T) {
		id := insertPending(childID, classB)
		defer removePending(id)

		requireCode(accept(classB, id, uuid.NewString()), apperror.ErrClassNotFound)
		requireCode(accept(classB, id, guruA), apperror.ErrClassNotFound)
	})

	t.Run("a request that is gone, or that names another class, is not found", func(t *testing.T) {
		requireCode(accept(classB, uuid.NewString(), guruB), apperror.ErrMoveNotFound)

		id := insertPending(childID, classB)
		defer removePending(id)

		requireCode(accept(classA, id, guruA), apperror.ErrMoveNotFound)
	})

	t.Run("a child that was deleted cannot move", func(t *testing.T) {
		_, goneID := registerWali(t, codeA, "Anak Sudah Pergi")

		require.NoError(t, testDB.Exec("UPDATE students SET deleted_at = now() WHERE id = ?::uuid", goneID).Error)
		// the trigger of the deletion drops the requests that waited, so this one is a leftover of a race
		id := insertPending(goneID, classB)

		requireCode(accept(classB, id, guruB), apperror.ErrMoveNotFound)
	})

	t.Run("a child cannot move into the class it is in", func(t *testing.T) {
		id := insertPending(childID, classA)
		defer removePending(id)

		requireCode(accept(classA, id, guruA), apperror.ErrAlreadyInClass)
	})

	t.Run("an archived class decides nothing", func(t *testing.T) {
		id := insertPending(childID, classB)
		defer removePending(id)

		require.NoError(t, testDB.Exec("UPDATE classes SET archived_at = now() WHERE id = ?::uuid", classB).Error)
		// the update above rejected the request that waited, so it waits again here
		require.NoError(t, testDB.Exec("UPDATE enrollments SET status = 'pending', decided_at = NULL WHERE id = ?::uuid", id).Error)

		requireCode(accept(classB, id, guruB), apperror.ErrMoveNotFound)
	})
}
