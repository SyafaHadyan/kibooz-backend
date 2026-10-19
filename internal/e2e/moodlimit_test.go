package e2e

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
)

func TestMoodLogsPerChildPerDayAreLimited(t *testing.T) {
	guru := registerGuru(t, "Batas Anggrek")
	classID, joinCode := classOf(t, guru)
	_, fullID := registerWali(t, joinCode, "Anak Penuh")
	_, freeID := registerWali(t, joinCode, "Anak Lega")

	single := func(studentID string, extra map[string]any) result {
		body := map[string]any{"studentId": studentID, "moodType": "SENANG"}
		for key, value := range extra {
			body[key] = value
		}

		return call(t, http.MethodPost, moodLogRoute, guru.Token, body)
	}

	for i := 0; i < constants.MoodLogsPerStudentPerDay; i++ {
		res := single(fullID, nil)
		require.Equal(t, http.StatusCreated, res.Status, "log %d, body %v", i+1, res.Body)
	}

	require.EqualValues(t, constants.MoodLogsPerStudentPerDay, moodLogCount(t, fullID))

	t.Run("one more log for that child on that day is refused", func(t *testing.T) {
		single(fullID, nil).requireError(t, http.StatusConflict, "MOOD_LOG_LIMIT_REACHED")
		require.EqualValues(t, constants.MoodLogsPerStudentPerDay, moodLogCount(t, fullID))
	})

	t.Run("another child of the class is not affected", func(t *testing.T) {
		require.Equal(t, http.StatusCreated, single(freeID, nil).Status)
	})

	t.Run("a log dated on another day is counted on that day", func(t *testing.T) {
		earlier := time.Now().UTC().Add(-72 * time.Hour).Format(time.RFC3339Nano)
		require.Equal(t, http.StatusCreated, single(fullID, map[string]any{"recordedAt": earlier}).Status)
	})

	t.Run("a batch names the child who has no room left and saves nothing", func(t *testing.T) {
		before := moodLogCount(t, freeID)

		res := call(t, http.MethodPost, moodBatchPath, guru.Token, map[string]any{
			"classId": classID,
			"entries": []map[string]any{batchEntry(fullID, "SENANG", 0.9), batchEntry(freeID, "SEDIH", 0.9)},
		})
		res.requireError(t, http.StatusConflict, "MOOD_LOG_LIMIT_REACHED")
		require.Contains(t, res.Body["details"], "entries[0].studentId")
		require.NotContains(t, res.Body["details"], "entries[1].studentId")
		require.Equal(t, before, moodLogCount(t, freeID), "the whole batch is refused, the child with room gets nothing")
	})

	t.Run("notes are at most 500 characters", func(t *testing.T) {
		single(freeID, map[string]any{"notes": strings.Repeat("a", 501)}).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		require.Equal(t, http.StatusCreated, single(freeID, map[string]any{"notes": strings.Repeat("a", 500)}).Status)

		call(t, http.MethodPost, moodBatchPath, guru.Token, map[string]any{
			"classId": classID,
			"entries": []map[string]any{{"studentId": freeID, "moodType": "SENANG", "source": "MANUAL_INPUT", "notes": strings.Repeat("b", 501)}},
		}).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
	})
}
