package usecase_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/user/repository"
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
	files   []string
	err     error
	deleted int

	// waliID and studentClass drive the student avatar path, a nil studentClass means the child is not theirs
	waliID       *uuid.UUID
	studentClass *uuid.UUID

	// previous is the avatar URL the update replaces, userGone means the account vanished before the update
	previous  string
	userGone  bool
	updateErr error
}

func (f *fakeRepo) UpdateUserAvatar(context.Context, uuid.UUID, string) (string, bool, error) {
	return f.previous, !f.userGone, f.updateErr
}

func (f *fakeRepo) FindWaliIDByUserID(context.Context, uuid.UUID) (*uuid.UUID, error) {
	return f.waliID, nil
}

func (f *fakeRepo) UpdateStudentAvatar(context.Context, uuid.UUID, uuid.UUID, string) (*uuid.UUID, string, error) {
	return f.studentClass, f.previous, f.updateErr
}

func (f *fakeRepo) FindUserByID(context.Context, uuid.UUID) (*entity.User, error) {
	return f.user, nil
}

func (f *fakeRepo) SoftDeleteAccount(context.Context, uuid.UUID, constants.Role) (repository.DeletedAccount, error) {
	f.deleted++

	return repository.DeletedAccount{ClassIDs: f.classes, FileURLs: f.files}, f.err
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

	t.Run("a password longer than bcrypt reads cannot match a shorter one", func(t *testing.T) {
		registered := strings.Repeat("a", 70) + "é"
		repo := &fakeRepo{user: accountOf(t, constants.RoleWali, registered)}
		useCase := usecase.NewUserUseCase(repo, s3.Disabled{}, &fakeCache{})

		err := useCase.DeleteAccount(ctx, repo.user.ID, constants.RoleWali, registered+"z")

		require.Equal(t, "AUTH_PASSWORD_INCORRECT", apperror.As(err).Code)
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

type fakeStorage struct {
	s3.Disabled

	mu      sync.Mutex
	uploads int
	keys    []string
	deleted []string
}

func (f *fakeStorage) Enabled() bool { return true }

func (f *fakeStorage) Upload(_ context.Context, key string, _ string, _ []byte) (string, error) {
	f.uploads++
	f.keys = append(f.keys, key)

	return "https://example.com/avatar.png", nil
}

// KeyFromURL knows the one public base URL the fake hands out
func (f *fakeStorage) KeyFromURL(url string) (string, bool) {
	return strings.CutPrefix(url, "https://example.com/")
}

func (f *fakeStorage) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.deleted = append(f.deleted, key)

	return nil
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

func TestUploadAvatarForAnotherParentsChildLeavesNoFile(t *testing.T) {
	storage := &fakeStorage{}
	waliID := uuid.New()
	repo := &fakeRepo{waliID: &waliID}
	useCase := usecase.NewUserUseCase(repo, storage, &fakeCache{})
	studentID := uuid.New()

	_, err := useCase.UploadAvatar(context.Background(), uuid.New(), constants.RoleWali, &studentID, tinyPNG)

	require.Equal(t, "STUDENT_NOT_FOUND", apperror.As(err).Code)
	require.Equal(t, 1, storage.uploads)
	require.Equal(t, storage.keys, storage.deleted, "the stored file is removed again")
}

func TestUploadAvatarForOwnChildKeepsTheFile(t *testing.T) {
	storage := &fakeStorage{}
	waliID, classID := uuid.New(), uuid.New()
	repo := &fakeRepo{waliID: &waliID, studentClass: &classID}
	useCase := usecase.NewUserUseCase(repo, storage, &fakeCache{})
	studentID := uuid.New()

	_, err := useCase.UploadAvatar(context.Background(), uuid.New(), constants.RoleWali, &studentID, tinyPNG)

	require.NoError(t, err)
	require.Equal(t, 1, storage.uploads)
	require.Empty(t, storage.deleted)
}

func TestUploadAvatarDeletesTheReplacedFile(t *testing.T) {
	userID := uuid.New()
	old := "avatars/" + userID.String() + "/old.png"

	t.Run("user avatar", func(t *testing.T) {
		storage := &fakeStorage{}
		repo := &fakeRepo{user: accountOf(t, constants.RoleGuru, "any-password"), previous: "https://example.com/" + old}
		useCase := usecase.NewUserUseCase(repo, storage, &fakeCache{})

		_, err := useCase.UploadAvatar(context.Background(), repo.user.ID, constants.RoleGuru, nil, tinyPNG)

		require.NoError(t, err)
		require.Equal(t, []string{old}, storage.deleted)
	})

	t.Run("child avatar", func(t *testing.T) {
		storage := &fakeStorage{}
		waliID, classID := uuid.New(), uuid.New()
		repo := &fakeRepo{waliID: &waliID, studentClass: &classID, previous: "https://example.com/" + old}
		useCase := usecase.NewUserUseCase(repo, storage, &fakeCache{})
		studentID := uuid.New()

		_, err := useCase.UploadAvatar(context.Background(), uuid.New(), constants.RoleWali, &studentID, tinyPNG)

		require.NoError(t, err)
		require.Equal(t, []string{old}, storage.deleted)
	})
}

func TestUploadAvatarKeepsFilesItDoesNotOwn(t *testing.T) {
	tests := map[string]string{
		"no previous avatar":           "",
		"another bucket":               "https://cdn.other.example/avatars/x/old.png",
		"outside the avatar directory": "https://example.com/trash-scans/x/old.png",
		"directory only":               "https://example.com/avatars",
	}

	for name, previous := range tests {
		t.Run(name, func(t *testing.T) {
			storage := &fakeStorage{}
			repo := &fakeRepo{user: accountOf(t, constants.RoleGuru, "any-password"), previous: previous}
			useCase := usecase.NewUserUseCase(repo, storage, &fakeCache{})

			_, err := useCase.UploadAvatar(context.Background(), repo.user.ID, constants.RoleGuru, nil, tinyPNG)

			require.NoError(t, err)
			require.Empty(t, storage.deleted)
		})
	}
}

func TestUploadAvatarKeepsTheOldFileWhenTheUpdateFails(t *testing.T) {
	storage := &fakeStorage{}
	repo := &fakeRepo{
		user:      accountOf(t, constants.RoleGuru, "any-password"),
		previous:  "https://example.com/avatars/x/old.png",
		updateErr: errors.New("boom"),
	}
	useCase := usecase.NewUserUseCase(repo, storage, &fakeCache{})

	_, err := useCase.UploadAvatar(context.Background(), repo.user.ID, constants.RoleGuru, nil, tinyPNG)

	require.Error(t, err)
	require.Equal(t, storage.keys, storage.deleted, "only the new file is removed again")
}

func TestUploadAvatarForAnAccountDeletedMeanwhileLeavesNoFile(t *testing.T) {
	storage := &fakeStorage{}
	repo := &fakeRepo{user: accountOf(t, constants.RoleGuru, "any-password"), userGone: true}
	useCase := usecase.NewUserUseCase(repo, storage, &fakeCache{})

	_, err := useCase.UploadAvatar(context.Background(), repo.user.ID, constants.RoleGuru, nil, tinyPNG)

	require.Equal(t, "PROFILE_NOT_FOUND", apperror.As(err).Code)
	require.Equal(t, storage.keys, storage.deleted)
}

func TestDeleteAccountDeletesTheFilesOfTheAccount(t *testing.T) {
	ctx := context.Background()

	t.Run("only avatars and trash photos of this storage are deleted", func(t *testing.T) {
		repo := &fakeRepo{
			user: accountOf(t, constants.RoleWali, "correct-password"),
			files: []string{
				"https://example.com/avatars/user/a.png",
				"https://example.com/avatars/child/b.jpg",
				"https://example.com/trash-scans/child/scan.png",
				"https://example.com/videos/class/lesson.mp4",
				"https://example.com/pending/upload.mp4",
				"https://elsewhere.test/avatars/user/c.png",
			},
		}
		storage := &fakeStorage{}
		useCase := usecase.NewUserUseCase(repo, storage, &fakeCache{})

		require.NoError(t, useCase.DeleteAccount(ctx, repo.user.ID, constants.RoleWali, "correct-password"))

		require.ElementsMatch(t, []string{
			"avatars/user/a.png",
			"avatars/child/b.jpg",
			"trash-scans/child/scan.png",
		}, storage.deleted)
	})

	t.Run("many photos are all deleted", func(t *testing.T) {
		repo := &fakeRepo{user: accountOf(t, constants.RoleWali, "correct-password")}
		for i := range 40 {
			repo.files = append(repo.files, fmt.Sprintf("https://example.com/trash-scans/child/%d.png", i))
		}

		storage := &fakeStorage{}
		useCase := usecase.NewUserUseCase(repo, storage, &fakeCache{})

		require.NoError(t, useCase.DeleteAccount(ctx, repo.user.ID, constants.RoleWali, "correct-password"))
		require.Len(t, storage.deleted, 40)
	})

	t.Run("nothing is deleted when the account was not", func(t *testing.T) {
		repo := &fakeRepo{
			user:  accountOf(t, constants.RoleGuru, "correct-password"),
			files: []string{"https://example.com/avatars/user/a.png"},
			err:   errors.New("boom"),
		}
		storage := &fakeStorage{}
		useCase := usecase.NewUserUseCase(repo, storage, &fakeCache{})

		require.Error(t, useCase.DeleteAccount(ctx, repo.user.ID, constants.RoleGuru, "correct-password"))
		require.Empty(t, storage.deleted)
	})

	t.Run("a disabled storage deletes nothing and does not fail", func(t *testing.T) {
		repo := &fakeRepo{
			user:  accountOf(t, constants.RoleGuru, "correct-password"),
			files: []string{"https://example.com/avatars/user/a.png"},
		}
		useCase := usecase.NewUserUseCase(repo, s3.Disabled{}, &fakeCache{})

		require.NoError(t, useCase.DeleteAccount(ctx, repo.user.ID, constants.RoleGuru, "correct-password"))
	})
}
