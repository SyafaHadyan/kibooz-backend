package e2e

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// A NUL character cannot be stored in PostgreSQL text, so every route that takes text has to refuse it with a 400 and the name
// of the field and not let the query fail as a 500.
func TestANulCharacterInTextIsRefusedAsAValidationError(t *testing.T) {
	guru := registerGuru(t, "Nul Class")
	classID, joinCode := classOf(t, guru)
	wali, studentID := registerWali(t, joinCode, "Nul Child")

	thread := call(t, http.MethodPost, "/api/v1/classes/"+classID+"/forum", guru.Token, map[string]any{"title": "Thread", "body": "Body"})
	require.Equal(t, http.StatusCreated, thread.Status, "body %v", thread.Body)
	threadID := thread.data("id").(string)

	reply := call(t, http.MethodPost, "/api/v1/classes/"+classID+"/forum/"+threadID+"/replies", guru.Token, map[string]any{"body": "Reply"})
	require.Equal(t, http.StatusCreated, reply.Status, "body %v", reply.Body)
	replyID := reply.data("id").(string)

	video := call(t, http.MethodPost, "/api/v1/classes/"+classID+"/videos", guru.Token, map[string]any{
		"title": "Video", "videoUrl": "https://example.com/nul.mp4",
	})
	require.Equal(t, http.StatusCreated, video.Status, "body %v", video.Body)
	videoID := video.data("id").(string)

	classes := "/api/v1/classes/" + classID
	bad := "bad\x00text"

	cases := []struct {
		name   string
		method string
		path   string
		token  string
		body   map[string]any
		field  string
	}{
		{"register a teacher", http.MethodPost, "/api/v1/auth/register", "", map[string]any{
			"email": fmt.Sprintf("nul.%s@example.com", suffix()), "password": testPassword, "fullName": bad,
			"role": "GURU", "class": map[string]any{"name": "Nul"},
		}, "fullName"},
		{"register a teacher with a bad class name", http.MethodPost, "/api/v1/auth/register", "", map[string]any{
			"email": fmt.Sprintf("nul.%s@example.com", suffix()), "password": testPassword, "fullName": "Nul Teacher",
			"role": "GURU", "class": map[string]any{"name": bad},
		}, "class.name"},
		{"register a parent", http.MethodPost, "/api/v1/auth/register", "", map[string]any{
			"email": fmt.Sprintf("nul.%s@example.com", suffix()), "password": testPassword, "fullName": "Nul Parent",
			"role": "WALI", "classCode": joinCode, "student": map[string]any{"nisn": nisn(), "fullName": bad},
		}, "student.fullName"},
		{"sign in", http.MethodPost, "/api/v1/auth/login", "", map[string]any{
			"email": "nul\x00@example.com", "password": testPassword, "role": "GURU",
		}, "email"},
		{"post a thread", http.MethodPost, classes + "/forum", guru.Token, map[string]any{"title": bad, "body": "Body"}, "title"},
		{"edit a thread", http.MethodPatch, classes + "/forum/" + threadID, guru.Token, map[string]any{"body": bad}, "body"},
		{"post a reply", http.MethodPost, classes + "/forum/" + threadID + "/replies", wali.Token, map[string]any{"body": bad}, "body"},
		{"edit a reply", http.MethodPatch, classes + "/forum/" + threadID + "/replies/" + replyID, guru.Token, map[string]any{"body": bad}, "body"},
		{"add a video", http.MethodPost, classes + "/videos", guru.Token, map[string]any{"title": bad, "videoUrl": "https://example.com/v.mp4"}, "title"},
		{"edit a video", http.MethodPatch, classes + "/videos/" + videoID, guru.Token, map[string]any{"title": bad}, "title"},
		{"create a class", http.MethodPost, "/api/v1/guru/classes", guru.Token, map[string]any{"name": bad}, "name"},
		{"change the profile", http.MethodPut, "/api/v1/guru/profile", guru.Token, map[string]any{"address": bad}, "address"},
		{"change the name of a teacher", http.MethodPut, "/api/v1/guru/profile", guru.Token, map[string]any{"fullName": bad}, "fullName"},
		{"change the name of a parent", http.MethodPatch, "/api/v1/wali/profile", wali.Token, map[string]any{"fullName": bad}, "fullName"},
		{"change the address of a parent", http.MethodPatch, "/api/v1/wali/profile", wali.Token, map[string]any{"address": bad}, "address"},
		{"apply a guide", http.MethodPost, "/api/v1/wali/guidance/apply", wali.Token, map[string]any{
			"guidanceId": "guidance-senang-4step", "studentId": studentID, "parentNotes": bad,
		}, "parentNotes"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := call(t, tc.method, tc.path, tc.token, tc.body)
			require.Equal(t, http.StatusBadRequest, res.Status, "body %v", res.Body)
			require.Equal(t, "VALIDATION_ERROR", res.Body["errorCode"])
			require.Contains(t, res.Body["details"], tc.field)
		})
	}

	t.Run("the same requests with clean text still work", func(t *testing.T) {
		res := call(t, http.MethodPatch, classes+"/forum/"+threadID, guru.Token, map[string]any{"body": "Clean text"})
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
	})
}
