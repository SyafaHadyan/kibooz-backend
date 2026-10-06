package usecase_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/user/usecase"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/s3"
)

func TestUploadAvatarReportsDisabledStorage(t *testing.T) {
	useCase := usecase.NewUserUseCase(nil, s3.Disabled{}, nil)

	_, err := useCase.UploadAvatar(context.Background(), uuid.New(), constants.RoleGuru, nil, []byte("anything"))

	appErr := apperror.As(err)
	require.Equal(t, http.StatusServiceUnavailable, appErr.Status)
	require.Equal(t, "STORAGE_UNAVAILABLE", appErr.Code)
}

type fakeRepo struct {
	user    *entity.User
	classes []uuid.UUID
	err     error
	deleted int
}

func (f *fakeRepo) UpdateUserAvatar(context.Context, uuid.UUID, string) error { return nil }

func (f *fakeRepo) FindWaliIDByUserID(context.Context, uuid.UUID) (*uuid.UUID, error) {
	return nil, nil
}

func (f *fakeRepo) UpdateStudentAvatar(context.Context, uuid.UUID, uuid.UUID, string) (*uuid.UUID, error) {
	return nil, nil
}

func (f *fakeRepo) FindUserByID(context.Context, uuid.UUID) (*entity.User, error) {
	return f.user, nil
}

func (f *fakeRepo) SoftDeleteAccount(context.Context, uuid.UUID, constants.Role) ([]uuid.UUID, error) {
	f.deleted++

	return f.classes, f.err
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

func accountOf(t *testing.T, role constants.Role, password string) *entity.User {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)

	return &entity.User{ID: uuid.New(), Role: role, PasswordHash: string(hash)}
}

func TestDeleteAccount(t *testing.T) {
	ctx := context.Background()

	t.Run("a wrong password deletes nothing", func(t *testing.T) {
		repo := &fakeRepo{user: accountOf(t, constants.RoleWali, "correct-password")}
		useCase := usecase.NewUserUseCase(repo, s3.Disabled{}, &fakeCache{})

		err := useCase.DeleteAccount(ctx, repo.user.ID, constants.RoleWali, "wrong-password")

		appErr := apperror.As(err)
		require.Equal(t, http.StatusForbidden, appErr.Status)
		require.Equal(t, "AUTH_PASSWORD_INCORRECT", appErr.Code)
		require.Zero(t, repo.deleted)
	})

	t.Run("a role that does not match the account is refused", func(t *testing.T) {
		repo := &fakeRepo{user: accountOf(t, constants.RoleWali, "correct-password")}
		useCase := usecase.NewUserUseCase(repo, s3.Disabled{}, &fakeCache{})

		err := useCase.DeleteAccount(ctx, repo.user.ID, constants.RoleGuru, "correct-password")

		require.Equal(t, "AUTH_FORBIDDEN", apperror.As(err).Code)
		require.Zero(t, repo.deleted)
	})

	t.Run("an account that is already gone is reported", func(t *testing.T) {
		useCase := usecase.NewUserUseCase(&fakeRepo{}, s3.Disabled{}, &fakeCache{})

		err := useCase.DeleteAccount(ctx, uuid.New(), constants.RoleWali, "anything")

		require.Equal(t, "PROFILE_NOT_FOUND", apperror.As(err).Code)
	})

	t.Run("the right password deletes and clears the leaderboard of every class", func(t *testing.T) {
		classA, classB := uuid.New(), uuid.New()
		repo := &fakeRepo{user: accountOf(t, constants.RoleWali, "correct-password"), classes: []uuid.UUID{classA, classB}}
		cache := &fakeCache{}
		useCase := usecase.NewUserUseCase(repo, s3.Disabled{}, cache)

		err := useCase.DeleteAccount(ctx, repo.user.ID, constants.RoleWali, "correct-password")

		require.NoError(t, err)
		require.Equal(t, 1, repo.deleted)
		require.Equal(t, []string{
			constants.LeaderboardKeyPrefix + classA.String(),
			constants.LeaderboardKeyPrefix + classB.String(),
		}, cache.deleted)
	})

	t.Run("a database failure becomes an internal error", func(t *testing.T) {
		repo := &fakeRepo{user: accountOf(t, constants.RoleGuru, "correct-password"), err: errors.New("boom")}
		useCase := usecase.NewUserUseCase(repo, s3.Disabled{}, &fakeCache{})

		err := useCase.DeleteAccount(ctx, repo.user.ID, constants.RoleGuru, "correct-password")

		require.Equal(t, http.StatusInternalServerError, apperror.As(err).Status)
	})
}

type fakeStorage struct{ uploads int }

func (f *fakeStorage) Enabled() bool { return true }

func (f *fakeStorage) Upload(context.Context, string, string, []byte) (string, error) {
	f.uploads++

	return "https://example.com/avatar.png", nil
}

// a 1x1 PNG, enough for the image check
var tinyPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41, 0x54, 0x78, 0xda, 0x63, 0x64, 0x60, 0xf8, 0x5f,
	0x0f, 0x00, 0x02, 0x87, 0x01, 0x80, 0xeb, 0x47, 0xba, 0x92, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45,
	0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

func TestUploadAvatarRefusesADeletedAccountBeforeStoring(t *testing.T) {
	storage := &fakeStorage{}
	useCase := usecase.NewUserUseCase(&fakeRepo{}, storage, &fakeCache{})

	_, err := useCase.UploadAvatar(context.Background(), uuid.New(), constants.RoleGuru, nil, tinyPNG)

	require.Equal(t, "PROFILE_NOT_FOUND", apperror.As(err).Code)
	require.Zero(t, storage.uploads)
}

func TestUploadAvatarStoresForAnActiveAccount(t *testing.T) {
	storage := &fakeStorage{}
	repo := &fakeRepo{user: accountOf(t, constants.RoleGuru, "any-password")}
	useCase := usecase.NewUserUseCase(repo, storage, &fakeCache{})

	res, err := useCase.UploadAvatar(context.Background(), repo.user.ID, constants.RoleGuru, nil, tinyPNG)

	require.NoError(t, err)
	require.Equal(t, "https://example.com/avatar.png", res.AvatarURL)
	require.Equal(t, 1, storage.uploads)
}
