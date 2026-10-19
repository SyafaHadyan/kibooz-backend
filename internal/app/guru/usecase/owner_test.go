package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/guru/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
)

// ownerRepo records what the use case asked for and answers the removal and the transfer from memory
type ownerRepo struct {
	*teacherRepo

	removeErr   error
	transferErr error
	newOwner    *repository.TeacherRow

	removedClass  uuid.UUID
	removedActor  uuid.UUID
	removedTarget uuid.UUID
	movedActor    uuid.UUID
	movedTarget   uuid.UUID
}

func (r *ownerRepo) RemoveTeacher(_ context.Context, classID uuid.UUID, actor uuid.UUID, target uuid.UUID) error {
	r.removedClass, r.removedActor, r.removedTarget = classID, actor, target

	return r.removeErr
}

func (r *ownerRepo) TransferOwnership(_ context.Context, _ uuid.UUID, actor uuid.UUID, target uuid.UUID) (*repository.TeacherRow, error) {
	r.movedActor, r.movedTarget = actor, target

	return r.newOwner, r.transferErr
}

func owning() *ownerRepo {
	base := teachingRepo()
	base.guru = &repository.GuruRow{Guru: entity.Guru{ID: uuid.New()}}

	return &ownerRepo{teacherRepo: base}
}

func TestRemoveTeacherSpeaksForTheCallerAndTheClass(t *testing.T) {
	repo := owning()
	target := uuid.New()

	err := NewGuruUseCase(repo, nil, nil).RemoveTeacher(context.Background(), uuid.New(), repo.class.ID, target)

	require.NoError(t, err)
	require.Equal(t, repo.class.ID, repo.removedClass)
	require.Equal(t, repo.guru.ID, repo.removedActor, "the actor is the teacher of the token and never a name from the request")
	require.Equal(t, target, repo.removedTarget)
}

func TestRemoveTeacherKeepsTheRefusalsAndHidesOtherFailures(t *testing.T) {
	ctx := context.Background()

	for name, refusal := range map[string]*apperror.Error{
		"the last teacher":     apperror.ErrLastTeacher,
		"not the owner":        apperror.ErrNotClassOwner,
		"not a teacher of it":  apperror.ErrTeacherNotInClass,
		"the caller left":      apperror.ErrForbidden,
		"a class that is gone": apperror.ErrClassNotFound,
	} {
		t.Run(name, func(t *testing.T) {
			repo := owning()
			repo.removeErr = refusal

			err := NewGuruUseCase(repo, nil, nil).RemoveTeacher(ctx, uuid.New(), repo.class.ID, uuid.New())

			requireCode(t, err, refusal)
		})
	}

	t.Run("a database that fails", func(t *testing.T) {
		repo := owning()
		repo.removeErr = errors.New("down")

		err := NewGuruUseCase(repo, nil, nil).RemoveTeacher(ctx, uuid.New(), repo.class.ID, uuid.New())

		requireCode(t, err, apperror.Internal(nil))
	})

	t.Run("a class the caller does not teach", func(t *testing.T) {
		repo := &ownerRepo{teacherRepo: &teacherRepo{guru: &repository.GuruRow{}}}

		err := NewGuruUseCase(repo, nil, nil).RemoveTeacher(ctx, uuid.New(), uuid.New(), uuid.New())

		requireCode(t, err, apperror.ErrClassNotFound)
		require.Equal(t, uuid.Nil, repo.removedTarget, "nothing was removed")
	})
}

func TestTransferOwnershipReturnsTheNewOwner(t *testing.T) {
	repo := owning()
	row := teacherRow("Budi")
	row.IsOwner = true
	repo.newOwner = &row

	res, err := NewGuruUseCase(repo, nil, nil).TransferOwnership(
		context.Background(), uuid.New(), repo.class.ID, dto.TransferOwnershipRequest{TeacherID: row.ID},
	)

	require.NoError(t, err)
	require.Equal(t, row.ID, res.ID)
	require.True(t, res.IsOwner)
	require.Equal(t, repo.guru.ID, repo.movedActor)
	require.Equal(t, row.ID, repo.movedTarget)
}

func TestTransferOwnershipFailures(t *testing.T) {
	ctx := context.Background()
	req := dto.TransferOwnershipRequest{TeacherID: uuid.New()}

	t.Run("not the owner", func(t *testing.T) {
		repo := owning()
		repo.transferErr = apperror.ErrNotClassOwner

		_, err := NewGuruUseCase(repo, nil, nil).TransferOwnership(ctx, uuid.New(), repo.class.ID, req)

		requireCode(t, err, apperror.ErrNotClassOwner)
	})

	t.Run("a teacher that left in between", func(t *testing.T) {
		repo := owning()

		_, err := NewGuruUseCase(repo, nil, nil).TransferOwnership(ctx, uuid.New(), repo.class.ID, req)

		requireCode(t, err, apperror.ErrTeacherNotInClass)
	})

	t.Run("a database that fails", func(t *testing.T) {
		repo := owning()
		repo.transferErr = errors.New("down")

		_, err := NewGuruUseCase(repo, nil, nil).TransferOwnership(ctx, uuid.New(), repo.class.ID, req)

		requireCode(t, err, apperror.Internal(nil))
	})

	t.Run("a class the caller does not teach", func(t *testing.T) {
		repo := &ownerRepo{teacherRepo: &teacherRepo{guru: &repository.GuruRow{}}}

		_, err := NewGuruUseCase(repo, nil, nil).TransferOwnership(ctx, uuid.New(), uuid.New(), req)

		requireCode(t, err, apperror.ErrClassNotFound)
	})
}

func TestTeachersShowWhoOwnsTheClass(t *testing.T) {
	repo := owning()
	owner, member := teacherRow("Siti"), teacherRow("Budi")
	owner.IsOwner = true
	repo.teachers = []repository.TeacherRow{owner, member}

	res, err := NewGuruUseCase(repo, nil, nil).Teachers(context.Background(), uuid.New(), repo.class.ID)

	require.NoError(t, err)
	require.True(t, res.Teachers[0].IsOwner)
	require.False(t, res.Teachers[1].IsOwner)
}
