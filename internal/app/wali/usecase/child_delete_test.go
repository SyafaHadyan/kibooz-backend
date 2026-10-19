package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
)

func TestDeleteChildHidesTheChildAndClearsTheLeaderboard(t *testing.T) {
	classID := uuid.New()
	childID := uuid.New()
	repo := parentRepo()
	repo.removeFound, repo.removeClass = true, classID
	cache := &fakeCache{}

	err := newUseCase(repo, cache).DeleteChild(context.Background(), uuid.New(), childID)

	require.NoError(t, err)
	require.Equal(t, childID, repo.removedChild)
	require.Equal(t, repo.wali.ID, repo.removedParent, "the parent who asked is the scope of the removal")
	require.Equal(t, []string{constants.LeaderboardKeyPrefix + classID.String()}, cache.deleted)
}

func TestDeleteChildRefusesWhatItCannotFind(t *testing.T) {
	ctx := context.Background()

	t.Run("an account without a parent profile", func(t *testing.T) {
		err := newUseCase(&fakeRepo{}, &fakeCache{}).DeleteChild(ctx, uuid.New(), uuid.New())

		requireAppError(t, err, apperror.ErrProfileNotFound)
	})

	t.Run("a child that is not theirs", func(t *testing.T) {
		cache := &fakeCache{}

		err := newUseCase(parentRepo(), cache).DeleteChild(ctx, uuid.New(), uuid.New())

		requireAppError(t, err, apperror.ErrStudentNotFound)
		require.Empty(t, cache.deleted, "nothing changed, so the cache stays")
	})

	t.Run("a failure of the database", func(t *testing.T) {
		repo := parentRepo()
		repo.removeErr = errors.New("connection lost")

		err := newUseCase(repo, &fakeCache{}).DeleteChild(ctx, uuid.New(), uuid.New())

		requireAppError(t, err, apperror.Internal(nil))
	})
}

func TestAParentWithoutChildrenGetsAClearAnswerFromTheDashboard(t *testing.T) {
	ctx := context.Background()

	_, err := newUseCase(parentRepo(), &fakeCache{}).Dashboard(ctx, uuid.New(), nil)

	requireAppError(t, err, apperror.ErrNoChild)

	missing := uuid.New()

	_, err = newUseCase(parentRepo(), &fakeCache{}).Dashboard(ctx, uuid.New(), &missing)

	requireAppError(t, err, apperror.ErrStudentNotFound)
}
