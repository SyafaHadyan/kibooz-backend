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
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
)

// teacherRepo answers only what the teacher use cases ask for, any other call panics on the nil interface
type teacherRepo struct {
	repository.GuruDBItf

	guru     *repository.GuruRow
	guruErr  error
	class    *entity.Class
	classErr error

	teachers   []repository.TeacherRow
	listErr    error
	added      *repository.TeacherRow
	addErr     error
	addedEmail string
	addedClass uuid.UUID
}

func (r *teacherRepo) FindGuruByUserID(context.Context, uuid.UUID) (*repository.GuruRow, error) {
	return r.guru, r.guruErr
}

func (r *teacherRepo) FindTaughtClass(context.Context, uuid.UUID, *uuid.UUID) (*entity.Class, error) {
	return r.class, r.classErr
}

func (r *teacherRepo) ListTeachers(context.Context, uuid.UUID) ([]repository.TeacherRow, error) {
	return r.teachers, r.listErr
}

func (r *teacherRepo) AddTeacher(_ context.Context, classID uuid.UUID, email string) (*repository.TeacherRow, error) {
	r.addedClass, r.addedEmail = classID, email

	return r.added, r.addErr
}

func teachingRepo() *teacherRepo {
	return &teacherRepo{guru: &repository.GuruRow{}, class: &entity.Class{ID: uuid.New()}}
}

func requireCode(t *testing.T, err error, want *apperror.Error) {
	t.Helper()

	var got *apperror.Error

	require.ErrorAs(t, err, &got)
	require.Equal(t, want.Code, got.Code)
}

func teacherRow(name string) repository.TeacherRow {
	nip := "1980" + name
	row := repository.TeacherRow{FullName: name, JoinedAt: time.Now()}
	row.ID = uuid.New()
	row.NIP = &nip

	return row
}

func TestTeachersListsTheTeachersOfTheClass(t *testing.T) {
	repo := teachingRepo()
	repo.teachers = []repository.TeacherRow{teacherRow("Siti"), teacherRow("Budi")}

	res, err := NewGuruUseCase(repo, nil).Teachers(context.Background(), uuid.New(), repo.class.ID)
	require.NoError(t, err)
	require.Len(t, res.Teachers, 2)
	require.Equal(t, "Siti", res.Teachers[0].FullName)
	require.Equal(t, repo.teachers[1].ID, res.Teachers[1].ID)
	require.Equal(t, "1980Siti", *res.Teachers[0].NIP)
}

func TestTeachersFailures(t *testing.T) {
	ctx := context.Background()

	t.Run("a class the caller does not teach", func(t *testing.T) {
		repo := &teacherRepo{guru: &repository.GuruRow{}}

		_, err := NewGuruUseCase(repo, nil).Teachers(ctx, uuid.New(), uuid.New())
		requireCode(t, err, apperror.ErrClassNotFound)
	})

	t.Run("a database that fails on the list", func(t *testing.T) {
		repo := teachingRepo()
		repo.listErr = errors.New("down")

		_, err := NewGuruUseCase(repo, nil).Teachers(ctx, uuid.New(), repo.class.ID)
		requireCode(t, err, apperror.Internal(nil))
	})
}

func TestAddTeacherNormalizesTheEmailAndReturnsTheTeacher(t *testing.T) {
	repo := teachingRepo()
	row := teacherRow("Budi")
	repo.added = &row

	res, err := NewGuruUseCase(repo, nil).AddTeacher(
		context.Background(), uuid.New(), repo.class.ID, dto.AddTeacherRequest{Email: "  Budi@Example.COM "},
	)
	require.NoError(t, err)
	require.Equal(t, "budi@example.com", repo.addedEmail)
	require.Equal(t, repo.class.ID, repo.addedClass)
	require.Equal(t, row.ID, res.ID)
	require.Equal(t, "Budi", res.FullName)
}

func TestAddTeacherFailures(t *testing.T) {
	ctx := context.Background()
	req := dto.AddTeacherRequest{Email: "budi@example.com"}

	t.Run("a class the caller does not teach", func(t *testing.T) {
		repo := &teacherRepo{guru: &repository.GuruRow{}}

		_, err := NewGuruUseCase(repo, nil).AddTeacher(ctx, uuid.New(), uuid.New(), req)
		requireCode(t, err, apperror.ErrClassNotFound)
		require.Empty(t, repo.addedEmail, "nothing was added")
	})

	t.Run("an email that is no teacher account", func(t *testing.T) {
		repo := teachingRepo()

		_, err := NewGuruUseCase(repo, nil).AddTeacher(ctx, uuid.New(), repo.class.ID, req)
		requireCode(t, err, apperror.ErrTeacherNotFound)
	})

	t.Run("a teacher who is already in the class", func(t *testing.T) {
		repo := teachingRepo()
		repo.addErr = apperror.ErrTeacherInClass

		_, err := NewGuruUseCase(repo, nil).AddTeacher(ctx, uuid.New(), repo.class.ID, req)
		requireCode(t, err, apperror.ErrTeacherInClass)
	})

	t.Run("a database error is hidden behind an internal error", func(t *testing.T) {
		repo := teachingRepo()
		repo.addErr = errors.New("down")

		_, err := NewGuruUseCase(repo, nil).AddTeacher(ctx, uuid.New(), repo.class.ID, req)
		requireCode(t, err, apperror.Internal(nil))
	})
}
