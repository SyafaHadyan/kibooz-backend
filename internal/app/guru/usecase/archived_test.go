package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/guru/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
)

// An archived class is read only, so a teacher cannot change its code, its joining, its children, its teachers or its owner.
func TestAnArchivedClassRefusesEveryChangeOfItsSettingsAndPeople(t *testing.T) {
	ctx := context.Background()
	repo := newMoveRepo()
	archivedAt := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	repo.class.ArchivedAt = &archivedAt
	useCase := NewGuruUseCase(repo, &deletedKeys{}, nil)
	open := true

	_, err := useCase.RotateJoinCode(ctx, uuid.New(), repo.class.ID)
	require.ErrorIs(t, err, apperror.ErrClassArchived)

	_, err = useCase.SetJoining(ctx, uuid.New(), repo.class.ID, dto.SetJoiningRequest{JoinOpen: &open})
	require.ErrorIs(t, err, apperror.ErrClassArchived)

	require.ErrorIs(t, useCase.RemoveStudent(ctx, uuid.New(), repo.class.ID, uuid.New()), apperror.ErrClassArchived)

	_, err = useCase.AddTeacher(ctx, uuid.New(), repo.class.ID, dto.AddTeacherRequest{Email: "other@example.com"})
	require.ErrorIs(t, err, apperror.ErrClassArchived)

	require.ErrorIs(t, useCase.RemoveTeacher(ctx, uuid.New(), repo.class.ID, uuid.New()), apperror.ErrClassArchived)

	_, err = useCase.TransferOwnership(ctx, uuid.New(), repo.class.ID, dto.TransferOwnershipRequest{TeacherID: uuid.New()})
	require.ErrorIs(t, err, apperror.ErrClassArchived)

	require.Empty(t, repo.calls, "nothing was asked of the store")
}

func TestAClassSummaryShowsWhenTheClassWasArchived(t *testing.T) {
	archivedAt := time.Date(2026, 10, 22, 9, 0, 0, 0, time.FixedZone("WIB", 7*3600))

	live := classSummary(&repository.ClassRow{})
	require.Nil(t, live.ArchivedAt)

	row := repository.ClassRow{}
	row.ArchivedAt = &archivedAt

	got := classSummary(&row)
	require.NotNil(t, got.ArchivedAt)
	require.Equal(t, archivedAt.UTC(), *got.ArchivedAt)
}
