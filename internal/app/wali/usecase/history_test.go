package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/wali/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/app/wali/usecase"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/pagination"
)

type childHistoryRepo struct {
	fakeRepo

	rows      []repository.WaliHistoryRow
	total     int
	listErr   error
	askedType string
}

func (r *childHistoryRepo) ListChildHistory(
	_ context.Context, _ uuid.UUID, eventType string, _ int, _ int,
) ([]repository.WaliHistoryRow, int, error) {
	r.askedType = eventType

	return r.rows, r.total, r.listErr
}

func TestChildHistory(t *testing.T) {
	now := time.Date(2026, 11, 20, 8, 30, 0, 0, time.UTC)
	studentID := uuid.New()
	mood := constants.MoodSenang
	trash := constants.TrashOrganik
	points := 10
	guideID := "guidance-senang-4step"

	repo := &childHistoryRepo{
		fakeRepo: fakeRepo{
			wali: &entity.Wali{ID: uuid.New()},
			findResult: &repository.StudentRow{
				Student:   entity.Student{ID: studentID, FullName: "Amelia"},
				ClassName: "Kelompok B",
			},
		},
		total: 3,
		rows: []repository.WaliHistoryRow{
			{ID: uuid.New(), EventType: "guidance", GuidanceID: &guideID, OccurredAt: now},
			{ID: uuid.New(), EventType: "trash", TrashType: &trash, PointsAwarded: &points, OccurredAt: now.Add(-time.Hour)},
			{ID: uuid.New(), EventType: "mood", MoodType: &mood, OccurredAt: now.Add(-2 * time.Hour)},
		},
	}

	uc := usecase.NewWaliUseCase(repo, nil, &env.Env{})

	res, err := uc.ChildHistory(context.Background(), uuid.New(), studentID, "all", pagination.Params{Page: 1, Limit: 20})
	require.NoError(t, err)
	require.Equal(t, "all", repo.askedType)
	require.Equal(t, studentID, res.Student.ID)
	require.Equal(t, "Kelompok B", res.Student.ClassName)
	require.Len(t, res.Events, 3)
	require.Contains(t, res.Events[0].Title, "Celebrate a Cheerful Day")
	require.NotNil(t, res.Events[0].GuidanceCategory)
	require.Equal(t, "ORGANIK (+10 points)", res.Events[1].Title)
	require.Equal(t, "Mood: Happy / Cheerful", res.Events[2].Title)

	_, err = uc.ChildHistory(context.Background(), uuid.New(), studentID, "bad", pagination.Params{Page: 1, Limit: 20})
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "VALIDATION_ERROR", appErr.Code)

	repo.listErr = errors.New("db failure")
	_, err = uc.ChildHistory(context.Background(), uuid.New(), studentID, "", pagination.Params{Page: 1, Limit: 20})
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "INTERNAL_ERROR", appErr.Code)
}
