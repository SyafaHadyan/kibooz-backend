package usecase_test

import (
	"context"
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
