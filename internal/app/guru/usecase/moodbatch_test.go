package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/guru/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
)

// batchRepo knows a few students and keeps the mood logs the use case asked to store
type batchRepo struct {
	*teacherRepo

	students  map[uuid.UUID]*entity.Student
	createErr error

	createCalls int
	created     []entity.MoodLog
}

func (r *batchRepo) FindStudent(_ context.Context, id uuid.UUID) (*entity.Student, error) {
	return r.students[id], nil
}

func (r *batchRepo) CreateMoodLogs(_ context.Context, moodLogs []entity.MoodLog) error {
	r.createCalls++
	r.created = moodLogs

	return r.createErr
}

func batching() *batchRepo {
	base := teachingRepo()
	base.guru = &repository.GuruRow{Guru: entity.Guru{ID: uuid.New()}}

	return &batchRepo{teacherRepo: base, students: map[uuid.UUID]*entity.Student{}}
}

// enroll adds a student to the class of the repo
func (r *batchRepo) enroll() uuid.UUID {
	return r.enrollIn(r.class.ID)
}

func (r *batchRepo) enrollIn(classID uuid.UUID) uuid.UUID {
	student := &entity.Student{ID: uuid.New(), ClassID: classID}
	r.students[student.ID] = student

	return student.ID
}

func batchUseCase(repo *batchRepo, now time.Time) *GuruUseCase {
	return &GuruUseCase{repo: repo, now: func() time.Time { return now }}
}

func confidence(value float32) *float32 {
	return &value
}

func aiEntry(studentID uuid.UUID, mood constants.Mood, score float32) dto.LogMoodBatchEntry {
	return dto.LogMoodBatchEntry{StudentID: studentID, MoodType: mood, ConfidenceScore: confidence(score)}
}

func requireDetails(t *testing.T, err error, want map[string]string) {
	t.Helper()

	var got *apperror.Error

	require.ErrorAs(t, err, &got)
	require.Equal(t, "VALIDATION_ERROR", got.Code)
	require.Equal(t, want, got.Details)
}

func TestLogMoodBatchStoresEveryEntryWithOneTimestamp(t *testing.T) {
	repo := batching()
	first, second := repo.enroll(), repo.enroll()
	now := time.Date(2026, 3, 4, 8, 30, 15, 123456789, time.UTC)

	res, err := batchUseCase(repo, now).LogMoodBatch(context.Background(), uuid.New(), dto.LogMoodBatchRequest{
		ClassID: repo.class.ID,
		Entries: []dto.LogMoodBatchEntry{
			{StudentID: first, MoodType: constants.MoodSenang, ConfidenceScore: confidence(0.92), Notes: "  front row  "},
			{StudentID: second, MoodType: constants.MoodBingung, Source: constants.SourceManualInput, Notes: "   "},
		},
	})

	require.NoError(t, err)
	require.Equal(t, 1, repo.createCalls, "one call writes the whole batch")
	require.Len(t, repo.created, 2)
	require.Len(t, res.Logs, 2)

	want := now.UTC().Truncate(time.Microsecond)

	for i, moodLog := range repo.created {
		require.Equal(t, repo.guru.ID, moodLog.RecordedByGuruID)
		require.Equal(t, want, moodLog.RecordedAt, "every entry shares one recordedAt")
		require.Equal(t, moodLog.ID, res.Logs[i].LogID)
		require.Equal(t, moodLog.StudentID, res.Logs[i].StudentID)
		require.Equal(t, want, res.Logs[i].RecordedAt)
	}

	require.Equal(t, first, res.Logs[0].StudentID, "the response keeps the request order")
	require.Equal(t, second, res.Logs[1].StudentID)

	require.Equal(t, constants.SourceAICamera, repo.created[0].Source, "the source defaults to AI_CAMERA")
	require.InDelta(t, 0.92, repo.created[0].ConfidenceScore, 1e-6)
	require.Equal(t, "front row", *repo.created[0].Notes)

	require.Equal(t, constants.SourceManualInput, repo.created[1].Source)
	require.InDelta(t, 1, repo.created[1].ConfidenceScore, 1e-6, "a manual entry without a score counts as 1")
	require.Nil(t, repo.created[1].Notes, "whitespace only notes are dropped")
}

func TestLogMoodBatchKeepsAnExplicitScoreOnAManualEntry(t *testing.T) {
	repo := batching()
	student := repo.enroll()

	_, err := batchUseCase(repo, time.Now()).LogMoodBatch(context.Background(), uuid.New(), dto.LogMoodBatchRequest{
		ClassID: repo.class.ID,
		Entries: []dto.LogMoodBatchEntry{
			{StudentID: student, MoodType: constants.MoodSedih, Source: constants.SourceManualInput, ConfidenceScore: confidence(0.4)},
		},
	})

	require.NoError(t, err)
	require.InDelta(t, 0.4, repo.created[0].ConfidenceScore, 1e-6)
}

func TestLogMoodBatchHoldsBetweenOneAndTheMaximumEntries(t *testing.T) {
	repo := batching()
	student := repo.enroll()

	over := make([]dto.LogMoodBatchEntry, constants.MoodBatchMax+1)
	for i := range over {
		over[i] = aiEntry(student, constants.MoodSenang, 0.9)
	}

	for name, entries := range map[string][]dto.LogMoodBatchEntry{"none": nil, "one too many": over} {
		t.Run(name, func(t *testing.T) {
			_, err := batchUseCase(repo, time.Now()).LogMoodBatch(context.Background(), uuid.New(),
				dto.LogMoodBatchRequest{ClassID: repo.class.ID, Entries: entries})

			requireDetails(t, err, map[string]string{"entries": "must hold between 1 and 30 items"})
			require.Zero(t, repo.createCalls)
		})
	}
}

func TestLogMoodBatchAcceptsTheMaximumEntries(t *testing.T) {
	repo := batching()
	entries := make([]dto.LogMoodBatchEntry, constants.MoodBatchMax)

	for i := range entries {
		entries[i] = aiEntry(repo.enroll(), constants.MoodSenang, 0.9)
	}

	res, err := batchUseCase(repo, time.Now()).LogMoodBatch(context.Background(), uuid.New(),
		dto.LogMoodBatchRequest{ClassID: repo.class.ID, Entries: entries})

	require.NoError(t, err)
	require.Len(t, res.Logs, constants.MoodBatchMax)
}

func TestLogMoodBatchNeedsATeacherOfTheClass(t *testing.T) {
	entry := aiEntry(uuid.New(), constants.MoodSenang, 0.9)

	t.Run("a class the teacher does not teach", func(t *testing.T) {
		repo := batching()
		repo.class = nil

		_, err := batchUseCase(repo, time.Now()).LogMoodBatch(context.Background(), uuid.New(),
			dto.LogMoodBatchRequest{ClassID: uuid.New(), Entries: []dto.LogMoodBatchEntry{entry}})

		requireCode(t, err, apperror.ErrClassNotFound)
		require.Zero(t, repo.createCalls)
	})

	t.Run("no teacher profile", func(t *testing.T) {
		repo := batching()
		repo.guru = nil

		_, err := batchUseCase(repo, time.Now()).LogMoodBatch(context.Background(), uuid.New(),
			dto.LogMoodBatchRequest{ClassID: repo.class.ID, Entries: []dto.LogMoodBatchEntry{entry}})

		requireCode(t, err, apperror.ErrProfileNotFound)
		require.Zero(t, repo.createCalls)
	})

	t.Run("a failing lookup", func(t *testing.T) {
		repo := batching()
		repo.classErr = errors.New("db down")

		_, err := batchUseCase(repo, time.Now()).LogMoodBatch(context.Background(), uuid.New(),
			dto.LogMoodBatchRequest{ClassID: repo.class.ID, Entries: []dto.LogMoodBatchEntry{entry}})

		var got *apperror.Error

		require.ErrorAs(t, err, &got)
		require.Equal(t, "INTERNAL_ERROR", got.Code)
	})
}

func TestLogMoodBatchTreatsAnUnknownAndAForeignStudentAlike(t *testing.T) {
	repo := batching()
	inClass := repo.enroll()
	foreign := repo.enrollIn(uuid.New())
	unknown := uuid.New()

	_, err := batchUseCase(repo, time.Now()).LogMoodBatch(context.Background(), uuid.New(), dto.LogMoodBatchRequest{
		ClassID: repo.class.ID,
		Entries: []dto.LogMoodBatchEntry{
			aiEntry(inClass, constants.MoodSenang, 0.9),
			aiEntry(foreign, constants.MoodSenang, 0.9),
			aiEntry(unknown, constants.MoodSenang, 0.9),
		},
	})

	requireDetails(t, err, map[string]string{
		"entries[1].studentId": "is not in this class",
		"entries[2].studentId": "is not in this class",
	})
	require.Zero(t, repo.createCalls)
}

func TestLogMoodBatchRejectsARepeatedStudent(t *testing.T) {
	repo := batching()
	student, other := repo.enroll(), repo.enroll()

	_, err := batchUseCase(repo, time.Now()).LogMoodBatch(context.Background(), uuid.New(), dto.LogMoodBatchRequest{
		ClassID: repo.class.ID,
		Entries: []dto.LogMoodBatchEntry{
			aiEntry(student, constants.MoodSenang, 0.9),
			aiEntry(other, constants.MoodSedih, 0.8),
			aiEntry(student, constants.MoodMarah, 0.7),
		},
	})

	requireDetails(t, err, map[string]string{"entries[2].studentId": "appears more than once"})
	require.Zero(t, repo.createCalls)
}

func TestLogMoodBatchNeedsAScoreForTheCameraSource(t *testing.T) {
	repo := batching()
	first, second := repo.enroll(), repo.enroll()

	_, err := batchUseCase(repo, time.Now()).LogMoodBatch(context.Background(), uuid.New(), dto.LogMoodBatchRequest{
		ClassID: repo.class.ID,
		Entries: []dto.LogMoodBatchEntry{
			aiEntry(first, constants.MoodSenang, 0.9),
			{StudentID: second, MoodType: constants.MoodSedih},
		},
	})

	requireDetails(t, err, map[string]string{"entries[1].confidenceScore": "is required"})
	require.Zero(t, repo.createCalls)
}

func TestLogMoodBatchWritesNothingWhenOneEntryIsBad(t *testing.T) {
	repo := batching()
	good, alsoGood, repeated := repo.enroll(), repo.enroll(), repo.enroll()

	_, err := batchUseCase(repo, time.Now()).LogMoodBatch(context.Background(), uuid.New(), dto.LogMoodBatchRequest{
		ClassID: repo.class.ID,
		Entries: []dto.LogMoodBatchEntry{
			aiEntry(good, constants.MoodSenang, 0.9),
			aiEntry(uuid.New(), constants.MoodSedih, 0.8),
			aiEntry(alsoGood, constants.MoodMarah, 0.7),
			{StudentID: repeated, MoodType: constants.MoodBingung},
		},
	})

	requireDetails(t, err, map[string]string{
		"entries[1].studentId":       "is not in this class",
		"entries[3].confidenceScore": "is required",
	})
	require.Zero(t, repo.createCalls, "the valid entries are not stored either")
	require.Empty(t, repo.created)
}

func TestLogMoodBatchHidesAStorageFailure(t *testing.T) {
	repo := batching()
	repo.createErr = errors.New("insert failed")
	student := repo.enroll()

	res, err := batchUseCase(repo, time.Now()).LogMoodBatch(context.Background(), uuid.New(), dto.LogMoodBatchRequest{
		ClassID: repo.class.ID,
		Entries: []dto.LogMoodBatchEntry{aiEntry(student, constants.MoodSenang, 0.9)},
	})

	var got *apperror.Error

	require.ErrorAs(t, err, &got)
	require.Equal(t, "INTERNAL_ERROR", got.Code)
	require.NotContains(t, got.Message, "insert failed")
	require.Empty(t, res.Logs)
}

func TestRecordedAtOf(t *testing.T) {
	now := time.Date(2026, 3, 4, 8, 30, 15, 123456789, time.UTC)
	useCase := &GuruUseCase{now: func() time.Time { return now }}
	at := func(offset time.Duration) *time.Time {
		sent := now.Add(offset)

		return &sent
	}
	wall := now.Truncate(time.Microsecond)

	t.Run("no time means now", func(t *testing.T) {
		got, err := useCase.recordedAtOf(nil)

		require.NoError(t, err)
		require.Equal(t, wall, got)
	})

	t.Run("a time in the past is kept in UTC to the microsecond", func(t *testing.T) {
		sent := now.Add(-3 * time.Hour).In(time.FixedZone("WIB", 7*3600))

		got, err := useCase.recordedAtOf(&sent)

		require.NoError(t, err)
		require.Equal(t, now.Add(-3*time.Hour).Truncate(time.Microsecond), got)
		require.Equal(t, time.UTC, got.Location())
	})

	t.Run("the oldest accepted time is the retention of the idempotency keys", func(t *testing.T) {
		got, err := useCase.recordedAtOf(at(-constants.MoodRecordedAtMaxAge))
		require.NoError(t, err)
		require.Equal(t, now.Add(-constants.MoodRecordedAtMaxAge).Truncate(time.Microsecond), got)

		_, err = useCase.recordedAtOf(at(-constants.MoodRecordedAtMaxAge - time.Second))
		requireDetails(t, err, map[string]string{"recordedAt": "must not be older than 7 days"})
	})

	t.Run("a time ahead of the clock inside the skew counts as now", func(t *testing.T) {
		got, err := useCase.recordedAtOf(at(constants.MoodRecordedAtSkew))

		require.NoError(t, err)
		require.Equal(t, wall, got)
	})

	t.Run("a time beyond the skew is refused", func(t *testing.T) {
		_, err := useCase.recordedAtOf(at(constants.MoodRecordedAtSkew + time.Second))

		requireDetails(t, err, map[string]string{"recordedAt": "must not be in the future"})
	})
}

func TestLogMoodBatchUsesTheTimeTheClientSent(t *testing.T) {
	repo := batching()
	first, second := repo.enroll(), repo.enroll()
	now := time.Date(2026, 3, 4, 8, 30, 15, 0, time.UTC)
	seen := now.Add(-90 * time.Minute)

	res, err := batchUseCase(repo, now).LogMoodBatch(context.Background(), uuid.New(), dto.LogMoodBatchRequest{
		ClassID:    repo.class.ID,
		RecordedAt: &seen,
		Entries:    []dto.LogMoodBatchEntry{aiEntry(first, constants.MoodSenang, 0.9), aiEntry(second, constants.MoodSedih, 0.8)},
	})

	require.NoError(t, err)

	for i, moodLog := range repo.created {
		require.Equal(t, seen, moodLog.RecordedAt)
		require.Equal(t, seen, res.Logs[i].RecordedAt)
	}
}

func TestLogMoodBatchRefusesAnImpossibleTimeBeforeTouchingTheStore(t *testing.T) {
	repo := batching()
	student := repo.enroll()
	now := time.Date(2026, 3, 4, 8, 30, 15, 0, time.UTC)
	later := now.Add(time.Hour)

	_, err := batchUseCase(repo, now).LogMoodBatch(context.Background(), uuid.New(), dto.LogMoodBatchRequest{
		ClassID:    repo.class.ID,
		RecordedAt: &later,
		Entries:    []dto.LogMoodBatchEntry{aiEntry(student, constants.MoodSenang, 0.9)},
	})

	requireDetails(t, err, map[string]string{"recordedAt": "must not be in the future"})
	require.Zero(t, repo.createCalls)
}
