package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/guru/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
)

// The use case reads the child and checks the teacher before the transaction starts, so a move that is accepted in between
// leaves it with a stale class. The transaction has to find that, and write nothing for a child that is in another class now.
func TestAMoodIsNotWrittenForAChildThatMovedAfterTheTeacherWasChecked(t *testing.T) {
	teacherA := registerGuru(t, "Guru Lama Cek")
	classAText, codeA := classOf(t, teacherA)
	teacherB := registerGuru(t, "Guru Baru Cek")
	classB, codeB := classOf(t, teacherB)

	parent, movedID := registerWali(t, codeA, "Anak Sudah Pindah")
	_, stayingID := registerWali(t, codeA, "Anak Tetap")

	asked := requestMove(t, parent.Token, movedID, codeB)
	require.Equal(t, http.StatusCreated, asked.Status, "body %v", asked.Body)

	accepted := call(t, http.MethodPost, "/api/v1/guru/classes/"+classB+"/move-requests/"+asked.data("id").(string)+"/accept", teacherB.Token, nil)
	require.Equal(t, http.StatusOK, accepted.Status, "body %v", accepted.Body)

	var guruRow string

	require.NoError(t, testDB.Raw(
		"SELECT g.id FROM gurus g JOIN users u ON u.id = g.user_id WHERE u.email = ?", teacherA.Email,
	).Scan(&guruRow).Error)

	guruID, err := uuid.Parse(guruRow)
	require.NoError(t, err)

	classA := uuid.MustParse(classAText)
	now := time.Now().UTC()
	day := repository.MoodDay{From: now.Add(-24 * time.Hour), To: now.Add(24 * time.Hour), Limit: constants.MoodLogsPerStudentPerDay}
	repo := repository.NewGuruDB(testDB, nil)

	moodFor := func(studentID string) entity.MoodLog {
		return entity.MoodLog{
			ID: uuid.New(), StudentID: uuid.MustParse(studentID), RecordedByGuruID: guruID,
			MoodType: constants.MoodSenang, ConfidenceScore: 0.9, Source: constants.SourceAICamera, RecordedAt: now,
		}
	}

	// the call that the use case would have made with the class that it read before the move
	_, err = repo.CreateMoodLogs(context.Background(), classA, []entity.MoodLog{moodFor(movedID)}, day)
	require.ErrorIs(t, err, apperror.ErrStudentNotFound)
	require.Zero(t, moodLogCount(t, movedID), "nothing is written for a child that left the class")

	// one child of the batch that left holds back the whole batch, and the child that stayed gets nothing either
	_, err = repo.CreateMoodLogs(context.Background(), classA, []entity.MoodLog{moodFor(stayingID), moodFor(movedID)}, day)
	require.ErrorIs(t, err, apperror.ErrStudentNotFound)
	require.Zero(t, moodLogCount(t, stayingID))

	// a child that is still in the class is not held back
	_, err = repo.CreateMoodLogs(context.Background(), classA, []entity.MoodLog{moodFor(stayingID)}, day)
	require.NoError(t, err)
	require.EqualValues(t, 1, moodLogCount(t, stayingID))
}
