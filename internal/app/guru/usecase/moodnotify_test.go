package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/kiboozapp/kibooz-backend/internal/apperror"
	"github.com/kiboozapp/kibooz-backend/internal/constants"
	"github.com/kiboozapp/kibooz-backend/internal/domain/dto"
	"github.com/kiboozapp/kibooz-backend/internal/domain/entity"
	"github.com/kiboozapp/kibooz-backend/internal/infra/env"
	"github.com/kiboozapp/kibooz-backend/internal/notifier"
)

// spyNotifier keeps what the use case asked to send
type spyNotifier struct {
	notifier.Notifier

	single  []string
	batches [][]notifier.StudentNotice
	kinds   []string
}

func (s *spyNotifier) NotifyStudentParent(
	_ context.Context, _ uuid.UUID, kind string, _ string, body string, _ string, _ uuid.UUID,
) {
	s.kinds = append(s.kinds, kind)
	s.single = append(s.single, body)
}

func (s *spyNotifier) NotifyStudentParents(_ context.Context, kind string, _ string, notices []notifier.StudentNotice) {
	s.kinds = append(s.kinds, kind)
	s.batches = append(s.batches, notices)
}

func named(repo *batchRepo, fullName string) uuid.UUID {
	id := repo.enroll()
	repo.students[id].FullName = fullName

	return id
}

func TestMoodNoticeNamesTheChildByFirstName(t *testing.T) {
	logID := uuid.New()

	for name, want := range map[string]string{
		"Amelia Putri":    "Teacher recorded mood for Amelia: Happy / Cheerful",
		"  Budi  Santoso": "Teacher recorded mood for Budi: Happy / Cheerful",
		"Citra":           "Teacher recorded mood for Citra: Happy / Cheerful",
		"":                "Teacher recorded mood for your child: Happy / Cheerful",
		"   ":             "Teacher recorded mood for your child: Happy / Cheerful",
	} {
		student := &entity.Student{ID: uuid.New(), FullName: name}
		notice := moodNotice(student, constants.MoodSenang, logID)

		require.Equal(t, want, notice.Body, "name %q", name)
		require.Equal(t, student.ID, notice.StudentID)
		require.Equal(t, logID, notice.RefID)
		require.Equal(t, "Daily mood recorded", notice.Title)
	}
}

func TestLogMoodNotifiesTheParentOfTheChildByName(t *testing.T) {
	repo := batching()
	student := named(repo, "Amelia Putri")
	spy := &spyNotifier{}

	useCase := &GuruUseCase{
		repo: teachingAll{repo}, cfg: &env.Env{AppTimezone: "Asia/Jakarta"}, now: time.Now, notifier: spy,
	}

	_, err := useCase.LogMood(context.Background(), uuid.New(), dto.LogMoodRequest{StudentID: student, MoodType: constants.MoodSenang})

	require.NoError(t, err)
	require.Equal(t, []string{"Teacher recorded mood for Amelia: Happy / Cheerful"}, spy.single)
	require.Empty(t, spy.batches)
}

func TestLogMoodBatchNotifiesTheParentsInOneCall(t *testing.T) {
	repo := batching()
	first, second := named(repo, "Amelia Putri"), named(repo, "")
	spy := &spyNotifier{}

	useCase := batchUseCase(repo, time.Now())
	useCase.notifier = spy

	res, err := useCase.LogMoodBatch(context.Background(), uuid.New(), dto.LogMoodBatchRequest{
		ClassID: repo.class.ID,
		Entries: []dto.LogMoodBatchEntry{aiEntry(first, constants.MoodSenang, 0.9), aiEntry(second, constants.MoodSedih, 0.8)},
	})

	require.NoError(t, err)
	require.Empty(t, spy.single, "a batch never makes one call for each entry")
	require.Len(t, spy.batches, 1)
	require.Equal(t, []string{constants.NotificationMoodLogged}, spy.kinds)

	notices := spy.batches[0]
	require.Len(t, notices, 2)
	require.Equal(t, first, notices[0].StudentID)
	require.Equal(t, res.Logs[0].LogID, notices[0].RefID)
	require.Equal(t, "Teacher recorded mood for Amelia: Happy / Cheerful", notices[0].Body)
	require.Equal(t, second, notices[1].StudentID)
	require.Equal(t, res.Logs[1].LogID, notices[1].RefID)
	require.Equal(t, "Teacher recorded mood for your child: Sad / Needs Cheering Up", notices[1].Body)
}

func TestNoNotificationGoesOutWhenTheLogsAreRefused(t *testing.T) {
	repo := batching()
	repo.createErr = apperror.ErrMoodLimitReached
	student := named(repo, "Amelia Putri")
	spy := &spyNotifier{}

	useCase := batchUseCase(repo, time.Now())
	useCase.notifier = spy

	_, err := useCase.LogMoodBatch(context.Background(), uuid.New(), dto.LogMoodBatchRequest{
		ClassID: repo.class.ID,
		Entries: []dto.LogMoodBatchEntry{aiEntry(student, constants.MoodSenang, 0.9)},
	})

	require.ErrorIs(t, err, apperror.ErrMoodLimitReached)
	require.Empty(t, spy.kinds)
}
