package e2e

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNotifications(t *testing.T) {
	guru := registerGuru(t, "Mawar Notif")
	classID, joinCode := classOf(t, guru)
	wali, childID := registerWali(t, joinCode, "Mawar Notif Child")

	otherGuru := registerGuru(t, "Anggrek Notif")
	otherClassID, otherCode := classOf(t, otherGuru)
	_ = otherClassID

	t.Run("a new user has an empty notification inbox", func(t *testing.T) {
		for _, token := range []string{guru.Token, wali.Token} {
			res := call(t, http.MethodGet, "/api/v1/notifications", token, nil)
			require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
			require.Empty(t, listOf(t, res, "notifications"))
			require.EqualValues(t, 0, res.data("unreadCount"))
			require.EqualValues(t, 0, res.data("total"))
		}

		call(t, http.MethodGet, "/api/v1/notifications", "", nil).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
		call(t, http.MethodGet, "/api/v1/notifications?unreadOnly=maybe", wali.Token, nil).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
	})

	var moodNotifID string

	t.Run("events across mood, guidance, videos and moves create notifications", func(t *testing.T) {
		moodRes := call(t, http.MethodPost, "/api/v1/guru/mood/log", guru.Token, map[string]any{
			"studentId": childID, "moodType": "SENANG",
		})
		require.Equal(t, http.StatusCreated, moodRes.Status, "body %v", moodRes.Body)

		vidRes := call(t, http.MethodPost, fmt.Sprintf("/api/v1/classes/%s/videos", classID), guru.Token, map[string]any{
			"title": "Video Emosi", "videoUrl": "https://videos.example.com/emosi.mp4",
		})
		require.Equal(t, http.StatusCreated, vidRes.Status, "body %v", vidRes.Body)

		applyRes := call(t, http.MethodPost, "/api/v1/wali/guidance/apply", wali.Token, map[string]any{
			"guidanceId": "guidance-senang-4step", "studentId": childID, "parentNotes": "Applied at home",
		})
		require.Equal(t, http.StatusOK, applyRes.Status, "body %v", applyRes.Body)

		moveRes := call(t, http.MethodPost, "/api/v1/wali/children/"+childID+"/move-requests", wali.Token, map[string]any{
			"classCode": otherCode,
		})
		require.Equal(t, http.StatusCreated, moveRes.Status, "body %v", moveRes.Body)
		moveID := moveRes.data("id").(string)

		acceptRes := call(t, http.MethodPost, fmt.Sprintf("/api/v1/guru/classes/%s/move-requests/%s/accept", otherClassID, moveID),
			otherGuru.Token, nil)
		require.Equal(t, http.StatusOK, acceptRes.Status, "body %v", acceptRes.Body)

		waliInbox := call(t, http.MethodGet, "/api/v1/notifications", wali.Token, nil)
		require.Equal(t, http.StatusOK, waliInbox.Status, "body %v", waliInbox.Body)
		require.EqualValues(t, 3, waliInbox.data("unreadCount"))
		require.EqualValues(t, 3, waliInbox.data("total"))

		items := listOf(t, waliInbox, "notifications")
		require.Equal(t, "move_decided", dig(items[0], "kind"))
		moodNotifID = dig(items[2], "id").(string)
		require.Equal(t, "mood_logged", dig(items[2], "kind"))
		require.Equal(t, "Teacher recorded mood for Mawar: Happy / Cheerful", dig(items[2], "body"))

		guruInbox := call(t, http.MethodGet, "/api/v1/notifications", guru.Token, nil)
		require.Equal(t, http.StatusOK, guruInbox.Status)
		require.EqualValues(t, 1, guruInbox.data("unreadCount"))

		targetInbox := call(t, http.MethodGet, "/api/v1/notifications", otherGuru.Token, nil)
		require.Equal(t, http.StatusOK, targetInbox.Status)
		require.EqualValues(t, 1, targetInbox.data("unreadCount"))
		require.Equal(t, "move_requested", dig(listOf(t, targetInbox, "notifications")[0], "kind"))
	})

	t.Run("marking notifications read updates unread counts and protects ownership", func(t *testing.T) {
		call(t, http.MethodPost, "/api/v1/notifications/"+moodNotifID+"/read", guru.Token, nil).
			requireError(t, http.StatusNotFound, "NOTIFICATION_NOT_FOUND")
		call(t, http.MethodPost, "/api/v1/notifications/"+uuid.NewString()+"/read", wali.Token, nil).
			requireError(t, http.StatusNotFound, "NOTIFICATION_NOT_FOUND")
		call(t, http.MethodPost, "/api/v1/notifications/not-a-uuid/read", wali.Token, nil).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")

		readOne := call(t, http.MethodPost, "/api/v1/notifications/"+moodNotifID+"/read", wali.Token, nil)
		require.Equal(t, http.StatusOK, readOne.Status, "body %v", readOne.Body)
		require.Equal(t, "Notification marked as read", readOne.Body["message"])

		unreadOnly := call(t, http.MethodGet, "/api/v1/notifications?unreadOnly=true", wali.Token, nil)
		require.Equal(t, http.StatusOK, unreadOnly.Status)
		require.EqualValues(t, 2, unreadOnly.data("unreadCount"))
		require.EqualValues(t, 2, unreadOnly.data("total"))

		readAll := call(t, http.MethodPost, "/api/v1/notifications/read-all", wali.Token, nil)
		require.Equal(t, http.StatusOK, readAll.Status, "body %v", readAll.Body)
		require.Equal(t, "All notifications marked as read", readAll.Body["message"])

		after := call(t, http.MethodGet, "/api/v1/notifications?unreadOnly=true", wali.Token, nil)
		require.Equal(t, http.StatusOK, after.Status)
		require.EqualValues(t, 0, after.data("unreadCount"))
		require.Empty(t, listOf(t, after, "notifications"))
	})

	t.Run("deleting the account removes its notifications", func(t *testing.T) {
		count := func() int64 {
			var total int64

			require.NoError(t, testDB.Raw(
				"SELECT COUNT(*) FROM notifications AS n JOIN users AS u ON u.id = n.user_id WHERE u.email = ?", wali.Email,
			).Scan(&total).Error)

			return total
		}

		require.EqualValues(t, 3, count(), "the parent has notifications before the deletion")

		res := call(t, http.MethodDelete, "/api/v1/users/me", wali.Token, map[string]any{"password": testPassword})
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		require.EqualValues(t, 0, count())
	})
}
