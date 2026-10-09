package e2e

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func listOf(t *testing.T, res result, key string) []any {
	t.Helper()

	items, ok := res.data(key).([]any)
	require.True(t, ok, "%s must be a list, body %v", key, res.Body)

	return items
}

func titles(t *testing.T, items []any, field string) []string {
	t.Helper()

	out := make([]string, 0, len(items))

	for _, item := range items {
		out = append(out, item.(map[string]any)[field].(string))
	}

	return out
}

func TestLearningVideos(t *testing.T) {
	guru := registerGuru(t, "Melati")
	classID, joinCode := classOf(t, guru)
	wali, _ := registerWali(t, joinCode, "Melati Child")

	otherGuru := registerGuru(t, "Cempaka")
	otherClassID, otherCode := classOf(t, otherGuru)
	otherWali, _ := registerWali(t, otherCode, "Cempaka Child")

	videos := fmt.Sprintf("/api/v1/classes/%s/videos", classID)

	t.Run("a parent finds the class id on the dashboard", func(t *testing.T) {
		res := call(t, http.MethodGet, "/api/v1/wali/dashboard", wali.Token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, classID, res.data("student", "classId"))
	})

	t.Run("a new class has no videos", func(t *testing.T) {
		for name, token := range map[string]string{"teacher": guru.Token, "parent": wali.Token} {
			res := call(t, http.MethodGet, videos, token, nil)
			require.Equal(t, http.StatusOK, res.Status, "%s, body %v", name, res.Body)
			require.Empty(t, listOf(t, res, "videos"))
			require.EqualValues(t, 1, res.data("page"))
			require.EqualValues(t, 20, res.data("limit"))
			require.EqualValues(t, 0, res.data("total"))
		}
	})

	t.Run("only a teacher of the class can add a video", func(t *testing.T) {
		body := map[string]any{"title": "Sorting trash", "videoUrl": "https://videos.example.com/sorting.mp4"}

		call(t, http.MethodPost, videos, wali.Token, body).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodPost, videos, otherGuru.Token, body).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodPost, videos, "", body).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
	})

	t.Run("only members of the class can list its videos", func(t *testing.T) {
		call(t, http.MethodGet, videos, otherGuru.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodGet, videos, otherWali.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodGet, videos, "", nil).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
		call(t, http.MethodGet, "/api/v1/classes/"+uuid.NewString()+"/videos", guru.Token, nil).
			requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodGet, "/api/v1/classes/"+otherClassID+"/videos", guru.Token, nil).
			requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
	})

	t.Run("a video needs a title and https addresses", func(t *testing.T) {
		invalid := map[string]map[string]any{
			"no body fields":     {},
			"blank title":        {"title": "   ", "videoUrl": "https://videos.example.com/a.mp4"},
			"http video":         {"title": "A", "videoUrl": "http://videos.example.com/a.mp4"},
			"script address":     {"title": "A", "videoUrl": "javascript:alert(1)"},
			"address with a gap": {"title": "A", "videoUrl": "https://videos.example.com/a b.mp4"},
			"http thumbnail":     {"title": "A", "videoUrl": "https://videos.example.com/a.mp4", "thumbnailUrl": "http://img.example.com/a.png"},
			"zero duration":      {"title": "A", "videoUrl": "https://videos.example.com/a.mp4", "durationSeconds": 0},
			"too long a title":   {"title": strings.Repeat("a", 151), "videoUrl": "https://videos.example.com/a.mp4"},
		}

		for name, body := range invalid {
			res := call(t, http.MethodPost, videos, guru.Token, body)
			require.Equal(t, http.StatusBadRequest, res.Status, "%s, body %v", name, res.Body)
			require.Equal(t, "VALIDATION_ERROR", res.Body["errorCode"], name)
		}

		res := call(t, http.MethodPost, videos, guru.Token, map[string]any{
			"title": " ", "videoUrl": "http://videos.example.com/a.mp4", "thumbnailUrl": "ftp://img.example.com/a.png",
		})
		res.requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")

		details, ok := res.Body["details"].(map[string]any)
		require.True(t, ok)
		require.Contains(t, details, "title")
		require.Contains(t, details, "videoUrl")
		require.Contains(t, details, "thumbnailUrl")
	})

	t.Run("add videos", func(t *testing.T) {
		first := call(t, http.MethodPost, videos, guru.Token, map[string]any{
			"title": "  Sorting trash  ", "description": "How to sort at school", "videoUrl": "https://videos.example.com/sorting.mp4",
			"thumbnailUrl": "https://img.example.com/sorting.png", "durationSeconds": 125,
		})
		require.Equal(t, http.StatusCreated, first.Status, "body %v", first.Body)
		require.Equal(t, "Learning video added", first.Body["message"])
		require.Equal(t, "Sorting trash", first.data("title"))
		require.Equal(t, "How to sort at school", first.data("description"))
		require.EqualValues(t, 125, first.data("durationSeconds"))
		require.NotEmpty(t, first.data("id"))

		minimal := call(t, http.MethodPost, videos, guru.Token, map[string]any{
			"title": "Washing hands", "videoUrl": "https://videos.example.com/hands.mp4",
		})
		require.Equal(t, http.StatusCreated, minimal.Status, "body %v", minimal.Body)
		require.Nil(t, minimal.data("description"))
		require.Nil(t, minimal.data("thumbnailUrl"))
		require.Nil(t, minimal.data("durationSeconds"))
	})

	t.Run("list the newest video first and page through them", func(t *testing.T) {
		res := call(t, http.MethodGet, videos, wali.Token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.EqualValues(t, 2, res.data("total"))
		require.Equal(t, []string{"Washing hands", "Sorting trash"}, titles(t, listOf(t, res, "videos"), "title"))

		second := call(t, http.MethodGet, videos+"?page=2&limit=1", guru.Token, nil)
		require.Equal(t, http.StatusOK, second.Status, "body %v", second.Body)
		require.Equal(t, []string{"Sorting trash"}, titles(t, listOf(t, second, "videos"), "title"))
		require.EqualValues(t, 2, second.data("page"))
		require.EqualValues(t, 1, second.data("limit"))
		require.EqualValues(t, 2, second.data("total"))

		beyond := call(t, http.MethodGet, videos+"?page=3&limit=1", guru.Token, nil)
		require.Equal(t, http.StatusOK, beyond.Status)
		require.Empty(t, listOf(t, beyond, "videos"))
	})

	t.Run("a bad request is refused", func(t *testing.T) {
		for _, query := range []string{"?page=0", "?page=x", "?limit=0", "?limit=51", "?limit=x"} {
			call(t, http.MethodGet, videos+query, guru.Token, nil).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		}

		call(t, http.MethodGet, "/api/v1/classes/not-a-uuid/videos", guru.Token, nil).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodPost, "/api/v1/classes/not-a-uuid/videos", guru.Token, map[string]any{}).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
	})
}

func TestClassForum(t *testing.T) {
	guru := registerGuru(t, "Flamboyan")
	classID, joinCode := classOf(t, guru)
	wali, _ := registerWali(t, joinCode, "Flamboyan Child")

	otherGuru := registerGuru(t, "Teratai")
	otherClassID, otherCode := classOf(t, otherGuru)
	otherWali, _ := registerWali(t, otherCode, "Teratai Child")

	forum := fmt.Sprintf("/api/v1/classes/%s/forum", classID)

	var guruThread, waliThread string

	t.Run("a new class has no threads", func(t *testing.T) {
		res := call(t, http.MethodGet, forum, wali.Token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Empty(t, listOf(t, res, "threads"))
		require.EqualValues(t, 0, res.data("total"))
	})

	t.Run("only members of the class can use the forum", func(t *testing.T) {
		thread := map[string]any{"title": "Hello", "body": "Hello everyone"}

		for _, token := range []string{otherGuru.Token, otherWali.Token} {
			call(t, http.MethodGet, forum, token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
			call(t, http.MethodPost, forum, token, thread).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		}

		call(t, http.MethodGet, forum, "", nil).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
		call(t, http.MethodPost, forum, "", thread).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
	})

	t.Run("a thread needs a title and a body", func(t *testing.T) {
		for _, body := range []map[string]any{
			{},
			{"title": "Only a title"},
			{"body": "Only a body"},
			{"title": "  ", "body": "Blank title"},
			{"title": "Blank body", "body": " \n "},
			{"title": strings.Repeat("a", 151), "body": "Long title"},
			{"title": "Long body", "body": strings.Repeat("a", 5001)},
		} {
			call(t, http.MethodPost, forum, guru.Token, body).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		}
	})

	t.Run("teachers and parents start threads", func(t *testing.T) {
		first := call(t, http.MethodPost, forum, guru.Token, map[string]any{"title": " Field trip ", "body": " Bring a hat. "})
		require.Equal(t, http.StatusCreated, first.Status, "body %v", first.Body)
		require.Equal(t, "Forum thread created", first.Body["message"])
		require.Equal(t, "Field trip", first.data("title"))
		require.Equal(t, "Bring a hat.", first.data("body"))
		require.Equal(t, "GURU", first.data("author", "role"))
		require.Equal(t, "Siti Rahayu, S.Pd.", first.data("author", "fullName"))
		require.EqualValues(t, 0, first.data("replyCount"))

		guruThread = first.data("id").(string)

		second := call(t, http.MethodPost, forum, wali.Token, map[string]any{"title": "Lunch box", "body": "Is fruit allowed?"})
		require.Equal(t, http.StatusCreated, second.Status, "body %v", second.Body)
		require.Equal(t, "WALI", second.data("author", "role"))

		waliThread = second.data("id").(string)
	})

	t.Run("list the newest thread first", func(t *testing.T) {
		res := call(t, http.MethodGet, forum, guru.Token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.EqualValues(t, 2, res.data("total"))
		require.Equal(t, []string{"Lunch box", "Field trip"}, titles(t, listOf(t, res, "threads"), "title"))

		page := call(t, http.MethodGet, forum+"?page=2&limit=1", wali.Token, nil)
		require.Equal(t, http.StatusOK, page.Status, "body %v", page.Body)
		require.Equal(t, []string{"Field trip"}, titles(t, listOf(t, page, "threads"), "title"))

		call(t, http.MethodGet, forum+"?limit=100", guru.Token, nil).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodGet, "/api/v1/classes/not-a-uuid/forum", guru.Token, nil).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodPost, "/api/v1/classes/not-a-uuid/forum", guru.Token, map[string]any{}).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
	})

	replies := func(thread string) string {
		return fmt.Sprintf("%s/%s/replies", forum, thread)
	}

	t.Run("replies", func(t *testing.T) {
		empty := call(t, http.MethodGet, replies(guruThread), wali.Token, nil)
		require.Equal(t, http.StatusOK, empty.Status, "body %v", empty.Body)
		require.Empty(t, listOf(t, empty, "replies"))

		fromParent := call(t, http.MethodPost, replies(guruThread), wali.Token, map[string]any{"body": " I will bring one. "})
		require.Equal(t, http.StatusCreated, fromParent.Status, "body %v", fromParent.Body)
		require.Equal(t, "Forum reply added", fromParent.Body["message"])
		require.Equal(t, "I will bring one.", fromParent.data("body"))
		require.Equal(t, "WALI", fromParent.data("author", "role"))

		fromTeacher := call(t, http.MethodPost, replies(guruThread), guru.Token, map[string]any{"body": "Thank you!"})
		require.Equal(t, http.StatusCreated, fromTeacher.Status, "body %v", fromTeacher.Body)

		list := call(t, http.MethodGet, replies(guruThread), guru.Token, nil)
		require.Equal(t, http.StatusOK, list.Status, "body %v", list.Body)
		require.EqualValues(t, 2, list.data("total"))
		require.Equal(t, []string{"I will bring one.", "Thank you!"}, titles(t, listOf(t, list, "replies"), "body"))

		second := call(t, http.MethodGet, replies(guruThread)+"?page=2&limit=1", guru.Token, nil)
		require.Equal(t, http.StatusOK, second.Status, "body %v", second.Body)
		require.Equal(t, []string{"Thank you!"}, titles(t, listOf(t, second, "replies"), "body"))

		threads := call(t, http.MethodGet, forum, guru.Token, nil)
		require.Equal(t, http.StatusOK, threads.Status)

		counts := map[string]any{}
		for _, item := range listOf(t, threads, "threads") {
			thread := item.(map[string]any)
			counts[thread["id"].(string)] = thread["replyCount"]
		}

		require.EqualValues(t, 2, counts[guruThread])
		require.EqualValues(t, 0, counts[waliThread])
	})

	t.Run("a reply needs a body", func(t *testing.T) {
		for _, body := range []map[string]any{{}, {"body": "   "}, {"body": strings.Repeat("a", 5001)}} {
			call(t, http.MethodPost, replies(guruThread), guru.Token, body).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		}
	})

	t.Run("replies belong to a thread of this class", func(t *testing.T) {
		reply := call(t, http.MethodPost, replies(waliThread), guru.Token, map[string]any{"body": "Yes, fruit is fine."})
		require.Equal(t, http.StatusCreated, reply.Status, "body %v", reply.Body)

		replyID := reply.data("id").(string)
		body := map[string]any{"body": "Nested"}

		// a reply cannot be answered again, and neither can a post that does not exist
		call(t, http.MethodPost, replies(replyID), guru.Token, body).requireError(t, http.StatusNotFound, "FORUM_POST_NOT_FOUND")
		call(t, http.MethodGet, replies(replyID), guru.Token, nil).requireError(t, http.StatusNotFound, "FORUM_POST_NOT_FOUND")
		call(t, http.MethodPost, replies(uuid.NewString()), guru.Token, body).requireError(t, http.StatusNotFound, "FORUM_POST_NOT_FOUND")
		call(t, http.MethodGet, replies(uuid.NewString()), wali.Token, nil).requireError(t, http.StatusNotFound, "FORUM_POST_NOT_FOUND")

		// a thread of another class is not reachable through this class
		other := call(t, http.MethodPost, fmt.Sprintf("/api/v1/classes/%s/forum", otherClassID), otherGuru.Token,
			map[string]any{"title": "Other class", "body": "Not for you"})
		require.Equal(t, http.StatusCreated, other.Status, "body %v", other.Body)

		otherThread := other.data("id").(string)

		call(t, http.MethodPost, replies(otherThread), guru.Token, body).requireError(t, http.StatusNotFound, "FORUM_POST_NOT_FOUND")
		call(t, http.MethodGet, replies(otherThread), guru.Token, nil).requireError(t, http.StatusNotFound, "FORUM_POST_NOT_FOUND")

		// members of another class cannot read or write replies at all
		call(t, http.MethodGet, replies(guruThread), otherWali.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodPost, replies(guruThread), otherGuru.Token, body).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodGet, replies(guruThread), "", nil).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")

		call(t, http.MethodGet, replies("not-a-uuid"), guru.Token, nil).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodPost, replies("not-a-uuid"), guru.Token, body).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodGet, replies(guruThread)+"?page=0", guru.Token, nil).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
	})

	t.Run("a deleted account keeps its posts without its name", func(t *testing.T) {
		require.Equal(t, http.StatusOK, deleteAccount(t, wali.Token, wali.Password).Status)

		// the account cannot use the forum any more
		call(t, http.MethodGet, forum, wali.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")

		res := call(t, http.MethodGet, forum, guru.Token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		var seen bool

		for _, item := range listOf(t, res, "threads") {
			thread := item.(map[string]any)
			if thread["id"] != waliThread {
				continue
			}

			seen = true
			author := thread["author"].(map[string]any)
			require.Equal(t, "Deleted account", author["fullName"])
			require.Nil(t, author["avatarUrl"])
			require.Equal(t, "WALI", author["role"])
		}

		require.True(t, seen, "the thread of the deleted account must stay")

		list := call(t, http.MethodGet, replies(guruThread), guru.Token, nil)
		require.Equal(t, http.StatusOK, list.Status, "body %v", list.Body)
		require.Equal(t, "Deleted account", dig(listOf(t, list, "replies")[0], "author", "fullName"))
	})
}

// sendFile does what the app does with a signed address, a PUT of the bytes with the headers of the answer
func sendFile(t *testing.T, upload result, contentType string, size int) {
	t.Helper()

	req, err := http.NewRequest(http.MethodPut, upload.data("uploadUrl").(string), bytes.NewReader(make([]byte, size)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", contentType)

	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	require.Equal(t, http.StatusOK, res.StatusCode)
}

func TestVideoUpload(t *testing.T) {
	guru := registerGuru(t, "Anggrek")
	classID, joinCode := classOf(t, guru)
	wali, _ := registerWali(t, joinCode, "Anggrek Child")

	otherGuru := registerGuru(t, "Dahlia")
	otherClassID, _ := classOf(t, otherGuru)

	sign := func(token string, class string, body map[string]any) result {
		return call(t, http.MethodPost, fmt.Sprintf("/api/v1/classes/%s/videos/upload-url", class), token, body)
	}

	add := func(token string, class string, videoURL string) result {
		return call(t, http.MethodPost, fmt.Sprintf("/api/v1/classes/%s/videos", class), token,
			map[string]any{"title": "Recorded lesson", "videoUrl": videoURL})
	}

	t.Run("sign, send and add a file", func(t *testing.T) {
		upload := sign(guru.Token, classID, map[string]any{"contentType": "video/mp4", "sizeBytes": 2048})
		require.Equal(t, http.StatusOK, upload.Status, "body %v", upload.Body)
		require.Equal(t, "Upload URL created", upload.Body["message"])
		require.Equal(t, "PUT", upload.data("method"))
		require.Equal(t, "video/mp4", upload.data("headers", "Content-Type"))
		require.NotEmpty(t, upload.data("expiresAt"))

		videoURL := upload.data("videoUrl").(string)
		require.True(t, strings.HasPrefix(videoURL, testPublicURL+"/pending/videos/"+classID+"/"), videoURL)
		require.True(t, strings.HasSuffix(videoURL, ".mp4"), videoURL)

		signed, err := url.Parse(upload.data("uploadUrl").(string))
		require.NoError(t, err)
		require.Contains(t, signed.Path, "/"+testBucket+"/pending/videos/"+classID+"/")
		require.NotEmpty(t, signed.Query().Get("X-Amz-Signature"))

		early := add(guru.Token, classID, videoURL)
		early.requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		require.Equal(t, "has no uploaded file yet", dig(early.Body, "details", "videoUrl"))

		sendFile(t, upload, "video/mp4", 2048)

		stagedKey := strings.TrimPrefix(videoURL, testPublicURL+"/")
		require.Contains(t, uploaded("pending/videos/"+classID+"/"), stagedKey)

		added := add(guru.Token, classID, videoURL)
		require.Equal(t, http.StatusCreated, added.Status, "body %v", added.Body)

		// the file moved from pending/ to its permanent key, which is the address the video keeps
		permanentURL := testPublicURL + "/" + strings.TrimPrefix(stagedKey, "pending/")
		require.Equal(t, permanentURL, added.data("videoUrl"))
		require.Contains(t, uploaded("videos/"+classID+"/"), strings.TrimPrefix(stagedKey, "pending/"))
		require.NotContains(t, uploaded("pending/videos/"+classID+"/"), stagedKey)

		again := add(guru.Token, classID, videoURL)
		again.requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		require.Equal(t, "has no uploaded file yet", dig(again.Body, "details", "videoUrl"), "a staged file is added once")

		reused := add(guru.Token, classID, permanentURL)
		require.Equal(t, http.StatusCreated, reused.Status, "the permanent address can be added again, body %v", reused.Body)

		listed := call(t, http.MethodGet, fmt.Sprintf("/api/v1/classes/%s/videos", classID), wali.Token, nil)
		require.Equal(t, http.StatusOK, listed.Status, "body %v", listed.Body)
		require.Equal(t, []string{"Recorded lesson", "Recorded lesson"}, titles(t, listOf(t, listed, "videos"), "title"))
	})

	t.Run("a webm file is accepted too", func(t *testing.T) {
		upload := sign(guru.Token, classID, map[string]any{"contentType": "video/webm", "sizeBytes": 10})
		require.Equal(t, http.StatusOK, upload.Status, "body %v", upload.Body)
		require.True(t, strings.HasSuffix(upload.data("videoUrl").(string), ".webm"))
	})

	t.Run("a file that is not a video is refused when it is added", func(t *testing.T) {
		upload := sign(guru.Token, classID, map[string]any{"contentType": "video/mp4", "sizeBytes": 64})
		require.Equal(t, http.StatusOK, upload.Status, "body %v", upload.Body)

		sendFile(t, upload, "image/png", 64)

		res := add(guru.Token, classID, upload.data("videoUrl").(string))
		res.requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		require.Equal(t, "is not an accepted video file", dig(res.Body, "details", "videoUrl"))
	})

	t.Run("a file of another class or another kind cannot be added", func(t *testing.T) {
		theirs := sign(otherGuru.Token, otherClassID, map[string]any{"contentType": "video/mp4", "sizeBytes": 16})
		require.Equal(t, http.StatusOK, theirs.Status, "body %v", theirs.Body)
		sendFile(t, theirs, "video/mp4", 16)

		for name, videoURL := range map[string]string{
			"another class": theirs.data("videoUrl").(string),
			"an avatar":     testPublicURL + "/avatars/" + uuid.NewString() + "/a.png",
		} {
			res := add(guru.Token, classID, videoURL)
			res.requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
			require.Equal(t, "is not a file uploaded for this class", dig(res.Body, "details", "videoUrl"), name)
		}
	})

	t.Run("a link to another site still works", func(t *testing.T) {
		res := add(guru.Token, classID, "https://videos.example.com/lesson.mp4")
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)
	})

	t.Run("the request is checked", func(t *testing.T) {
		tooBig := int64(101) * 1024 * 1024

		for _, body := range []map[string]any{
			{},
			{"contentType": "video/x-msvideo", "sizeBytes": 10},
			{"contentType": "image/png", "sizeBytes": 10},
			{"contentType": "video/mp4"},
			{"contentType": "video/mp4", "sizeBytes": -1},
			{"contentType": "video/mp4", "sizeBytes": tooBig},
			{"contentType": "video/mp4", "sizeBytes": "big"},
		} {
			sign(guru.Token, classID, body).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		}

		sign(guru.Token, "not-a-uuid", map[string]any{"contentType": "video/mp4", "sizeBytes": 10}).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
	})

	t.Run("only a teacher of the class signs an upload", func(t *testing.T) {
		body := map[string]any{"contentType": "video/mp4", "sizeBytes": 10}

		sign(wali.Token, classID, body).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		sign(otherGuru.Token, classID, body).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		sign("", classID, body).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
	})
}

func TestEditAndDeleteVideos(t *testing.T) {
	guru := registerGuru(t, "Flamboyan")
	classID, joinCode := classOf(t, guru)
	wali, _ := registerWali(t, joinCode, "Flamboyan Child")

	otherGuru := registerGuru(t, "Gardenia")
	otherClassID, _ := classOf(t, otherGuru)

	videos := fmt.Sprintf("/api/v1/classes/%s/videos", classID)

	add := func(videoURL string) string {
		res := call(t, http.MethodPost, videos, guru.Token, map[string]any{
			"title": "Lesson", "description": "First version", "videoUrl": videoURL, "durationSeconds": 60,
		})
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

		return res.data("id").(string)
	}

	t.Run("change the details", func(t *testing.T) {
		id := add("https://videos.example.com/edit.mp4")

		res := call(t, http.MethodPatch, videos+"/"+id, guru.Token, map[string]any{
			"title": " Better lesson ", "thumbnailUrl": "https://videos.example.com/edit.jpg", "durationSeconds": 90,
		})
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, "Learning video updated", res.Body["message"])
		require.Equal(t, "Better lesson", res.data("title"))
		require.Equal(t, "First version", res.data("description"), "a missing field stays")
		require.Equal(t, "https://videos.example.com/edit.jpg", res.data("thumbnailUrl"))
		require.EqualValues(t, 90, res.data("durationSeconds"))
		require.Equal(t, "https://videos.example.com/edit.mp4", res.data("videoUrl"))

		cleared := call(t, http.MethodPatch, videos+"/"+id, guru.Token, map[string]any{
			"description": "", "thumbnailUrl": "", "durationSeconds": 0,
		})
		require.Equal(t, http.StatusOK, cleared.Status, "body %v", cleared.Body)
		require.Nil(t, cleared.data("description"))
		require.Nil(t, cleared.data("thumbnailUrl"))
		require.Nil(t, cleared.data("durationSeconds"))
		require.Equal(t, "Better lesson", cleared.data("title"))

		listed := call(t, http.MethodGet, videos, wali.Token, nil)
		require.Equal(t, http.StatusOK, listed.Status, "body %v", listed.Body)
		require.Contains(t, titles(t, listOf(t, listed, "videos"), "title"), "Better lesson")
	})

	t.Run("bad changes are refused", func(t *testing.T) {
		id := add("https://videos.example.com/bad.mp4")

		for _, body := range []map[string]any{
			{},
			{"title": "   "},
			{"title": strings.Repeat("a", 151)},
			{"thumbnailUrl": "http://videos.example.com/a.jpg"},
			{"durationSeconds": 86401},
			{"durationSeconds": -1},
			{"description": strings.Repeat("a", 2001)},
		} {
			call(t, http.MethodPatch, videos+"/"+id, guru.Token, body).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		}

		call(t, http.MethodPatch, videos+"/not-a-uuid", guru.Token, map[string]any{"title": "x"}).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
	})

	t.Run("a video that is not in the class is not found", func(t *testing.T) {
		theirs := call(t, http.MethodPost, fmt.Sprintf("/api/v1/classes/%s/videos", otherClassID), otherGuru.Token,
			map[string]any{"title": "Theirs", "videoUrl": "https://videos.example.com/theirs.mp4"})
		require.Equal(t, http.StatusCreated, theirs.Status, "body %v", theirs.Body)

		for _, id := range []string{uuid.NewString(), theirs.data("id").(string)} {
			call(t, http.MethodPatch, videos+"/"+id, guru.Token, map[string]any{"title": "x"}).
				requireError(t, http.StatusNotFound, "VIDEO_NOT_FOUND")
			call(t, http.MethodDelete, videos+"/"+id, guru.Token, nil).requireError(t, http.StatusNotFound, "VIDEO_NOT_FOUND")
		}
	})

	t.Run("only a teacher of the class changes or deletes a video", func(t *testing.T) {
		id := add("https://videos.example.com/guarded.mp4")

		for _, token := range []string{wali.Token, otherGuru.Token} {
			call(t, http.MethodPatch, videos+"/"+id, token, map[string]any{"title": "x"}).
				requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
			call(t, http.MethodDelete, videos+"/"+id, token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		}

		call(t, http.MethodPatch, videos+"/"+id, "", map[string]any{"title": "x"}).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
		call(t, http.MethodDelete, videos+"/"+id, "", nil).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
		call(t, http.MethodDelete, videos+"/not-a-uuid", guru.Token, nil).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
	})

	t.Run("deleting a link only forgets it", func(t *testing.T) {
		id := add("https://videos.example.com/gone.mp4")

		res := call(t, http.MethodDelete, videos+"/"+id, guru.Token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, "Learning video deleted", res.Body["message"])

		call(t, http.MethodDelete, videos+"/"+id, guru.Token, nil).requireError(t, http.StatusNotFound, "VIDEO_NOT_FOUND")

		listed := call(t, http.MethodGet, videos, guru.Token, nil)
		for _, item := range listOf(t, listed, "videos") {
			require.NotEqual(t, id, item.(map[string]any)["id"])
		}
	})

	t.Run("the uploaded file goes with its last video", func(t *testing.T) {
		upload := call(t, http.MethodPost, videos+"/upload-url", guru.Token, map[string]any{"contentType": "video/mp4", "sizeBytes": 128})
		require.Equal(t, http.StatusOK, upload.Status, "body %v", upload.Body)
		sendFile(t, upload, "video/mp4", 128)

		stagedURL := upload.data("videoUrl").(string)
		require.Contains(t, uploaded("pending/videos/"+classID+"/"), strings.TrimPrefix(stagedURL, testPublicURL+"/"))

		// the first video moves the file out of pending/ and a second one shares the permanent file
		first := add(stagedURL)

		fileURL := testPublicURL + "/" + strings.TrimPrefix(strings.TrimPrefix(stagedURL, testPublicURL+"/"), "pending/")
		key := strings.TrimPrefix(fileURL, testPublicURL+"/")
		require.Contains(t, uploaded("videos/"+classID+"/"), key)

		second := add(fileURL)

		require.Equal(t, http.StatusOK, call(t, http.MethodDelete, videos+"/"+first, guru.Token, nil).Status)
		require.Contains(t, uploaded("videos/"+classID+"/"), key, "another video still uses the file")

		require.Equal(t, http.StatusOK, call(t, http.MethodDelete, videos+"/"+second, guru.Token, nil).Status)
		require.NotContains(t, uploaded("videos/"+classID+"/"), key)
	})
}

func TestForumEditAndDelete(t *testing.T) {
	guru := registerGuru(t, "Heliconia")
	classID, joinCode := classOf(t, guru)
	author, _ := registerWali(t, joinCode, "Heliconia Child One")
	neighbour, _ := registerWali(t, joinCode, "Heliconia Child Two")

	otherGuru := registerGuru(t, "Iris")
	_, otherCode := classOf(t, otherGuru)
	outsider, _ := registerWali(t, otherCode, "Iris Child")

	forum := fmt.Sprintf("/api/v1/classes/%s/forum", classID)

	thread := func(token string, title string) string {
		res := call(t, http.MethodPost, forum, token, map[string]any{"title": title, "body": "Original text"})
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

		return res.data("id").(string)
	}

	reply := func(token string, threadID string, body string) string {
		res := call(t, http.MethodPost, forum+"/"+threadID+"/replies", token, map[string]any{"body": body})
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

		return res.data("id").(string)
	}

	t.Run("the author changes a thread", func(t *testing.T) {
		id := thread(author.Token, "First title")

		res := call(t, http.MethodPatch, forum+"/"+id, author.Token, map[string]any{"title": " Second title ", "body": "Changed text"})
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, "Forum thread updated", res.Body["message"])
		require.Equal(t, "Second title", res.data("title"))
		require.Equal(t, "Changed text", res.data("body"))

		onlyBody := call(t, http.MethodPatch, forum+"/"+id, author.Token, map[string]any{"body": "Third text"})
		require.Equal(t, http.StatusOK, onlyBody.Status, "body %v", onlyBody.Body)
		require.Equal(t, "Second title", onlyBody.data("title"), "a missing field stays")

		listed := call(t, http.MethodGet, forum, guru.Token, nil)
		require.Contains(t, titles(t, listOf(t, listed, "threads"), "title"), "Second title")
	})

	t.Run("a bad thread change is refused", func(t *testing.T) {
		id := thread(author.Token, "Checked")

		for _, body := range []map[string]any{
			{},
			{"title": "   "},
			{"body": ""},
			{"title": strings.Repeat("a", 151)},
			{"body": strings.Repeat("a", 5001)},
		} {
			call(t, http.MethodPatch, forum+"/"+id, author.Token, body).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		}

		call(t, http.MethodPatch, forum+"/not-a-uuid", author.Token, map[string]any{"body": "x"}).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
	})

	t.Run("only the author changes the words", func(t *testing.T) {
		id := thread(author.Token, "Mine")
		replyID := reply(guru.Token, id, "A teacher answer")

		for _, token := range []string{neighbour.Token, guru.Token} {
			call(t, http.MethodPatch, forum+"/"+id, token, map[string]any{"body": "x"}).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		}

		call(t, http.MethodPatch, forum+"/"+id+"/replies/"+replyID, author.Token, map[string]any{"body": "x"}).
			requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")

		call(t, http.MethodPatch, forum+"/"+id, outsider.Token, map[string]any{"body": "x"}).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodPatch, forum+"/"+id, "", map[string]any{"body": "x"}).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
	})

	t.Run("the author changes a reply", func(t *testing.T) {
		id := thread(neighbour.Token, "Discussion")
		replyID := reply(author.Token, id, "First answer")

		res := call(t, http.MethodPatch, forum+"/"+id+"/replies/"+replyID, author.Token, map[string]any{"body": " Better answer "})
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, "Forum reply updated", res.Body["message"])
		require.Equal(t, "Better answer", res.data("body"))

		call(t, http.MethodPatch, forum+"/"+id+"/replies/"+replyID, author.Token, map[string]any{"body": "  "}).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodPatch, forum+"/"+id+"/replies/not-a-uuid", author.Token, map[string]any{"body": "x"}).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
	})

	t.Run("a post that is not where the path says is not found", func(t *testing.T) {
		first, second := thread(author.Token, "One"), thread(author.Token, "Two")
		replyID := reply(author.Token, first, "Under one")

		call(t, http.MethodPatch, forum+"/"+uuid.NewString(), author.Token, map[string]any{"body": "x"}).
			requireError(t, http.StatusNotFound, "FORUM_POST_NOT_FOUND")
		call(t, http.MethodPatch, forum+"/"+replyID, author.Token, map[string]any{"body": "x"}).
			requireError(t, http.StatusNotFound, "FORUM_POST_NOT_FOUND")
		call(t, http.MethodPatch, forum+"/"+second+"/replies/"+replyID, author.Token, map[string]any{"body": "x"}).
			requireError(t, http.StatusNotFound, "FORUM_POST_NOT_FOUND")
		call(t, http.MethodPatch, forum+"/"+first+"/replies/"+second, author.Token, map[string]any{"body": "x"}).
			requireError(t, http.StatusNotFound, "FORUM_POST_NOT_FOUND")
		call(t, http.MethodDelete, forum+"/"+second+"/replies/"+replyID, author.Token, nil).
			requireError(t, http.StatusNotFound, "FORUM_POST_NOT_FOUND")
		call(t, http.MethodDelete, forum+"/"+replyID, author.Token, nil).requireError(t, http.StatusNotFound, "FORUM_POST_NOT_FOUND")
	})

	t.Run("the author and a teacher delete a reply", func(t *testing.T) {
		id := thread(author.Token, "Replies")
		own := reply(author.Token, id, "Mine")
		theirs := reply(neighbour.Token, id, "Theirs")
		moderated := reply(neighbour.Token, id, "Against the rules")

		call(t, http.MethodDelete, forum+"/"+id+"/replies/"+theirs, author.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")

		res := call(t, http.MethodDelete, forum+"/"+id+"/replies/"+own, author.Token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, "Forum reply deleted", res.Body["message"])

		require.Equal(t, http.StatusOK, call(t, http.MethodDelete, forum+"/"+id+"/replies/"+moderated, guru.Token, nil).Status)
		call(t, http.MethodDelete, forum+"/"+id+"/replies/"+moderated, guru.Token, nil).requireError(t, http.StatusNotFound, "FORUM_POST_NOT_FOUND")

		left := call(t, http.MethodGet, forum+"/"+id+"/replies", author.Token, nil)
		require.Equal(t, http.StatusOK, left.Status, "body %v", left.Body)
		require.Equal(t, []string{"Theirs"}, titles(t, listOf(t, left, "replies"), "body"))
	})

	t.Run("deleting a thread takes its replies along", func(t *testing.T) {
		id := thread(author.Token, "Short lived")
		reply(neighbour.Token, id, "Soon gone")

		call(t, http.MethodDelete, forum+"/"+id, neighbour.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodDelete, forum+"/"+id, outsider.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodDelete, forum+"/"+id, "", nil).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
		call(t, http.MethodDelete, forum+"/not-a-uuid", author.Token, nil).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")

		res := call(t, http.MethodDelete, forum+"/"+id, author.Token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, "Forum thread deleted", res.Body["message"])

		call(t, http.MethodGet, forum+"/"+id+"/replies", author.Token, nil).requireError(t, http.StatusNotFound, "FORUM_POST_NOT_FOUND")
		call(t, http.MethodDelete, forum+"/"+id, author.Token, nil).requireError(t, http.StatusNotFound, "FORUM_POST_NOT_FOUND")
		require.NotContains(t, titles(t, listOf(t, call(t, http.MethodGet, forum, guru.Token, nil), "threads"), "title"), "Short lived")
	})

	t.Run("a teacher removes a thread of a parent", func(t *testing.T) {
		id := thread(author.Token, "Off topic")

		require.Equal(t, http.StatusOK, call(t, http.MethodDelete, forum+"/"+id, guru.Token, nil).Status)
	})
}
