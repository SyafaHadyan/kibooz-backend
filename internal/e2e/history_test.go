package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestActivityHistory(t *testing.T) {
	guru := registerGuru(t, "Melati History")
	classID, joinCode := classOf(t, guru)
	wali, childID := registerWali(t, joinCode, "Bima Arya")
	otherWali, otherChild := registerWali(t, joinCode, "Candra Kirana")

	otherGuru := registerGuru(t, "Kenanga History")
	otherClassID, otherCode := classOf(t, otherGuru)
	_ = otherChild

	t.Run("a new class and child begin with empty history timelines", func(t *testing.T) {
		guruRes := call(t, http.MethodGet, "/api/v1/guru/history", guru.Token, nil)
		require.Equal(t, http.StatusOK, guruRes.Status, "body %v", guruRes.Body)
		require.Empty(t, listOf(t, guruRes, "events"))
		require.EqualValues(t, 0, guruRes.data("total"))

		waliRes := call(t, http.MethodGet, "/api/v1/wali/children/"+childID+"/history", wali.Token, nil)
		require.Equal(t, http.StatusOK, waliRes.Status, "body %v", waliRes.Body)
		require.Equal(t, childID, waliRes.data("student", "id"))
		require.Empty(t, listOf(t, waliRes, "events"))
		require.EqualValues(t, 0, waliRes.data("total"))

		call(t, http.MethodGet, "/api/v1/guru/history", wali.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodGet, "/api/v1/wali/children/"+childID+"/history", guru.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodGet, "/api/v1/guru/history", "", nil).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
		call(t, http.MethodGet, "/api/v1/wali/children/"+childID+"/history", "", nil).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
	})

	t.Run("guru and wali timelines include cross-date events, filtering, pagination and ownership checks", func(t *testing.T) {
		yesterday := time.Now().UTC().Add(-26 * time.Hour).Format(time.RFC3339)

		oldMood := call(t, http.MethodPost, "/api/v1/guru/mood/log", guru.Token, map[string]any{
			"studentId": childID, "moodType": "SEDIH", "notes": "Yesterday mood", "recordedAt": yesterday,
		})
		require.Equal(t, http.StatusCreated, oldMood.Status, "body %v", oldMood.Body)

		vidRes := call(t, http.MethodPost, fmt.Sprintf("/api/v1/classes/%s/videos", classID), guru.Token, map[string]any{
			"title": "Senam Pagi", "videoUrl": "https://videos.example.com/senam.mp4", "description": "Warm up",
		})
		require.Equal(t, http.StatusCreated, vidRes.Status, "body %v", vidRes.Body)

		scanRes := call(t, http.MethodPost, "/api/v1/trash/scan-claim", wali.Token, map[string]any{
			"studentId": childID, "trashType": "ORGANIK", "confidenceScore": 0.92,
		})
		require.Equal(t, http.StatusOK, scanRes.Status, "body %v", scanRes.Body)

		applyRes := call(t, http.MethodPost, "/api/v1/wali/guidance/apply", wali.Token, map[string]any{
			"guidanceId": "guidance-sedih-4step", "studentId": childID, "parentNotes": "Applied at home",
		})
		require.Equal(t, http.StatusOK, applyRes.Status, "body %v", applyRes.Body)

		guruAll := call(t, http.MethodGet, "/api/v1/guru/history?classId="+classID+"&page=1&limit=2", guru.Token, nil)
		require.Equal(t, http.StatusOK, guruAll.Status, "body %v", guruAll.Body)
		require.EqualValues(t, 3, guruAll.data("total"))
		require.Len(t, listOf(t, guruAll, "events"), 2)

		guruMoods := call(t, http.MethodGet, "/api/v1/guru/history?type=mood", guru.Token, nil)
		require.Equal(t, http.StatusOK, guruMoods.Status, "body %v", guruMoods.Body)
		require.EqualValues(t, 1, guruMoods.data("total"))
		require.Equal(t, "mood", dig(listOf(t, guruMoods, "events")[0], "type"))

		guruVideos := call(t, http.MethodGet, "/api/v1/guru/history?type=video", guru.Token, nil)
		require.Equal(t, http.StatusOK, guruVideos.Status, "body %v", guruVideos.Body)
		require.Equal(t, "video", dig(listOf(t, guruVideos, "events")[0], "type"))

		guruGuidance := call(t, http.MethodGet, "/api/v1/guru/history?type=guidance", guru.Token, nil)
		require.Equal(t, http.StatusOK, guruGuidance.Status, "body %v", guruGuidance.Body)
		require.Equal(t, "guidance", dig(listOf(t, guruGuidance, "events")[0], "type"))

		waliAll := call(t, http.MethodGet, "/api/v1/wali/children/"+childID+"/history?page=1&limit=2", wali.Token, nil)
		require.Equal(t, http.StatusOK, waliAll.Status, "body %v", waliAll.Body)
		require.EqualValues(t, 3, waliAll.data("total"))
		require.Len(t, listOf(t, waliAll, "events"), 2)

		waliTrash := call(t, http.MethodGet, "/api/v1/wali/children/"+childID+"/history?type=trash", wali.Token, nil)
		require.Equal(t, http.StatusOK, waliTrash.Status, "body %v", waliTrash.Body)
		require.EqualValues(t, 1, waliTrash.data("total"))
		require.Equal(t, "trash", dig(listOf(t, waliTrash, "events")[0], "type"))

		call(t, http.MethodGet, "/api/v1/guru/history?type=wrong", guru.Token, nil).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodGet, "/api/v1/guru/history?classId=not-a-uuid", guru.Token, nil).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodGet, "/api/v1/guru/history?classId="+otherClassID, guru.Token, nil).
			requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")

		call(t, http.MethodGet, "/api/v1/wali/children/not-a-uuid/history", wali.Token, nil).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodGet, "/api/v1/wali/children/"+childID+"/history?type=video", wali.Token, nil).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodGet, "/api/v1/wali/children/"+childID+"/history", otherWali.Token, nil).
			requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")
		call(t, http.MethodGet, "/api/v1/wali/children/"+uuid.NewString()+"/history", wali.Token, nil).
			requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")
	})

	t.Run("moving to another class isolates previous history from the new class timeline", func(t *testing.T) {
		moveRes := call(t, http.MethodPost, "/api/v1/wali/children/"+childID+"/move-requests", wali.Token, map[string]any{
			"classCode": otherCode,
		})
		require.Equal(t, http.StatusCreated, moveRes.Status, "body %v", moveRes.Body)
		moveID := moveRes.data("id").(string)

		acceptRes := call(t, http.MethodPost, fmt.Sprintf("/api/v1/guru/classes/%s/move-requests/%s/accept", otherClassID, moveID),
			otherGuru.Token, nil)
		require.Equal(t, http.StatusOK, acceptRes.Status, "body %v", acceptRes.Body)

		newWaliHistory := call(t, http.MethodGet, "/api/v1/wali/children/"+childID+"/history", wali.Token, nil)
		require.Equal(t, http.StatusOK, newWaliHistory.Status, "body %v", newWaliHistory.Body)
		require.Empty(t, listOf(t, newWaliHistory, "events"))

		newGuruHistory := call(t, http.MethodGet, "/api/v1/guru/history?classId="+otherClassID, otherGuru.Token, nil)
		require.Equal(t, http.StatusOK, newGuruHistory.Status, "body %v", newGuruHistory.Body)
		require.Empty(t, listOf(t, newGuruHistory, "events"))
	})
}
