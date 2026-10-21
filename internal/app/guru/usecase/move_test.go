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
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/enrollment"
)

// moveRepo answers only what the move routes ask for, any other call panics on the nil interface
type moveRepo struct {
	repository.GuruDBItf

	guru  *repository.GuruRow
	class *entity.Class
	rows  []enrollment.ClassRow
	left  uuid.UUID
	err   error

	asked   uuid.UUID
	request uuid.UUID
	actor   uuid.UUID
	calls   []string
}

func (r *moveRepo) FindGuruByUserID(context.Context, uuid.UUID) (*repository.GuruRow, error) {
	return r.guru, nil
}

func (r *moveRepo) FindTaughtClass(context.Context, uuid.UUID, *uuid.UUID) (*entity.Class, error) {
	return r.class, nil
}

func (r *moveRepo) ListMoveRequests(_ context.Context, classID uuid.UUID) ([]enrollment.ClassRow, error) {
	r.asked = classID

	return r.rows, r.err
}

func (r *moveRepo) AcceptMove(_ context.Context, classID uuid.UUID, requestID uuid.UUID, guruID uuid.UUID) (uuid.UUID, error) {
	r.asked, r.request, r.actor = classID, requestID, guruID
	r.calls = append(r.calls, "accept")

	return r.left, r.err
}

func (r *moveRepo) RejectMove(_ context.Context, classID uuid.UUID, requestID uuid.UUID, guruID uuid.UUID) error {
	r.asked, r.request, r.actor = classID, requestID, guruID
	r.calls = append(r.calls, "reject")

	return r.err
}

func newMoveRepo() *moveRepo {
	return &moveRepo{guru: &repository.GuruRow{ID: uuid.New()}, class: &entity.Class{ID: uuid.New()}, left: uuid.New()}
}

func TestAcceptMoveClearsTheLeaderboardOfBothClasses(t *testing.T) {
	repo := newMoveRepo()
	cache := &deletedKeys{}
	requestID := uuid.New()

	require.NoError(t, NewGuruUseCase(repo, cache, nil).AcceptMove(context.Background(), uuid.New(), repo.class.ID, requestID))

	require.Equal(t, repo.class.ID, repo.asked)
	require.Equal(t, requestID, repo.request)
	require.Equal(t, repo.guru.ID, repo.actor)
	require.ElementsMatch(t, []string{
		constants.LeaderboardKeyPrefix + repo.class.ID.String(), constants.LeaderboardKeyPrefix + repo.left.String(),
	}, cache.keys)
}

func TestRejectMoveLeavesTheLeaderboardAlone(t *testing.T) {
	repo := newMoveRepo()
	cache := &deletedKeys{}

	require.NoError(t, NewGuruUseCase(repo, cache, nil).RejectMove(context.Background(), uuid.New(), repo.class.ID, uuid.New()))
	require.Equal(t, []string{"reject"}, repo.calls)
	require.Empty(t, cache.keys)
}

func TestMoveRoutesOnlyWorkForATeacherOfTheClass(t *testing.T) {
	ctx := context.Background()
	repo := newMoveRepo()
	repo.class = nil
	useCase := NewGuruUseCase(repo, &deletedKeys{}, nil)

	_, err := useCase.MoveRequests(ctx, uuid.New(), uuid.New())
	require.ErrorIs(t, err, apperror.ErrClassNotFound)
	require.ErrorIs(t, useCase.AcceptMove(ctx, uuid.New(), uuid.New(), uuid.New()), apperror.ErrClassNotFound)
	require.ErrorIs(t, useCase.RejectMove(ctx, uuid.New(), uuid.New(), uuid.New()), apperror.ErrClassNotFound)
	require.Empty(t, repo.calls, "nothing is decided for a class the teacher does not teach")
}

func TestMoveRoutesKeepTheAnswersOfTheRulesAndHideOtherErrors(t *testing.T) {
	ctx := context.Background()

	for _, rule := range []*apperror.Error{apperror.ErrMoveNotFound, apperror.ErrClassFull, apperror.ErrAlreadyInClass} {
		repo := newMoveRepo()
		repo.err = rule
		useCase := NewGuruUseCase(repo, &deletedKeys{}, nil)

		require.ErrorIs(t, useCase.AcceptMove(ctx, uuid.New(), repo.class.ID, uuid.New()), rule)
		require.ErrorIs(t, useCase.RejectMove(ctx, uuid.New(), repo.class.ID, uuid.New()), rule)
	}

	repo := newMoveRepo()
	repo.err = errors.New("connection reset")
	cache := &deletedKeys{}

	err := NewGuruUseCase(repo, cache, nil).AcceptMove(ctx, uuid.New(), repo.class.ID, uuid.New())

	var appErr *apperror.Error

	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "INTERNAL_ERROR", appErr.Code)
	require.Empty(t, cache.keys, "a move that failed leaves the leaderboards as they are")
}

func TestMoveRequestsListsTheRequestsThatWait(t *testing.T) {
	repo := newMoveRepo()
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	repo.rows = []enrollment.ClassRow{{ID: uuid.New(), StudentID: uuid.New(), StudentName: "Amelia", RequestedAt: at}}

	res, err := NewGuruUseCase(repo, &deletedKeys{}, nil).MoveRequests(context.Background(), uuid.New(), repo.class.ID)
	require.NoError(t, err)
	require.Len(t, res.Requests, 1)
	require.Equal(t, "Amelia", res.Requests[0].StudentName)
	require.Equal(t, at, res.Requests[0].RequestedAt)

	repo.rows = nil

	empty, err := NewGuruUseCase(repo, &deletedKeys{}, nil).MoveRequests(context.Background(), uuid.New(), repo.class.ID)
	require.NoError(t, err)
	require.NotNil(t, empty.Requests)
}
