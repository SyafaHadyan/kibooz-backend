package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// eventsOf reads one page of a history route and returns its events and the total
func eventsOf(t *testing.T, path string, token string) ([]map[string]any, int) {
	t.Helper()

	res := call(t, http.MethodGet, path, token, nil)
	require.Equal(t, http.StatusOK, res.Status, "path %s body %v", path, res.Body)

	events := make([]map[string]any, 0)

	for _, item := range listOf(t, res, "events") {
		events = append(events, item.(map[string]any))
	}

	total, ok := res.data("total").(float64)
	require.True(t, ok, "total must be a number, body %v", res.Body)

	return events, int(total)
}

// walkPages reads every page of a history route and returns the ids in the order that the pages gave them, and the
// times of the events in the same order
func walkPages(t *testing.T, path string, token string, limit int, wantTotal int) ([]string, []time.Time) {
	t.Helper()

	var (
		ids   []string
		times []time.Time
	)

	for page := 1; page <= (wantTotal+limit-1)/limit+1; page++ {
		events, total := eventsOf(t, fmt.Sprintf("%s%spage=%d&limit=%d", path, joiner(path), page, limit), token)
		require.Equal(t, wantTotal, total, "page %d must report the same total", page)

		if page > (wantTotal+limit-1)/limit {
			require.Empty(t, events, "a page past the end has no events")

			break
		}

		for _, event := range events {
			at, err := time.Parse(time.RFC3339Nano, event["occurredAt"].(string))
			require.NoError(t, err)

			ids = append(ids, event["id"].(string))
			times = append(times, at)
		}
	}

	return ids, times
}

func joiner(path string) string {
	for _, char := range path {
		if char == '?' {
			return "&"
		}
	}

	return "?"
}

// requireNewestFirst checks the order of a timeline. The time never goes up, and events with the same time go by id,
// largest first, which is the tie break that keeps a page boundary from skipping or repeating an event.
func requireNewestFirst(t *testing.T, ids []string, times []time.Time) {
	t.Helper()

	for i := 1; i < len(ids); i++ {
		require.False(t, times[i].After(times[i-1]), "event %d is newer than the one before it", i)

		if times[i].Equal(times[i-1]) {
			require.Less(t, ids[i], ids[i-1], "events with the same time go by id, largest first, at position %d", i)
		}
	}
}

func logMoodAt(t *testing.T, token string, studentID string, mood string, at time.Time) {
	t.Helper()

	body := map[string]any{"studentId": studentID, "moodType": mood}
	if !at.IsZero() {
		body["recordedAt"] = at.UTC().Format(time.RFC3339Nano)
	}

	res := call(t, http.MethodPost, moodLogRoute, token, body)
	require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)
}

func batchMoodsAt(t *testing.T, token string, classID string, at time.Time, studentIDs ...string) {
	t.Helper()

	entries := make([]map[string]any, 0, len(studentIDs))
	for _, id := range studentIDs {
		entries = append(entries, batchEntry(id, "SENANG", 0.9))
	}

	res := call(t, http.MethodPost, moodBatchPath, token, map[string]any{
		"classId": classID, "recordedAt": at.UTC().Format(time.RFC3339Nano), "entries": entries,
	})
	require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)
}

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

func TestActivityHistoryOrderAndPagesWithEqualTimes(t *testing.T) {
	guru := registerGuru(t, "Urutan Riwayat")
	classID, joinCode := classOf(t, guru)
	wali, childID := registerWali(t, joinCode, "Anak Urutan Satu")
	_, secondID := registerWali(t, joinCode, "Anak Urutan Dua")
	_, thirdID := registerWali(t, joinCode, "Anak Urutan Tiga")

	// a batch gives every entry the same time, so two batches with the same time make six moods that only the id can order
	tied := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Millisecond)
	batchMoodsAt(t, guru.Token, classID, tied, childID, secondID, thirdID)
	batchMoodsAt(t, guru.Token, classID, tied, childID, secondID, thirdID)

	logMoodAt(t, guru.Token, childID, "SEDIH", time.Now().UTC().Add(-5*time.Hour))
	logMoodAt(t, guru.Token, childID, "SENANG", time.Time{})

	t.Run("a teacher reads every event once, newest first, whatever the page size", func(t *testing.T) {
		full, total := eventsOf(t, "/api/v1/guru/history?limit=50", guru.Token)
		require.Equal(t, 8, total)
		require.Len(t, full, 8)

		var want []string
		for _, event := range full {
			want = append(want, event["id"].(string))
		}

		for _, limit := range []int{1, 3, 5} {
			ids, times := walkPages(t, "/api/v1/guru/history", guru.Token, limit, 8)
			require.Equal(t, want, ids, "limit %d must give the same sequence as one big page", limit)
			requireNewestFirst(t, ids, times)
		}

		_, times := walkPages(t, "/api/v1/guru/history", guru.Token, 3, 8)
		require.True(t, times[0].After(tied), "the mood of now comes first")
		require.True(t, times[7].Before(tied), "the oldest mood comes last")
	})

	t.Run("a parent reads the child the same way", func(t *testing.T) {
		path := "/api/v1/wali/children/" + childID + "/history"

		full, total := eventsOf(t, path+"?limit=50", wali.Token)
		require.Equal(t, 4, total, "the child has two tied moods, one of now and one of five hours ago")

		var want []string
		for _, event := range full {
			want = append(want, event["id"].(string))
		}

		for _, limit := range []int{1, 2, 3} {
			ids, times := walkPages(t, path, wali.Token, limit, 4)
			require.Equal(t, want, ids, "limit %d must give the same sequence as one big page", limit)
			requireNewestFirst(t, ids, times)
		}
	})

	t.Run("a page past the end is empty and keeps the total", func(t *testing.T) {
		events, total := eventsOf(t, "/api/v1/guru/history?page=9&limit=50", guru.Token)
		require.Empty(t, events)
		require.Equal(t, 8, total)
	})
}

func TestActivityHistoryKeepsTheOldClassApartAfterAMove(t *testing.T) {
	teacherA := registerGuru(t, "Riwayat Lama")
	classA, codeA := classOf(t, teacherA)
	teacherB := registerGuru(t, "Riwayat Baru")
	classB, codeB := classOf(t, teacherB)

	parent, childID := registerWali(t, codeA, "Anak Pindah Riwayat")
	_, stayID := registerWali(t, codeA, "Anak Tinggal Riwayat")

	logMoodAt(t, teacherA.Token, childID, "SEDIH", time.Time{})
	logMoodAt(t, teacherA.Token, stayID, "SENANG", time.Time{})

	applied := call(t, http.MethodPost, "/api/v1/wali/guidance/apply", parent.Token, map[string]any{
		"guidanceId": "guidance-sedih-4step", "studentId": childID, "parentNotes": "Dilakukan di rumah",
	})
	require.Equal(t, http.StatusOK, applied.Status, "body %v", applied.Body)

	video := call(t, http.MethodPost, fmt.Sprintf("/api/v1/classes/%s/videos", classA), teacherA.Token, map[string]any{
		"title": "Video Kelas Lama", "videoUrl": "https://videos.example.com/lama.mp4",
	})
	require.Equal(t, http.StatusCreated, video.Status, "body %v", video.Body)

	before, total := eventsOf(t, "/api/v1/guru/history?classId="+classA, teacherA.Token)
	require.Equal(t, 4, total, "two moods, the guidance and the video")
	require.Len(t, before, 4)

	asked := requestMove(t, parent.Token, childID, codeB)
	require.Equal(t, http.StatusCreated, asked.Status, "body %v", asked.Body)

	accepted := call(t, http.MethodPost, fmt.Sprintf("/api/v1/guru/classes/%s/move-requests/%s/accept", classB, asked.data("id").(string)),
		teacherB.Token, nil)
	require.Equal(t, http.StatusOK, accepted.Status, "body %v", accepted.Body)

	t.Run("the old teacher no longer sees the child, the other child and the video stay", func(t *testing.T) {
		events, total := eventsOf(t, "/api/v1/guru/history?classId="+classA, teacherA.Token)
		require.Equal(t, 2, total)

		for _, event := range events {
			require.NotEqual(t, childID, event["studentId"], "the child left, so its events leave the class timeline")
			require.NotEqual(t, "guidance", event["type"], "the only guidance was the one of the child who left")
		}
	})

	t.Run("the new class begins empty and shows only what happens after the move", func(t *testing.T) {
		events, total := eventsOf(t, "/api/v1/guru/history?classId="+classB, teacherB.Token)
		require.Equal(t, 0, total)
		require.Empty(t, events)

		logMoodAt(t, teacherB.Token, childID, "SENANG", time.Time{})

		events, total = eventsOf(t, "/api/v1/guru/history?classId="+classB, teacherB.Token)
		require.Equal(t, 1, total)
		require.Equal(t, childID, events[0]["studentId"])
		require.Equal(t, classB, events[0]["classId"])

		// and the old class does not get the new mood
		_, oldTotal := eventsOf(t, "/api/v1/guru/history?classId="+classA, teacherA.Token)
		require.Equal(t, 2, oldTotal)
	})

	t.Run("the parent sees the new class only", func(t *testing.T) {
		events, total := eventsOf(t, "/api/v1/wali/children/"+childID+"/history", parent.Token)
		require.Equal(t, 1, total)
		require.Len(t, events, 1)
		require.Equal(t, "mood", events[0]["type"])
	})
}

func TestActivityHistoryOfAnArchivedClassStaysReadable(t *testing.T) {
	owner := registerGuru(t, "Riwayat Arsip")
	classID, joinCode := classOf(t, owner)
	parent, childID := registerWali(t, joinCode, "Anak Riwayat Arsip")

	logMoodAt(t, owner.Token, childID, "SENANG", time.Time{})

	applied := call(t, http.MethodPost, "/api/v1/wali/guidance/apply", parent.Token, map[string]any{
		"guidanceId": "guidance-senang-4step", "studentId": childID,
	})
	require.Equal(t, http.StatusOK, applied.Status, "body %v", applied.Body)

	archived := call(t, http.MethodPost, "/api/v1/guru/classes/"+classID+"/archive", owner.Token, nil)
	require.Equal(t, http.StatusOK, archived.Status, "body %v", archived.Body)

	for _, path := range []string{"/api/v1/guru/history", "/api/v1/guru/history?classId=" + classID} {
		events, total := eventsOf(t, path, owner.Token)
		require.Equal(t, 2, total, path)
		require.Len(t, events, 2, path)
	}

	events, total := eventsOf(t, "/api/v1/wali/children/"+childID+"/history", parent.Token)
	require.Equal(t, 2, total)
	require.Len(t, events, 2)
}

func TestActivityHistoryAfterATeacherLeavesTheClassOrTheAccount(t *testing.T) {
	owner := registerGuru(t, "Riwayat Guru")
	classID, joinCode := classOf(t, owner)
	partner := registerGuru(t, "Riwayat Mitra")
	_, firstID := registerWali(t, joinCode, "Anak Guru Satu")
	_, secondID := registerWali(t, joinCode, "Anak Guru Dua")

	base := "/api/v1/guru/classes/" + classID

	added := call(t, http.MethodPost, base+"/teachers", owner.Token, map[string]any{"email": partner.Email})
	require.Equal(t, http.StatusCreated, added.Status, "body %v", added.Body)

	partnerTeacherID := added.data("id").(string)

	logMoodAt(t, owner.Token, firstID, "SENANG", time.Time{})
	logMoodAt(t, partner.Token, secondID, "SEDIH", time.Time{})

	actorOf := func(t *testing.T, events []map[string]any, studentID string) any {
		t.Helper()

		for _, event := range events {
			if event["studentId"] == studentID {
				return event["actorName"]
			}
		}

		t.Fatalf("no event for the child %s", studentID)

		return nil
	}

	t.Run("a teacher who was removed from the class cannot read it but the events stay", func(t *testing.T) {
		removed := call(t, http.MethodDelete, base+"/teachers/"+partnerTeacherID, owner.Token, nil)
		require.Equal(t, http.StatusOK, removed.Status, "body %v", removed.Body)

		call(t, http.MethodGet, "/api/v1/guru/history?classId="+classID, partner.Token, nil).
			requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")

		events, total := eventsOf(t, "/api/v1/guru/history", owner.Token)
		require.Equal(t, 2, total)
		require.Equal(t, "Siti Rahayu, S.Pd.", actorOf(t, events, secondID), "the mood of the removed teacher keeps the name of the teacher")
	})

	t.Run("a deleted account shows as Deleted account and nothing else of the person", func(t *testing.T) {
		res := deleteTeacher(t, partner)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		events, total := eventsOf(t, "/api/v1/guru/history", owner.Token)
		require.Equal(t, 2, total, "the events of a deleted account stay")
		require.Equal(t, "Deleted account", actorOf(t, events, secondID))
		require.Equal(t, "Siti Rahayu, S.Pd.", actorOf(t, events, firstID), "a live teacher keeps the name")
	})
}
