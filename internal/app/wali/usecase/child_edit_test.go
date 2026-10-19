package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/wali/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
)

func TestUpdateChildStoresTheFieldsAndClearsTheLeaderboard(t *testing.T) {
	classID := uuid.New()
	childID := uuid.New()
	repo := parentRepo()
	repo.studentFound, repo.studentClass = true, classID
	repo.findResult = &repository.StudentRow{
		Student: entity.Student{ID: childID, FullName: "Amelia Baru", NISN: "98765", ClassID: classID}, ClassName: "Mawar",
	}
	cache := &fakeCache{}

	res, err := newUseCase(repo, cache).UpdateChild(context.Background(), uuid.New(), childID, dto.UpdateChildRequest{
		FullName: text("  Amelia Baru  "), NISN: text(" 98765 "),
	})

	require.NoError(t, err)
	require.Equal(t, "Amelia Baru", res.FullName)
	require.Equal(t, "98765", res.NISN)
	require.True(t, repo.studentUpdated.SetName)
	require.Equal(t, "Amelia Baru", repo.studentUpdated.FullName)
	require.True(t, repo.studentUpdated.SetNISN)
	require.Equal(t, "98765", repo.studentUpdated.NISN)
	require.Equal(t, []string{constants.LeaderboardKeyPrefix + classID.String()}, cache.deleted)
}

func TestUpdateChildCanChangeOnlyOneField(t *testing.T) {
	repo := parentRepo()
	repo.studentFound = true
	repo.findResult = &repository.StudentRow{Student: entity.Student{ID: uuid.New()}}

	_, err := newUseCase(repo, &fakeCache{}).UpdateChild(context.Background(), uuid.New(), uuid.New(), dto.UpdateChildRequest{NISN: text("12345")})

	require.NoError(t, err)
	require.False(t, repo.studentUpdated.SetName)
	require.True(t, repo.studentUpdated.SetNISN)
}

func TestUpdateChildRefusesBadInput(t *testing.T) {
	cases := map[string]dto.UpdateChildRequest{
		"nothing to change": {},
		"a short name":      {FullName: text(" a ")},
		"an empty name":     {FullName: text("")},
		"a NISN with text":  {NISN: text("12a45")},
		"a short NISN":      {NISN: text("1234")},
		"a long NISN":       {NISN: text("1234567890123456789012345678901")},
	}

	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			repo := parentRepo()

			_, err := newUseCase(repo, &fakeCache{}).UpdateChild(context.Background(), uuid.New(), uuid.New(), req)

			requireAppError(t, err, apperror.Validation(nil))
			require.Nil(t, repo.studentUpdated, "nothing is stored")
		})
	}
}

func TestUpdateChildRefusesWhatItCannotFind(t *testing.T) {
	req := dto.UpdateChildRequest{FullName: text("Amelia")}

	t.Run("an account without a parent profile", func(t *testing.T) {
		_, err := newUseCase(&fakeRepo{}, &fakeCache{}).UpdateChild(context.Background(), uuid.New(), uuid.New(), req)

		requireAppError(t, err, apperror.ErrProfileNotFound)
	})

	t.Run("a child that is not theirs", func(t *testing.T) {
		cache := &fakeCache{}

		_, err := newUseCase(parentRepo(), cache).UpdateChild(context.Background(), uuid.New(), uuid.New(), req)

		requireAppError(t, err, apperror.ErrStudentNotFound)
		require.Empty(t, cache.deleted, "nothing changed, so the cache stays")
	})

	t.Run("a child that disappears before it is read again", func(t *testing.T) {
		repo := parentRepo()
		repo.studentFound = true

		_, err := newUseCase(repo, &fakeCache{}).UpdateChild(context.Background(), uuid.New(), uuid.New(), req)

		requireAppError(t, err, apperror.ErrStudentNotFound)
	})
}

func TestUpdateChildKeepsTheRefusalOfTheRepositoryAndHidesOtherFailures(t *testing.T) {
	req := dto.UpdateChildRequest{NISN: text("12345")}

	repo := parentRepo()
	repo.studentErr = apperror.ErrNISNTaken

	_, err := newUseCase(repo, &fakeCache{}).UpdateChild(context.Background(), uuid.New(), uuid.New(), req)

	requireAppError(t, err, apperror.ErrNISNTaken)

	repo = parentRepo()
	repo.studentErr = errors.New("connection lost")

	_, err = newUseCase(repo, &fakeCache{}).UpdateChild(context.Background(), uuid.New(), uuid.New(), req)

	var got *apperror.Error

	require.ErrorAs(t, err, &got)
	require.Equal(t, 500, got.Status)
}
