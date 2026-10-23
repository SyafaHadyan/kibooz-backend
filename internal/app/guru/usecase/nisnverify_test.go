package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/guru/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
)

// nisnRepo answers only what the verification of a NISN asks for, any other call panics on the nil interface
type nisnRepo struct {
	removeRepo

	found     bool
	displaced int64
	err       error
	asked     uuid.UUID
	askedBy   uuid.UUID
	unasked   uuid.UUID
	// guru is the teacher that the repository finds for the account of the caller
	guru repository.GuruRow
}

func (r *nisnRepo) FindGuruByUserID(context.Context, uuid.UUID) (*repository.GuruRow, error) {
	return &r.guru, nil
}

func (r *nisnRepo) VerifyNISN(_ context.Context, _ uuid.UUID, studentID uuid.UUID, guruID uuid.UUID) (bool, int64, error) {
	r.asked = studentID
	r.askedBy = guruID

	return r.found, r.displaced, r.err
}

func (r *nisnRepo) UnverifyNISN(_ context.Context, _ uuid.UUID, studentID uuid.UUID) (bool, error) {
	r.unasked = studentID

	return r.found, r.err
}

func TestVerifyNISNAnswersWhoLostTheNumber(t *testing.T) {
	class := &entity.Class{ID: uuid.New()}
	repo := &nisnRepo{
		removeRepo: removeRepo{class: class}, found: true, displaced: 3, guru: repository.GuruRow{Guru: entity.Guru{ID: uuid.New()}},
	}
	child := uuid.New()

	res, err := NewGuruUseCase(repo, &deletedKeys{}, nil).VerifyNISN(context.Background(), uuid.New(), class.ID, child)

	require.NoError(t, err)
	require.True(t, res.NISNVerified)
	require.EqualValues(t, 3, res.Displaced)
	require.Equal(t, child, repo.asked)
	require.Equal(t, repo.guru.ID, repo.askedBy, "the teacher who asked is the one that is recorded")
}

func TestUnverifyNISNAnswersUnverified(t *testing.T) {
	class := &entity.Class{ID: uuid.New()}
	repo := &nisnRepo{removeRepo: removeRepo{class: class}, found: true}
	child := uuid.New()

	res, err := NewGuruUseCase(repo, &deletedKeys{}, nil).UnverifyNISN(context.Background(), uuid.New(), class.ID, child)

	require.NoError(t, err)
	require.False(t, res.NISNVerified)
	require.Equal(t, child, repo.unasked)
}

func TestTheNISNRoutesRefuseWhatTheyCannotUse(t *testing.T) {
	archivedAt := time.Now()

	t.Run("a class the teacher does not teach", func(t *testing.T) {
		useCase := NewGuruUseCase(&nisnRepo{}, &deletedKeys{}, nil)

		_, err := useCase.VerifyNISN(context.Background(), uuid.New(), uuid.New(), uuid.New())
		require.ErrorIs(t, err, apperror.ErrClassNotFound)

		_, err = useCase.UnverifyNISN(context.Background(), uuid.New(), uuid.New(), uuid.New())
		require.ErrorIs(t, err, apperror.ErrClassNotFound)
	})

	t.Run("an archived class is read only", func(t *testing.T) {
		class := &entity.Class{ID: uuid.New(), ArchivedAt: &archivedAt}
		useCase := NewGuruUseCase(&nisnRepo{removeRepo: removeRepo{class: class}, found: true}, &deletedKeys{}, nil)

		_, err := useCase.VerifyNISN(context.Background(), uuid.New(), class.ID, uuid.New())
		require.ErrorIs(t, err, apperror.ErrClassArchived)

		_, err = useCase.UnverifyNISN(context.Background(), uuid.New(), class.ID, uuid.New())
		require.ErrorIs(t, err, apperror.ErrClassArchived)
	})

	t.Run("a child that is not in the class", func(t *testing.T) {
		class := &entity.Class{ID: uuid.New()}
		useCase := NewGuruUseCase(&nisnRepo{removeRepo: removeRepo{class: class}}, &deletedKeys{}, nil)

		_, err := useCase.VerifyNISN(context.Background(), uuid.New(), class.ID, uuid.New())
		require.ErrorIs(t, err, apperror.ErrStudentNotFound)

		_, err = useCase.UnverifyNISN(context.Background(), uuid.New(), class.ID, uuid.New())
		require.ErrorIs(t, err, apperror.ErrStudentNotFound)
	})

	t.Run("an application error of the repository reaches the caller as it is", func(t *testing.T) {
		class := &entity.Class{ID: uuid.New()}

		useCase := NewGuruUseCase(&nisnRepo{removeRepo: removeRepo{class: class}, err: apperror.ErrNISNNotSet}, &deletedKeys{}, nil)
		_, err := useCase.VerifyNISN(context.Background(), uuid.New(), class.ID, uuid.New())
		require.ErrorIs(t, err, apperror.ErrNISNNotSet)
	})
}
