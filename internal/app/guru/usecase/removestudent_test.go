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
)

// removeRepo answers only what RemoveStudent asks for, any other call panics on the nil interface
type removeRepo struct {
	repository.GuruDBItf

	class     *entity.Class
	removed   bool
	removeErr error
	asked     uuid.UUID
}

func (r *removeRepo) FindGuruByUserID(context.Context, uuid.UUID) (*repository.GuruRow, error) {
	return &repository.GuruRow{}, nil
}

func (r *removeRepo) FindTaughtClass(context.Context, uuid.UUID, *uuid.UUID) (*entity.Class, error) {
	return r.class, nil
}

func (r *removeRepo) RemoveStudent(_ context.Context, _ uuid.UUID, studentID uuid.UUID) (bool, error) {
	r.asked = studentID

	return r.removed, r.removeErr
}

type deletedKeys struct{ keys []string }

func (d *deletedKeys) Set(context.Context, string, string, time.Duration) error { return nil }

func (d *deletedKeys) Get(context.Context, string) (string, bool, error) { return "", false, nil }

func (d *deletedKeys) Del(_ context.Context, keys ...string) error {
	d.keys = append(d.keys, keys...)

	return nil
}

func (d *deletedKeys) Available() bool { return true }

func (d *deletedKeys) Ping(context.Context) error { return nil }

func TestRemoveStudentClearsTheLeaderboardOfTheClass(t *testing.T) {
	class := &entity.Class{ID: uuid.New()}
	repo := &removeRepo{class: class, removed: true}
	cache := &deletedKeys{}
	child := uuid.New()

	err := NewGuruUseCase(repo, cache, nil).RemoveStudent(context.Background(), uuid.New(), class.ID, child)

	require.NoError(t, err)
	require.Equal(t, child, repo.asked)
	require.Equal(t, []string{constants.LeaderboardKeyPrefix + class.ID.String()}, cache.keys)
}

func TestRemoveStudentRefusesWhatItCannotFind(t *testing.T) {
	class := &entity.Class{ID: uuid.New()}

	t.Run("a class the teacher does not teach", func(t *testing.T) {
		repo := &removeRepo{}
		cache := &deletedKeys{}

		err := NewGuruUseCase(repo, cache, nil).RemoveStudent(context.Background(), uuid.New(), uuid.New(), uuid.New())

		require.ErrorIs(t, err, apperror.ErrClassNotFound)
		require.Empty(t, cache.keys)
	})

	t.Run("a child that is not in the class", func(t *testing.T) {
		cache := &deletedKeys{}

		err := NewGuruUseCase(&removeRepo{class: class}, cache, nil).RemoveStudent(context.Background(), uuid.New(), class.ID, uuid.New())

		require.ErrorIs(t, err, apperror.ErrStudentNotFound)
		require.Empty(t, cache.keys, "nothing changed, so the cache stays")
	})

	t.Run("a failure of the database", func(t *testing.T) {
		cache := &deletedKeys{}
		repo := &removeRepo{class: class, removeErr: errors.New("connection lost")}

		err := NewGuruUseCase(repo, cache, nil).RemoveStudent(context.Background(), uuid.New(), class.ID, uuid.New())

		var appErr *apperror.Error

		require.ErrorAs(t, err, &appErr)
		require.Equal(t, 500, appErr.Status)
		require.Empty(t, cache.keys)
	})
}
