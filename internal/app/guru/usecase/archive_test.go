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
)

// archiveRepo answers what ArchiveClass and RestoreClass ask for, any other call panics on the nil interface
type archiveRepo struct {
	*moveRepo

	row     *repository.ClassRow
	calls   []string
	actor   uuid.UUID
	findErr error
}

func (r *archiveRepo) ArchiveClass(_ context.Context, _ uuid.UUID, actor uuid.UUID) error {
	r.calls, r.actor = append(r.calls, "archive"), actor

	return r.err
}

func (r *archiveRepo) RestoreClass(_ context.Context, _ uuid.UUID, actor uuid.UUID) (string, error) {
	r.calls, r.actor = append(r.calls, "restore"), actor

	return "NEWCODE1", r.err
}

func (r *archiveRepo) FindClassRow(context.Context, uuid.UUID, uuid.UUID) (*repository.ClassRow, error) {
	return r.row, r.findErr
}

func newArchiveRepo() *archiveRepo {
	base := newMoveRepo()
	row := &repository.ClassRow{}
	row.ID = base.class.ID
	row.JoinCode = "NEWCODE1"

	return &archiveRepo{moveRepo: base, row: row}
}

func TestArchiveClassActsForTheTeacherAndClearsTheLeaderboard(t *testing.T) {
	repo := newArchiveRepo()
	cache := &deletedKeys{}
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	repo.row.ArchivedAt = &at

	res, err := NewGuruUseCase(repo, cache, nil).ArchiveClass(context.Background(), uuid.New(), repo.class.ID)
	require.NoError(t, err)

	require.Equal(t, []string{"archive"}, repo.calls)
	require.Equal(t, repo.guru.ID, repo.actor)
	require.Equal(t, []string{constants.LeaderboardKeyPrefix + repo.class.ID.String()}, cache.keys)
	require.NotNil(t, res.ArchivedAt)
	require.Equal(t, at, *res.ArchivedAt)
}

func TestRestoreClassShowsTheNewJoinCodeAndNoArchiveTime(t *testing.T) {
	repo := newArchiveRepo()
	cache := &deletedKeys{}

	res, err := NewGuruUseCase(repo, cache, nil).RestoreClass(context.Background(), uuid.New(), repo.class.ID)
	require.NoError(t, err)

	require.Equal(t, []string{"restore"}, repo.calls)
	require.Equal(t, "NEWCODE1", res.JoinCode)
	require.Nil(t, res.ArchivedAt)
	require.Len(t, cache.keys, 1)
}

func TestArchiveAndRestoreKeepTheAnswersOfTheRulesAndHideOtherErrors(t *testing.T) {
	ctx := context.Background()

	for _, rule := range []*apperror.Error{
		apperror.ErrNotClassOwner, apperror.ErrClassArchived, apperror.ErrClassNotArchived, apperror.ErrClassLimitReached, apperror.ErrClassNotFound,
	} {
		repo := newArchiveRepo()
		repo.err = rule
		useCase := NewGuruUseCase(repo, &deletedKeys{}, nil)

		_, err := useCase.ArchiveClass(ctx, uuid.New(), repo.class.ID)
		require.ErrorIs(t, err, rule)

		_, err = useCase.RestoreClass(ctx, uuid.New(), repo.class.ID)
		require.ErrorIs(t, err, rule)
	}

	repo := newArchiveRepo()
	repo.err = errors.New("connection reset")
	cache := &deletedKeys{}

	_, err := NewGuruUseCase(repo, cache, nil).ArchiveClass(ctx, uuid.New(), repo.class.ID)

	var appErr *apperror.Error

	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "INTERNAL_ERROR", appErr.Code)
	require.Empty(t, cache.keys, "a class that was not archived keeps its cached leaderboard")
}

func TestArchiveAndRestoreOnlyWorkForATeacherOfTheClass(t *testing.T) {
	ctx := context.Background()
	repo := newArchiveRepo()
	repo.class = nil
	useCase := NewGuruUseCase(repo, &deletedKeys{}, nil)

	_, err := useCase.ArchiveClass(ctx, uuid.New(), uuid.New())
	require.ErrorIs(t, err, apperror.ErrClassNotFound)

	_, err = useCase.RestoreClass(ctx, uuid.New(), uuid.New())
	require.ErrorIs(t, err, apperror.ErrClassNotFound)
	require.Empty(t, repo.calls)
}

func TestArchiveAnswersWithoutTheClassWhenItVanishes(t *testing.T) {
	repo := newArchiveRepo()
	repo.row = nil

	_, err := NewGuruUseCase(repo, &deletedKeys{}, nil).ArchiveClass(context.Background(), uuid.New(), repo.class.ID)
	require.ErrorIs(t, err, apperror.ErrClassNotFound)

	repo.row, repo.findErr = &repository.ClassRow{}, errors.New("connection reset")

	_, err = NewGuruUseCase(repo, &deletedKeys{}, nil).RestoreClass(context.Background(), uuid.New(), repo.class.ID)

	var appErr *apperror.Error

	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "INTERNAL_ERROR", appErr.Code)
}
