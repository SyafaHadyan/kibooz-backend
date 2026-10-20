package e2e

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/idempotency"
)

// requireRepeatIsReplay sends the same request twice with one key. The second answer has to be the first one, marked as a replay.
// The handler must not run again, which the callers check with what the route writes.
func requireRepeatIsReplay(t *testing.T, path string, token string, body map[string]any) result {
	t.Helper()

	key := uuid.NewString()

	first, firstHeader := callKey(t, http.MethodPost, path, token, key, body)
	require.Less(t, first.Status, 300, "body %v", first.Body)
	require.Empty(t, firstHeader.Get(idempotency.ReplayedHeader))

	again, againHeader := callKey(t, http.MethodPost, path, token, key, body)
	require.Equal(t, first.Status, again.Status, "body %v", again.Body)
	require.Equal(t, "true", againHeader.Get(idempotency.ReplayedHeader))
	require.Equal(t, first.Body, again.Body, "the repeat has to carry the first answer")

	return first
}

// TestEveryOtherCreatingRouteRepeatsSafely covers the routes that TestIdempotencyKey does not, so all eleven routes that take an
// Idempotency-Key are held to the same rule. Each one is repeated with its own key and has to write once.
func TestEveryOtherCreatingRouteRepeatsSafely(t *testing.T) {
	guru := registerGuru(t, "Seruni")
	classID, joinCode := classOf(t, guru)
	amelia, ameliaID := registerWali(t, joinCode, "Amelia Siti Zahra")
	_, farhanID := registerWali(t, joinCode, "Farhan Al-Fatih")
	ameliaWaliID := waliIDOfEmail(t, amelia.Email)

	t.Run("a mood batch is saved once", func(t *testing.T) {
		before := moodLogCount(t, ameliaID) + moodLogCount(t, farhanID)

		requireRepeatIsReplay(t, moodBatchPath, guru.Token, map[string]any{
			"classId": classID,
			"entries": []map[string]any{batchEntry(ameliaID, "SENANG", 0.9), batchEntry(farhanID, "SEDIH", 0.8)},
		})

		require.EqualValues(t, before+2, moodLogCount(t, ameliaID)+moodLogCount(t, farhanID), "two entries, once")
	})

	t.Run("a guide is applied once", func(t *testing.T) {
		requireRepeatIsReplay(t, "/api/v1/wali/guidance/apply", amelia.Token, map[string]any{
			"guidanceId": "guidance-bingung-4step", "studentId": ameliaID, "parentNotes": "Talked to gently.",
		})

		reports := call(t, http.MethodGet, "/api/v1/classes/"+classID+"/guidance-reports", guru.Token, nil)
		require.Equal(t, http.StatusOK, reports.Status, "body %v", reports.Body)
		require.EqualValues(t, 1, reports.data("total"), "one guidance application")
	})

	t.Run("a child is added once", func(t *testing.T) {
		before := activeChildrenOf(t, ameliaWaliID)

		requireRepeatIsReplay(t, "/api/v1/wali/children", amelia.Token, map[string]any{
			"classCode": joinCode, "student": map[string]any{"nisn": nisn(), "fullName": "Second Child"},
		})

		require.EqualValues(t, before+1, activeChildrenOf(t, ameliaWaliID))
	})

	t.Run("a video is added once", func(t *testing.T) {
		upload := call(t, http.MethodPost, "/api/v1/classes/"+classID+"/videos/upload-url", guru.Token,
			map[string]any{"contentType": "video/mp4", "sizeBytes": 2048})
		require.Equal(t, http.StatusOK, upload.Status, "body %v", upload.Body)

		sendFile(t, upload, "video/mp4", 2048)

		// a second real run would find the staged file gone and answer 400, so a 201 twice can only be the saved answer
		requireRepeatIsReplay(t, "/api/v1/classes/"+classID+"/videos", guru.Token,
			map[string]any{"title": "Recorded lesson", "videoUrl": upload.data("videoUrl")})

		videos := call(t, http.MethodGet, "/api/v1/classes/"+classID+"/videos", guru.Token, nil)
		require.Len(t, listOf(t, videos, "videos"), 1)
	})

	t.Run("a reply is posted once", func(t *testing.T) {
		forum := "/api/v1/classes/" + classID + "/forum"

		thread := call(t, http.MethodPost, forum, guru.Token, map[string]any{"title": "Question", "body": "Who brings the fruit?"})
		require.Equal(t, http.StatusCreated, thread.Status, "body %v", thread.Body)

		replies := forum + "/" + thread.data("id").(string) + "/replies"

		requireRepeatIsReplay(t, replies, amelia.Token, map[string]any{"body": "I do"})

		list := call(t, http.MethodGet, replies, guru.Token, nil)
		require.Len(t, listOf(t, list, "replies"), 1)
	})

	t.Run("a teacher is added once", func(t *testing.T) {
		second := registerGuru(t, "Teratai")
		teachers := "/api/v1/guru/classes/" + classID + "/teachers"

		requireRepeatIsReplay(t, teachers, guru.Token, map[string]any{"email": second.Email})

		list := call(t, http.MethodGet, teachers, guru.Token, nil)
		require.Len(t, listOf(t, list, "teachers"), 2, "the owner and the teacher who was added")
	})
}
