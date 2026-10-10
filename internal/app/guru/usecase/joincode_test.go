package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/guru/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
)

// codeRepo answers only what RotateJoinCode asks for, any other call panics on the nil interface
type codeRepo struct {
	repository.GuruDBItf

	guru      *repository.GuruRow
	guruErr   error
	class     *entity.Class
	classErr  error
	rotateErr error
	rotated   uuid.UUID
}

func (r *codeRepo) FindGuruByUserID(context.Context, uuid.UUID) (*repository.GuruRow, error) {
	return r.guru, r.guruErr
}

func (r *codeRepo) FindTaughtClass(context.Context, uuid.UUID, *uuid.UUID) (*entity.Class, error) {
	return r.class, r.classErr
}

func (r *codeRepo) RotateJoinCode(_ context.Context, classID uuid.UUID) (string, error) {
	r.rotated = classID

	return "NEWCODE", r.rotateErr
}

func rotate(repo *codeRepo) error {
	_, err := NewGuruUseCase(repo, nil).RotateJoinCode(context.Background(), uuid.New(), uuid.New())

	return err
}

func TestRotateJoinCodeReplacesTheCodeOfTheResolvedClass(t *testing.T) {
	class := &entity.Class{ID: uuid.New()}
	repo := &codeRepo{guru: &repository.GuruRow{}, class: class}

	got, err := NewGuruUseCase(repo, nil).RotateJoinCode(context.Background(), uuid.New(), class.ID)

	require.NoError(t, err)
	require.Equal(t, "NEWCODE", got.JoinCode)
	require.Equal(t, class.ID, repo.rotated)
}

func TestRotateJoinCodeRefusesWhatItCannotResolve(t *testing.T) {
	found := &repository.GuruRow{}

	require.ErrorIs(t, rotate(&codeRepo{}), apperror.ErrProfileNotFound)
	require.ErrorIs(t, rotate(&codeRepo{guru: found}), apperror.ErrClassNotFound)

	for name, repo := range map[string]*codeRepo{
		"the teacher lookup fails": {guruErr: errors.New("db down")},
		"the class lookup fails":   {guru: found, classErr: errors.New("db down")},
		"the rotation fails":       {guru: found, class: &entity.Class{}, rotateErr: errors.New("db down")},
	} {
		var appErr *apperror.Error

		require.ErrorAs(t, rotate(repo), &appErr, name)
		require.Equal(t, "INTERNAL_ERROR", appErr.Code, name)
	}
}
