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
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
)

// fakeRepo answers the calls of the children use cases and records what it was asked to store
type fakeRepo struct {
	wali       *entity.Wali
	waliErr    error
	children   []repository.StudentRow
	listErr    error
	addErr     error
	findResult *repository.StudentRow
	findErr    error

	addedCode     string
	addedStudent  *entity.Student
	addedMax      int
	addedClass    uuid.UUID
	findRequested *uuid.UUID
}

func (f *fakeRepo) FindWaliByUserID(context.Context, uuid.UUID) (*entity.Wali, error) {
	return f.wali, f.waliErr
}

func (f *fakeRepo) FindStudent(_ context.Context, _ uuid.UUID, studentID *uuid.UUID) (*repository.StudentRow, error) {
	f.findRequested = studentID

	return f.findResult, f.findErr
}

func (f *fakeRepo) LatestMood(context.Context, uuid.UUID, time.Time, time.Time) (*entity.MoodLog, error) {
	return nil, nil
}

func (f *fakeRepo) CountTrashByType(context.Context, uuid.UUID) (map[constants.TrashType]int, error) {
	return nil, nil
}

func (f *fakeRepo) SumTrashByType(context.Context, uuid.UUID) (map[constants.TrashType]repository.TrashTotals, error) {
	return nil, nil
}

func (f *fakeRepo) CountScans(context.Context, uuid.UUID, time.Time, time.Time) (int, error) {
	return 0, nil
}

func (f *fakeRepo) FindGuardian(context.Context, uuid.UUID) (*repository.GuardianRow, error) {
	return nil, nil
}

func (f *fakeRepo) CreateGuidanceApplication(context.Context, *entity.GuidanceApplication) error {
	return nil
}

func (f *fakeRepo) ListChildren(context.Context, uuid.UUID) ([]repository.StudentRow, error) {
	return f.children, f.listErr
}

func (f *fakeRepo) AddChild(
	_ context.Context, _ uuid.UUID, joinCode string, student *entity.Student, maxChildren int,
) (uuid.UUID, error) {
	f.addedCode, f.addedStudent, f.addedMax = joinCode, student, maxChildren

	return f.addedClass, f.addErr
}

type fakeCache struct{ deleted []string }

func (f *fakeCache) Set(context.Context, string, string, time.Duration) error { return nil }

func (f *fakeCache) Get(context.Context, string) (string, bool, error) { return "", false, nil }

func (f *fakeCache) Del(_ context.Context, keys ...string) error {
	f.deleted = append(f.deleted, keys...)

	return nil
}

func (f *fakeCache) MarkUsed(context.Context, string, time.Duration) (bool, error) { return false, nil }

func (f *fakeCache) Available() bool { return true }

func (f *fakeCache) Ping(context.Context) error { return nil }

func newUseCase(repo *fakeRepo, cache *fakeCache) usecase.WaliUseCaseItf {
	return usecase.NewWaliUseCase(repo, cache, &env.Env{})
}

func parentRepo() *fakeRepo {
	return &fakeRepo{wali: &entity.Wali{ID: uuid.New()}}
}

func requireAppError(t *testing.T, err error, want *apperror.Error) {
	t.Helper()

	var got *apperror.Error

	require.ErrorAs(t, err, &got)
	require.Equal(t, want.Code, got.Code)
}

func TestChildrenListsTheChildrenWithTheirClasses(t *testing.T) {
	classID := uuid.New()
	repo := parentRepo()
	repo.children = []repository.StudentRow{
		{Student: entity.Student{ID: uuid.New(), FullName: "Amelia Siti", NISN: "12345", ClassID: classID}, ClassName: "Mawar"},
		{Student: entity.Student{ID: uuid.New(), FullName: "Brother Siti", NISN: "12346", ClassID: classID}, ClassName: "Mawar"},
	}

	res, err := newUseCase(repo, &fakeCache{}).Children(context.Background(), uuid.New())
	require.NoError(t, err)
	require.Len(t, res.Children, 2)
	require.Equal(t, "Amelia Siti", res.Children[0].FullName)
	require.Equal(t, "Mawar", res.Children[0].ClassName)
	require.Equal(t, classID, res.Children[1].ClassID)
}

func TestChildrenOfAParentWithoutChildrenIsAnEmptyListAndNotNull(t *testing.T) {
	res, err := newUseCase(parentRepo(), &fakeCache{}).Children(context.Background(), uuid.New())
	require.NoError(t, err)
	require.NotNil(t, res.Children)
	require.Empty(t, res.Children)
}

func TestChildrenFailures(t *testing.T) {
	t.Run("an account without a parent profile", func(t *testing.T) {
		_, err := newUseCase(&fakeRepo{}, &fakeCache{}).Children(context.Background(), uuid.New())
		requireAppError(t, err, apperror.ErrProfileNotFound)
	})

	t.Run("a database that fails on the profile", func(t *testing.T) {
		_, err := newUseCase(&fakeRepo{waliErr: errors.New("down")}, &fakeCache{}).Children(context.Background(), uuid.New())
		requireAppError(t, err, apperror.Internal(nil))
	})

	t.Run("a database that fails on the list", func(t *testing.T) {
		repo := parentRepo()
		repo.listErr = errors.New("down")

		_, err := newUseCase(repo, &fakeCache{}).Children(context.Background(), uuid.New())
		requireAppError(t, err, apperror.Internal(nil))
	})
}

func addRequest() dto.AddChildRequest {
	return dto.AddChildRequest{
		ClassCode: " k7m2qx4r ",
		Student:   dto.RegisterStudent{NISN: " 99001 ", FullName: "  Cousin Siti "},
	}
}

func TestAddChildStoresTrimmedInputAndDropsTheLeaderboardCache(t *testing.T) {
	classID := uuid.New()
	repo := parentRepo()
	repo.addedClass = classID
	repo.findResult = &repository.StudentRow{
		Student:   entity.Student{ID: uuid.New(), FullName: "Cousin Siti", NISN: "99001", ClassID: classID},
		ClassName: "Mawar",
	}
	cache := &fakeCache{}

	res, err := newUseCase(repo, cache).AddChild(context.Background(), uuid.New(), addRequest())
	require.NoError(t, err)

	require.Equal(t, "K7M2QX4R", repo.addedCode, "the code is trimmed and upper-cased")
	require.Equal(t, "99001", repo.addedStudent.NISN)
	require.Equal(t, "Cousin Siti", repo.addedStudent.FullName)
	require.Equal(t, usecase.MaxChildren, repo.addedMax)
	require.NotNil(t, repo.findRequested)
	require.Equal(t, repo.addedStudent.ID, *repo.findRequested, "the answer is the child that was just stored")
	require.Equal(t, []string{constants.LeaderboardKeyPrefix + classID.String()}, cache.deleted)
	require.Equal(t, "Mawar", res.ClassName)
}

func TestAddChildFailures(t *testing.T) {
	ctx := context.Background()

	t.Run("an account without a parent profile", func(t *testing.T) {
		_, err := newUseCase(&fakeRepo{}, &fakeCache{}).AddChild(ctx, uuid.New(), addRequest())
		requireAppError(t, err, apperror.ErrProfileNotFound)
	})

	t.Run("a database that fails on the profile", func(t *testing.T) {
		_, err := newUseCase(&fakeRepo{waliErr: errors.New("down")}, &fakeCache{}).AddChild(ctx, uuid.New(), addRequest())
		requireAppError(t, err, apperror.Internal(nil))
	})

	t.Run("the rules of the repository reach the caller as they are", func(t *testing.T) {
		for _, rule := range []*apperror.Error{
			apperror.ErrClassCodeNotFound, apperror.ErrClassNoTeacher, apperror.ErrChildLimitReached, apperror.ErrNISNTaken,
		} {
			repo := parentRepo()
			repo.addErr = rule
			cache := &fakeCache{}

			_, err := newUseCase(repo, cache).AddChild(ctx, uuid.New(), addRequest())
			requireAppError(t, err, rule)
			require.Empty(t, cache.deleted, "nothing changed, so the cache stays")
		}
	})

	t.Run("a database error on the insert is hidden behind an internal error", func(t *testing.T) {
		repo := parentRepo()
		repo.addErr = errors.New("down")

		_, err := newUseCase(repo, &fakeCache{}).AddChild(ctx, uuid.New(), addRequest())
		requireAppError(t, err, apperror.Internal(nil))
	})

	t.Run("a child that cannot be read back", func(t *testing.T) {
		repo := parentRepo()
		repo.findErr = errors.New("down")

		_, err := newUseCase(repo, &fakeCache{}).AddChild(ctx, uuid.New(), addRequest())
		requireAppError(t, err, apperror.Internal(nil))

		repo = parentRepo()

		_, err = newUseCase(repo, &fakeCache{}).AddChild(ctx, uuid.New(), addRequest())
		requireAppError(t, err, apperror.ErrStudentNotFound)
	})
}
