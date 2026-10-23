package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/guru/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
)

const moodBatchPath = "/api/v1/guru/mood/log/batch"

func moodLogCount(t *testing.T, studentID string) int64 {
	t.Helper()

	var total int64

	require.NoError(t, testDB.Raw("SELECT COUNT(*) FROM mood_logs WHERE student_id = ?", studentID).Scan(&total).Error)

	return total
}

func batchEntry(studentID string, mood string, score float64) map[string]any {
	return map[string]any{"studentId": studentID, "moodType": mood, "source": "AI_CAMERA", "confidenceScore": score}
}

func requireDetail(t *testing.T, res result, key string) {
	t.Helper()

	details, ok := res.Body["details"].(map[string]any)
	require.True(t, ok, "body %v", res.Body)
	require.Contains(t, details, key, "body %v", res.Body)
}

func TestMoodBatch(t *testing.T) {
	guru := registerGuru(t, "Anggrek")
	classID, joinCode := classOf(t, guru)

	amelia, ameliaID := registerWali(t, joinCode, "Amelia Siti Zahra")
	farhan, farhanID := registerWali(t, joinCode, "Farhan Al-Fatih")
	_, zahraID := registerWali(t, joinCode, "Zahra Putri")

	otherGuru := registerGuru(t, "Cempaka")
	otherClassID, otherCode := classOf(t, otherGuru)
	_, foreignID := registerWali(t, otherCode, "Other Child")

	t.Run("saves every entry and the dashboards count them", func(t *testing.T) {
		res := call(t, http.MethodPost, moodBatchPath, guru.Token, map[string]any{
			"classId": classID,
			"entries": []map[string]any{
				batchEntry(ameliaID, "BINGUNG", 0.88),
				{"studentId": farhanID, "moodType": "SENANG", "source": "MANUAL_INPUT", "notes": "  Smiling at the door.  "},
				batchEntry(zahraID, "SEDIH", 0.91),
			},
		})
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)
		require.Equal(t, "Mood logs saved", res.Body["message"])

		logs := listOf(t, res, "logs")
		require.Len(t, logs, 3)
		require.Equal(t, []any{ameliaID, farhanID, zahraID}, []any{dig(logs[0], "studentId"), dig(logs[1], "studentId"), dig(logs[2], "studentId")})
		require.Equal(t, dig(logs[0], "recordedAt"), dig(logs[2], "recordedAt"), "one shared recordedAt")

		for _, studentID := range []string{ameliaID, farhanID, zahraID} {
			require.EqualValues(t, 1, moodLogCount(t, studentID))
		}

		var stored struct {
			Source          string
			ConfidenceScore float32
			Notes           string
		}

		require.NoError(t, testDB.Raw("SELECT source, confidence_score, notes FROM mood_logs WHERE student_id = ?", farhanID).Scan(&stored).Error)
		require.Equal(t, "MANUAL_INPUT", stored.Source)
		require.InDelta(t, 1, stored.ConfidenceScore, 1e-6)
		require.Equal(t, "Smiling at the door.", stored.Notes)

		dashboard := call(t, http.MethodGet, "/api/v1/wali/dashboard", amelia.Token, nil)
		require.Equal(t, "BINGUNG", dashboard.data("todayMood", "moodType"))
		require.InDelta(t, 0.88, dashboard.data("todayMood", "confidence"), 0.001)

		overview := call(t, http.MethodGet, "/api/v1/guru/dashboard?classId="+classID, guru.Token, nil)
		require.EqualValues(t, 1, overview.data("dailyMoodDistribution", "bingung"))
		require.EqualValues(t, 1, overview.data("dailyMoodDistribution", "senang"))
		require.EqualValues(t, 1, overview.data("dailyMoodDistribution", "sedih"))
		require.EqualValues(t, 3, overview.data("classOverview", "presentStudents"))
	})

	t.Run("a rescan the same day supersedes the earlier entries", func(t *testing.T) {
		// the earlier batch and this one must not share a timestamp, or the latest one is a tie
		time.Sleep(20 * time.Millisecond)

		res := call(t, http.MethodPost, moodBatchPath, guru.Token, map[string]any{
			"classId": classID,
			"entries": []map[string]any{batchEntry(ameliaID, "SENANG", 0.95)},
		})
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)
		require.EqualValues(t, 2, moodLogCount(t, ameliaID), "the earlier entry stays as history")

		dashboard := call(t, http.MethodGet, "/api/v1/wali/dashboard", amelia.Token, nil)
		require.Equal(t, "SENANG", dashboard.data("todayMood", "moodType"))

		overview := call(t, http.MethodGet, "/api/v1/guru/dashboard?classId="+classID, guru.Token, nil)
		require.EqualValues(t, 0, overview.data("dailyMoodDistribution", "bingung"))
		require.EqualValues(t, 2, overview.data("dailyMoodDistribution", "senang"))
		require.EqualValues(t, 3, overview.data("classOverview", "presentStudents"), "a child still counts once")
	})

	t.Run("one bad entry saves nothing", func(t *testing.T) {
		before := moodLogCount(t, zahraID)

		res := call(t, http.MethodPost, moodBatchPath, guru.Token, map[string]any{
			"classId": classID,
			"entries": []map[string]any{
				batchEntry(zahraID, "MARAH", 0.7),
				batchEntry(foreignID, "SENANG", 0.9),
			},
		})
		res.requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		requireDetail(t, res, "entries[1].studentId")
		require.Equal(t, before, moodLogCount(t, zahraID))
	})

	t.Run("validation", func(t *testing.T) {
		tooMany := make([]map[string]any, constants.MoodBatchMax+1)
		for i := range tooMany {
			tooMany[i] = batchEntry(ameliaID, "SENANG", 0.9)
		}

		for name, tc := range map[string]struct {
			body map[string]any
			key  string
		}{
			"no entries":       {map[string]any{"classId": classID, "entries": []map[string]any{}}, "entries"},
			"too many entries": {map[string]any{"classId": classID, "entries": tooMany}, "entries"},
			"no class":         {map[string]any{"entries": []map[string]any{batchEntry(ameliaID, "SENANG", 0.9)}}, "classId"},
			"unknown mood": {
				map[string]any{"classId": classID, "entries": []map[string]any{batchEntry(ameliaID, "SENANG", 0.9), batchEntry(farhanID, "LAPAR", 0.9)}},
				"entries[1].moodType",
			},
			"score above one": {
				map[string]any{"classId": classID, "entries": []map[string]any{batchEntry(ameliaID, "SENANG", 1.5)}},
				"entries[0].confidenceScore",
			},
			"camera entry without a score": {
				map[string]any{"classId": classID, "entries": []map[string]any{{"studentId": ameliaID, "moodType": "SENANG"}}},
				"entries[0].confidenceScore",
			},
			"repeated child": {
				map[string]any{"classId": classID, "entries": []map[string]any{batchEntry(ameliaID, "SENANG", 0.9), batchEntry(ameliaID, "SEDIH", 0.9)}},
				"entries[1].studentId",
			},
			"child of another class": {
				map[string]any{"classId": classID, "entries": []map[string]any{batchEntry(foreignID, "SENANG", 0.9)}},
				"entries[0].studentId",
			},
			"unknown child": {
				map[string]any{"classId": classID, "entries": []map[string]any{batchEntry(uuid.NewString(), "SENANG", 0.9)}},
				"entries[0].studentId",
			},
		} {
			t.Run(name, func(t *testing.T) {
				res := call(t, http.MethodPost, moodBatchPath, guru.Token, tc.body)
				res.requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
				requireDetail(t, res, tc.key)
			})
		}
	})

	t.Run("access", func(t *testing.T) {
		body := map[string]any{"classId": classID, "entries": []map[string]any{batchEntry(farhanID, "SENANG", 0.9)}}
		before := moodLogCount(t, farhanID)

		call(t, http.MethodPost, moodBatchPath, "", body).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
		call(t, http.MethodPost, moodBatchPath, farhan.Token, body).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		// a teacher of another class gets the same answer as for a class that does not exist
		call(t, http.MethodPost, moodBatchPath, otherGuru.Token, body).requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")
		call(t, http.MethodPost, moodBatchPath, guru.Token, map[string]any{
			"classId": uuid.NewString(), "entries": body["entries"],
		}).requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")
		call(t, http.MethodPost, moodBatchPath, guru.Token, map[string]any{
			"classId": otherClassID, "entries": []map[string]any{batchEntry(foreignID, "SENANG", 0.9)},
		}).requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")

		require.Equal(t, before, moodLogCount(t, farhanID))
	})
}

// A database failure halfway through a batch must leave none of the rows behind. The use case checks everything first, so the
// only way to reach the transaction with a bad row is to call the repository, here with a mood that the database refuses.
func TestCreateMoodLogsRollsBackTheWholeBatch(t *testing.T) {
	guru := registerGuru(t, "Dahlia")
	classIDText, joinCode := classOf(t, guru)
	_, studentID := registerWali(t, joinCode, "Rollback Child")

	var guruRow string

	require.NoError(t, testDB.Raw(
		"SELECT g.id FROM gurus g JOIN users u ON u.id = g.user_id WHERE u.email = ?", guru.Email,
	).Scan(&guruRow).Error)

	guruID, err := uuid.Parse(guruRow)
	require.NoError(t, err)

	now := time.Now().UTC()
	valid := entity.MoodLog{
		ID: uuid.New(), StudentID: uuid.MustParse(studentID), RecordedByGuruID: guruID,
		MoodType: constants.MoodSenang, ConfidenceScore: 0.9, Source: constants.SourceAICamera, RecordedAt: now,
	}
	orphan := entity.MoodLog{
		ID: uuid.New(), StudentID: valid.StudentID, RecordedByGuruID: guruID,
		MoodType: constants.Mood("NOT_A_MOOD"), ConfidenceScore: 0.9, Source: constants.SourceAICamera, RecordedAt: now,
	}

	repo := repository.NewGuruDB(testDB, nil)

	classID := uuid.MustParse(classIDText)
	day := repository.MoodDay{From: now.Add(-24 * time.Hour), To: now.Add(24 * time.Hour), Limit: constants.MoodLogsPerStudentPerDay}

	_, err = repo.CreateMoodLogs(context.Background(), classID, []entity.MoodLog{valid, orphan}, day)
	require.Error(t, err, "the second row has a mood that the database refuses")
	require.Zero(t, moodLogCount(t, studentID), "the valid row %s must not stay behind", valid.ID)

	_, err = repo.CreateMoodLogs(context.Background(), classID, []entity.MoodLog{valid}, day)
	require.NoError(t, err)
	require.EqualValues(t, 1, moodLogCount(t, studentID))
}
