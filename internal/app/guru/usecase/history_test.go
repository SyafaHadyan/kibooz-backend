package usecase_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/guru/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/app/guru/usecase"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/pagination"
)

type classHistoryRepo struct {
	repository.GuruDBItf

	guru      *repository.GuruRow
	guruErr   error
	class     *entity.Class
	classErr  error
	rows      []repository.GuruHistoryRow
	total     int
	listErr   error
	askedType string
	listCalls int
}

func (r *classHistoryRepo) FindGuruByUserID(context.Context, uuid.UUID) (*repository.GuruRow, error) {
	return r.guru, r.guruErr
}

func (r *classHistoryRepo) FindTaughtClass(context.Context, uuid.UUID, *uuid.UUID) (*entity.Class, error) {
	return r.class, r.classErr
}

func (r *classHistoryRepo) ListClassHistory(
	_ context.Context, _ uuid.UUID, eventType string, _ int, _ int,
) ([]repository.GuruHistoryRow, int, error) {
	r.askedType = eventType
	r.listCalls++

	return r.rows, r.total, r.listErr
}

func TestClassHistory(t *testing.T) {
	now := time.Date(2026, 11, 20, 8, 30, 0, 0, time.UTC)
	classID := uuid.New()
	studentID := uuid.New()
	studentName := "Amelia"
	actorName := "Ibu Sari"
	mood := constants.MoodSenang
	conf := float32(0.95)
	guideID := "guidance-senang-4step"
	videoTitle := "Video Pagi"

	guru := &repository.GuruRow{Guru: entity.Guru{ID: uuid.New()}}
	class := &entity.Class{ID: classID, Name: "Kelompok B"}

	repo := &classHistoryRepo{
		guru:  guru,
		class: class,
		total: 3,
		rows: []repository.GuruHistoryRow{
			{ID: uuid.New(), EventType: "video", Title: &videoTitle, ActorName: &actorName, OccurredAt: now},
			{ID: uuid.New(), EventType: "guidance", StudentID: &studentID, StudentName: &studentName, GuidanceID: &guideID, OccurredAt: now.Add(-time.Hour)},
			{ID: uuid.New(), EventType: "mood", StudentID: &studentID, StudentName: &studentName, MoodType: &mood, ConfidenceScore: &conf, OccurredAt: now.Add(-2 * time.Hour)},
		},
	}

	uc := usecase.NewGuruUseCase(repo, nil, &env.Env{})

	res, err := uc.ClassHistory(context.Background(), uuid.New(), &classID, "all", pagination.Params{Page: 1, Limit: 20})
	require.NoError(t, err)
	require.Equal(t, "all", repo.askedType)
	require.Equal(t, 3, res.Total)
	require.Len(t, res.Events, 3)
	require.Equal(t, videoTitle, res.Events[0].Title)
	require.Contains(t, res.Events[1].Title, "Celebrate a Cheerful Day")
	require.Contains(t, res.Events[2].Title, "Amelia: Happy / Cheerful")

	_, err = uc.ClassHistory(context.Background(), uuid.New(), &classID, "invalid", pagination.Params{Page: 1, Limit: 20})
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "VALIDATION_ERROR", appErr.Code)

	repo.listErr = errors.New("boom")
	_, err = uc.ClassHistory(context.Background(), uuid.New(), &classID, "", pagination.Params{Page: 1, Limit: 20})
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "INTERNAL_ERROR", appErr.Code)
}

func historyCode(t *testing.T, err error) string {
	t.Helper()

	var appErr *apperror.Error

	require.ErrorAs(t, err, &appErr)

	return appErr.Code
}

func TestClassHistoryChecksTheTypeBeforeTouchingTheDatabase(t *testing.T) {
	classID := uuid.New()
	repo := &classHistoryRepo{guru: &repository.GuruRow{Guru: entity.Guru{ID: uuid.New()}}, class: &entity.Class{ID: classID, Name: "Kelompok B"}}
	uc := usecase.NewGuruUseCase(repo, nil, &env.Env{})
	page := pagination.Params{Page: 1, Limit: 20}

	for _, bad := range []string{"trash", "MOOD", " mood", "all,mood", "mood;"} {
		_, err := uc.ClassHistory(context.Background(), uuid.New(), &classID, bad, page)
		require.Equal(t, "VALIDATION_ERROR", historyCode(t, err), bad)
	}

	require.Zero(t, repo.listCalls, "a bad type must not reach the repository")

	for _, good := range []string{"", "all", "mood", "guidance", "video"} {
		_, err := uc.ClassHistory(context.Background(), uuid.New(), &classID, good, page)
		require.NoError(t, err, good)
		require.Equal(t, good, repo.askedType)
	}
}

func TestClassHistoryMapsTheErrorsOfEachStep(t *testing.T) {
	classID := uuid.New()
	guru := &repository.GuruRow{Guru: entity.Guru{ID: uuid.New()}}
	class := &entity.Class{ID: classID, Name: "Kelompok B"}
	page := pagination.Params{Page: 1, Limit: 20}

	cases := []struct {
		name string
		repo *classHistoryRepo
		code string
	}{
		{"the profile lookup fails", &classHistoryRepo{guruErr: errors.New("boom")}, "INTERNAL_ERROR"},
		{"the account has no teacher profile", &classHistoryRepo{}, "PROFILE_NOT_FOUND"},
		{"the class lookup fails", &classHistoryRepo{guru: guru, classErr: errors.New("boom")}, "INTERNAL_ERROR"},
		{"the class is not taught by the teacher", &classHistoryRepo{guru: guru}, "CLASS_NOT_FOUND"},
		{"the list fails", &classHistoryRepo{guru: guru, class: class, listErr: errors.New("boom")}, "INTERNAL_ERROR"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uc := usecase.NewGuruUseCase(tc.repo, nil, &env.Env{})

			_, err := uc.ClassHistory(context.Background(), uuid.New(), &classID, "all", page)
			require.Equal(t, tc.code, historyCode(t, err))
		})
	}
}

func TestClassHistoryHidesTheNameOfADeletedAccount(t *testing.T) {
	now := time.Date(2026, 11, 20, 8, 30, 0, 0, time.UTC)
	classID := uuid.New()
	studentID := uuid.New()
	studentName := "Amelia"
	teacher := "Ibu Sari"
	parent := "Bu Wati"
	mood := constants.MoodSenang
	guideID := "guidance-senang-4step"
	title := "Video Pagi"

	repo := &classHistoryRepo{
		guru:  &repository.GuruRow{Guru: entity.Guru{ID: uuid.New()}},
		class: &entity.Class{ID: classID, Name: "Kelompok B"},
		total: 4,
		rows: []repository.GuruHistoryRow{
			{ID: uuid.New(), EventType: "video", Title: &title, ActorName: &teacher, ActorDeleted: true, OccurredAt: now},
			{ID: uuid.New(), EventType: "mood", StudentID: &studentID, StudentName: &studentName, MoodType: &mood, ActorName: &teacher, ActorDeleted: true, OccurredAt: now},
			{ID: uuid.New(), EventType: "guidance", StudentID: &studentID, StudentName: &studentName, GuidanceID: &guideID, ActorName: &parent, ActorDeleted: true, OccurredAt: now},
			{ID: uuid.New(), EventType: "mood", StudentID: &studentID, StudentName: &studentName, MoodType: &mood, ActorName: &teacher, OccurredAt: now},
		},
	}

	uc := usecase.NewGuruUseCase(repo, nil, &env.Env{})

	res, err := uc.ClassHistory(context.Background(), uuid.New(), &classID, "all", pagination.Params{Page: 1, Limit: 20})
	require.NoError(t, err)
	require.Len(t, res.Events, 4)

	for _, event := range res.Events[:3] {
		require.NotNil(t, event.ActorName)
		require.Equal(t, "Deleted account", *event.ActorName)
	}

	require.Equal(t, teacher, *res.Events[3].ActorName, "a live account keeps its name")

	// the names of the two deleted accounts must not leave the use case in any field
	encoded, err := json.Marshal(res.Events[:3])
	require.NoError(t, err)
	require.NotContains(t, string(encoded), teacher)
	require.NotContains(t, string(encoded), parent)

	// an event with no actor at all stays without one
	repo.rows = []repository.GuruHistoryRow{{ID: uuid.New(), EventType: "video", Title: &title, OccurredAt: now}}
	repo.total = 1

	res, err = uc.ClassHistory(context.Background(), uuid.New(), &classID, "all", pagination.Params{Page: 1, Limit: 20})
	require.NoError(t, err)
	require.Nil(t, res.Events[0].ActorName)
}
