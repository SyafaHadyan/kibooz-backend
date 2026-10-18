package e2e

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
)

const moodLogRoute = "/api/v1/guru/mood/log"

func storedRecordedAt(t *testing.T, logID string) time.Time {
	t.Helper()

	var at time.Time

	require.NoError(t, testDB.Raw("SELECT recorded_at FROM mood_logs WHERE id = ?", logID).Scan(&at).Error)

	return at
}

func TestMoodLogsKeepTheTimeTheDeviceSent(t *testing.T) {
	guru := registerGuru(t, "Waktu Anggrek")
	classID, joinCode := classOf(t, guru)
	amelia, ameliaID := registerWali(t, joinCode, "Amelia Waktu")
	_, farhanID := registerWali(t, joinCode, "Farhan Waktu")

	t.Run("a replay of an older mood does not take the place of a newer one", func(t *testing.T) {
		live := call(t, http.MethodPost, moodLogRoute, guru.Token, map[string]any{"studentId": ameliaID, "moodType": "SENANG"})
		require.Equal(t, http.StatusCreated, live.Status, "body %v", live.Body)

		seen := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)

		replay := call(t, http.MethodPost, moodLogRoute, guru.Token, map[string]any{
			"studentId": ameliaID, "moodType": "SEDIH", "recordedAt": seen.Format(time.RFC3339Nano),
		})
		require.Equal(t, http.StatusCreated, replay.Status, "body %v", replay.Body)

		logID, _ := replay.data("logId").(string)
		require.WithinDuration(t, seen, storedRecordedAt(t, logID), time.Millisecond)

		got, err := time.Parse(time.RFC3339Nano, replay.data("recordedAt").(string))
		require.NoError(t, err)
		require.WithinDuration(t, seen, got, time.Millisecond)

		dashboard := call(t, http.MethodGet, "/api/v1/wali/dashboard", amelia.Token, nil)
		require.Equal(t, "SENANG", dashboard.data("todayMood", "moodType"), "the live mood is newer than the replayed one")
	})

	t.Run("a batch carries one time for all its entries", func(t *testing.T) {
		seen := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Millisecond)

		res := call(t, http.MethodPost, moodBatchPath, guru.Token, map[string]any{
			"classId":    classID,
			"recordedAt": seen.Format(time.RFC3339Nano),
			"entries":    []map[string]any{batchEntry(farhanID, "BINGUNG", 0.8)},
		})
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

		logs := listOf(t, res, "logs")
		require.Len(t, logs, 1)

		got, err := time.Parse(time.RFC3339Nano, dig(logs[0], "recordedAt").(string))
		require.NoError(t, err)
		require.WithinDuration(t, seen, got, time.Millisecond)
	})

	t.Run("a time that cannot be right is refused and stores nothing", func(t *testing.T) {
		before := moodLogCount(t, farhanID)
		future := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
		tooOld := time.Now().UTC().Add(-constants.MoodRecordedAtMaxAge - time.Hour).Format(time.RFC3339)

		for _, sent := range []string{future, tooOld} {
			single := call(t, http.MethodPost, moodLogRoute, guru.Token, map[string]any{
				"studentId": farhanID, "moodType": "SENANG", "recordedAt": sent,
			})
			single.requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
			requireDetail(t, single, "recordedAt")

			batch := call(t, http.MethodPost, moodBatchPath, guru.Token, map[string]any{
				"classId": classID, "recordedAt": sent, "entries": []map[string]any{batchEntry(farhanID, "SENANG", 0.9)},
			})
			batch.requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
			requireDetail(t, batch, "recordedAt")
		}

		call(t, http.MethodPost, moodLogRoute, guru.Token, map[string]any{
			"studentId": farhanID, "moodType": "SENANG", "recordedAt": "yesterday",
		}).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")

		require.Equal(t, before, moodLogCount(t, farhanID))
	})

	t.Run("a time a little ahead of the server counts as now", func(t *testing.T) {
		ahead := time.Now().UTC().Add(time.Minute)

		res := call(t, http.MethodPost, moodLogRoute, guru.Token, map[string]any{
			"studentId": farhanID, "moodType": "SENANG", "recordedAt": ahead.Format(time.RFC3339Nano),
		})
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

		logID, _ := res.data("logId").(string)
		require.False(t, storedRecordedAt(t, logID).After(time.Now().UTC()), "the stored time is not ahead of the server")
	})
}
