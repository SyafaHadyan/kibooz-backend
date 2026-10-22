package e2e

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// An archived class is read only. Its points, forum, videos, settings and people stay as they were, everyone can still read, and
// a child can still leave it for another class.
func TestAnArchivedClassIsReadOnly(t *testing.T) {
	teacher := registerGuru(t, "Kamboja")
	classID, joinCode := classOf(t, teacher)
	parent, childID := registerWali(t, joinCode, "Anak Kelas Arsip")
	_, otherChildID := registerWali(t, joinCode, "Anak Lain Arsip")
	next := registerGuru(t, "Lavender")
	nextClass, nextCode := classOf(t, next)

	claimScan(t, parent.Token, childID, "ORGANIK")

	thread := call(t, http.MethodPost, "/api/v1/classes/"+classID+"/forum", parent.Token, map[string]any{"title": "Sebelum arsip", "body": "Halo"})
	require.Equal(t, http.StatusCreated, thread.Status, "body %v", thread.Body)

	threadID := thread.data("id").(string)

	require.NoError(t, testDB.Exec("UPDATE classes SET archived_at = now() WHERE id = ?::uuid", classID).Error)

	t.Run("nothing can be written into the class", func(t *testing.T) {
		claim := call(t, http.MethodPost, "/api/v1/trash/scan-claim", parent.Token, map[string]any{
			"studentId": childID, "trashType": "ORGANIK", "confidenceScore": 0.9,
		})
		claim.requireError(t, http.StatusConflict, "CLASS_ARCHIVED")

		call(t, http.MethodPost, "/api/v1/guru/mood/log", teacher.Token, map[string]any{
			"studentId": childID, "moodType": "SENANG", "confidenceScore": 0.9,
		}).requireError(t, http.StatusConflict, "CLASS_ARCHIVED")
		call(t, http.MethodPost, "/api/v1/guru/mood/log/batch", teacher.Token, map[string]any{
			"classId": classID, "entries": []map[string]any{{"studentId": childID, "moodType": "SENANG", "confidenceScore": 0.9}},
		}).requireError(t, http.StatusConflict, "CLASS_ARCHIVED")

		call(t, http.MethodPost, "/api/v1/classes/"+classID+"/forum", parent.Token, map[string]any{"title": "Baru", "body": "Isi"}).
			requireError(t, http.StatusConflict, "CLASS_ARCHIVED")
		call(t, http.MethodPost, "/api/v1/classes/"+classID+"/forum/"+threadID+"/replies", teacher.Token, map[string]any{"body": "Balasan"}).
			requireError(t, http.StatusConflict, "CLASS_ARCHIVED")
		call(t, http.MethodPatch, "/api/v1/classes/"+classID+"/forum/"+threadID, parent.Token, map[string]any{"body": "Ubah"}).
			requireError(t, http.StatusConflict, "CLASS_ARCHIVED")
		call(t, http.MethodDelete, "/api/v1/classes/"+classID+"/forum/"+threadID, parent.Token, nil).
			requireError(t, http.StatusConflict, "CLASS_ARCHIVED")
		call(t, http.MethodPost, "/api/v1/classes/"+classID+"/videos", teacher.Token, map[string]any{
			"title": "Video", "videoUrl": "https://example.com/video.mp4",
		}).requireError(t, http.StatusConflict, "CLASS_ARCHIVED")
	})

	t.Run("the settings and the people of the class cannot change", func(t *testing.T) {
		base := "/api/v1/guru/classes/" + classID

		call(t, http.MethodPost, base+"/join-code", teacher.Token, nil).requireError(t, http.StatusConflict, "CLASS_ARCHIVED")
		call(t, http.MethodPut, base+"/joining", teacher.Token, map[string]any{"joinOpen": false}).
			requireError(t, http.StatusConflict, "CLASS_ARCHIVED")
		call(t, http.MethodDelete, base+"/students/"+otherChildID, teacher.Token, nil).requireError(t, http.StatusConflict, "CLASS_ARCHIVED")
		call(t, http.MethodPost, base+"/teachers", teacher.Token, map[string]any{"email": next.Email}).
			requireError(t, http.StatusConflict, "CLASS_ARCHIVED")
		call(t, http.MethodDelete, base+"/teachers/"+uuid.NewString(), teacher.Token, nil).requireError(t, http.StatusConflict, "CLASS_ARCHIVED")
		call(t, http.MethodPut, base+"/owner", teacher.Token, map[string]any{"teacherId": uuid.NewString()}).
			requireError(t, http.StatusConflict, "CLASS_ARCHIVED")
	})

	t.Run("everyone can still read it", func(t *testing.T) {
		detail := call(t, http.MethodGet, "/api/v1/guru/classes/"+classID, teacher.Token, nil)
		require.Equal(t, http.StatusOK, detail.Status, "body %v", detail.Body)
		require.NotEmpty(t, detail.data("archivedAt"), "the class says that it is archived")

		list := call(t, http.MethodGet, "/api/v1/guru/classes", teacher.Token, nil)
		require.Equal(t, http.StatusOK, list.Status, "body %v", list.Body)

		classes, ok := list.data("classes").([]any)
		require.True(t, ok)

		archived := 0

		for _, item := range classes {
			if dig(item, "id") == classID {
				require.NotEmpty(t, dig(item, "archivedAt"))

				archived++
			}
		}

		require.Equal(t, 1, archived)

		board := call(t, http.MethodGet, "/api/v1/leaderboard?classId="+classID, teacher.Token, nil)
		require.Equal(t, http.StatusOK, board.Status, "body %v", board.Body)
		require.EqualValues(t, 10, pointsByName(t, board)["Anak Kelas Arsip"], "the points are frozen where they were")

		forum := call(t, http.MethodGet, "/api/v1/classes/"+classID+"/forum", parent.Token, nil)
		require.Equal(t, http.StatusOK, forum.Status, "body %v", forum.Body)

		require.Equal(t, http.StatusOK, call(t, http.MethodGet, "/api/v1/guru/dashboard?classId="+classID, teacher.Token, nil).Status)
	})

	t.Run("a parent still keeps the child and a child can still move out", func(t *testing.T) {
		renamed := call(t, http.MethodPatch, "/api/v1/wali/children/"+childID, parent.Token, map[string]any{"fullName": "Anak Kelas Arsip Baru"})
		require.Equal(t, http.StatusOK, renamed.Status, "body %v", renamed.Body)

		res := requestMove(t, parent.Token, childID, nextCode)
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

		accepted := call(t, http.MethodPost, "/api/v1/guru/classes/"+nextClass+"/move-requests/"+res.data("id").(string)+"/accept", next.Token, nil)
		require.Equal(t, http.StatusOK, accepted.Status, "body %v", accepted.Body)

		// nobody can ask to move into the archived class, its code does not work
		requestMove(t, parent.Token, childID, joinCode).requireError(t, http.StatusNotFound, "CLASS_CODE_NOT_FOUND")
	})
}

func TestTheOnlyTeacherOfAnArchivedClassDeletesTheAccountWithoutASuccessor(t *testing.T) {
	teacher := registerGuru(t, "Mawar Arsip")
	classID, joinCode := classOf(t, teacher)
	_, _ = registerWali(t, joinCode, "Anak Yang Tinggal")

	require.NoError(t, testDB.Exec("UPDATE classes SET archived_at = now() WHERE id = ?::uuid", classID).Error)

	res := call(t, http.MethodDelete, "/api/v1/users/me", teacher.Token, map[string]any{"password": testPassword})
	require.Equal(t, http.StatusOK, res.Status, "a class that nobody can change needs no successor, body %v", res.Body)
}
