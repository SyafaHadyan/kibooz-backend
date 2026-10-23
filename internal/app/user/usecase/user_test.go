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
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/passwords"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/s3"
)

// hasher is what every use case of these tests shares, with room for any number of requests at once
var hasher = passwords.New(&env.Env{PasswordHashConcurrency: 64, PasswordHashWaitMilliseconds: 1000})

func TestUploadAvatarReportsDisabledStorage(t *testing.T) {
	useCase := usecase.NewUserUseCase(nil, s3.Disabled{}, nil, hasher)

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

	// successor is the email the last deletion passed on, and verifiedHash the hash it was bound to
	successor    string
	verifiedHash string

	// queued holds the keys the deletion put on the cleanup queue, unqueued the ones taken off it again
	queued     []string
	unqueued   []string
	unqueueErr error

	// waliID and studentClass drive the student avatar path, a nil studentClass means the child is not theirs
	waliID       *uuid.UUID
	studentClass *uuid.UUID

	// previous is the avatar URL the update replaces, userGone means the account vanished before the update
	previous  string
	userGone  bool
	updateErr error

	// readURL is the avatar address that a read after a failed update finds, readGone means there is no such row any more, and readErr
	// makes the read fail
	readURL  string
	readGone bool
	readErr  error
	// readFor holds the ids that the read was asked about and readCtxErr the state of its context
	readFor    []uuid.UUID
	readCtxErr error

	// passwordUpdates counts the password changes that were asked for, and passwordStale makes them find a hash that moved on
	passwordUpdates int
	passwordStale   bool
	newHash         string
}

func (f *fakeRepo) UpdatePassword(_ context.Context, _ uuid.UUID, _ string, newHash string) (bool, error) {
	f.passwordUpdates++
	f.newHash = newHash

	return !f.passwordStale, f.err
}

func (f *fakeRepo) UpdateUserAvatar(context.Context, uuid.UUID, string) (string, bool, error) {
	return f.previous, !f.userGone, f.updateErr
}

func (f *fakeRepo) UserAvatar(ctx context.Context, userID uuid.UUID) (string, bool, error) {
	f.readFor = []uuid.UUID{userID}
	f.readCtxErr = ctx.Err()

	return f.readURL, !f.readGone, f.readErr
}

func (f *fakeRepo) StudentAvatar(_ context.Context, waliID uuid.UUID, studentID uuid.UUID) (string, uuid.UUID, bool, error) {
	f.readFor = []uuid.UUID{waliID, studentID}

	var class uuid.UUID
	if f.studentClass != nil {
		class = *f.studentClass
	}

	return f.readURL, class, !f.readGone, f.readErr
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

func (f *fakeRepo) SoftDeleteAccount(
	_ context.Context, _ uuid.UUID, _ constants.Role, verifiedHash string, successorEmail string, fileKey repository.FileKeyFunc,
) (repository.DeletedAccount, error) {
	f.deleted++
	f.verifiedHash = verifiedHash
	f.successor = successorEmail

	if f.err != nil {
		return repository.DeletedAccount{}, f.err
	}

	var keys []string

	for _, url := range f.files {
		if key, ok := fileKey(url); ok {
			keys = append(keys, key)
		}
	}

	f.queued = keys

	return repository.DeletedAccount{ClassIDs: f.classes, FileKeys: keys}, nil
}

func (f *fakeRepo) UnqueueFileCleanups(_ context.Context, keys []string) error {
	f.unqueued = append(f.unqueued, keys...)

	return f.unqueueErr
}

type fakeCache struct{ deleted []string }

func (f *fakeCache) Set(context.Context, string, string, time.Duration) error { return nil }

func (f *fakeCache) Get(context.Context, string) (string, bool, error) { return "", false, nil }

func (f *fakeCache) Del(_ context.Context, keys ...string) error {
	f.deleted = append(f.deleted, keys...)

	return nil
}

func (f *fakeCache) Available() bool { return true }

func (f *fakeCache) Ping(context.Context) error { return nil }

func accountOf(t *testing.T, role constants.Role, password string) *entity.User {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)

	return &entity.User{ID: uuid.New(), Role: role, PasswordHash: string(hash)}
}

// A server whose every slot is busy hashing refuses the request with the code of a rate limit before it changes anything
func TestARequestThatChecksAPasswordIsRefusedWhileEverySlotIsTaken(t *testing.T) {
	busy := passwords.New(&env.Env{PasswordHashConcurrency: 1, PasswordHashWaitMilliseconds: 10})

	release, err := busy.Acquire(context.Background())
	require.NoError(t, err)

	defer release()

	ctx := context.Background()

	t.Run("deleting the account", func(t *testing.T) {
		repo := &fakeRepo{user: accountOf(t, constants.RoleWali, "correct-password")}
		useCase := usecase.NewUserUseCase(repo, s3.Disabled{}, &fakeCache{}, busy)

		err := useCase.DeleteAccount(ctx, repo.user.ID, constants.RoleWali, dto.DeleteAccountRequest{Password: "correct-password"})

		appErr := apperror.As(err)
		require.Equal(t, http.StatusTooManyRequests, appErr.Status)
		require.Equal(t, "RATE_LIMITED", appErr.Code)
		require.Zero(t, repo.deleted, "nothing was deleted")
	})

	t.Run("changing the password", func(t *testing.T) {
		repo := &fakeRepo{user: accountOf(t, constants.RoleWali, "correct-password")}
		useCase := usecase.NewUserUseCase(repo, s3.Disabled{}, &fakeCache{}, busy)

		err := useCase.ChangePassword(ctx, repo.user.ID, constants.RoleWali,
			dto.ChangePasswordRequest{CurrentPassword: "correct-password", NewPassword: "another-password"})

		require.Equal(t, "RATE_LIMITED", apperror.As(err).Code)
	})
}

func TestDeleteAccount(t *testing.T) {
	ctx := context.Background()

	t.Run("a wrong password deletes nothing", func(t *testing.T) {
		repo := &fakeRepo{user: accountOf(t, constants.RoleWali, "correct-password")}
		useCase := usecase.NewUserUseCase(repo, s3.Disabled{}, &fakeCache{}, hasher)

		err := useCase.DeleteAccount(ctx, repo.user.ID, constants.RoleWali, dto.DeleteAccountRequest{Password: "wrong-password"})

		appErr := apperror.As(err)
		require.Equal(t, http.StatusForbidden, appErr.Status)
		require.Equal(t, "AUTH_PASSWORD_INCORRECT", appErr.Code)
		require.Zero(t, repo.deleted)
	})

	t.Run("the deletion is bound to the hash that the password was checked against", func(t *testing.T) {
		repo := &fakeRepo{user: accountOf(t, constants.RoleWali, "correct-password")}
		useCase := usecase.NewUserUseCase(repo, s3.Disabled{}, &fakeCache{}, hasher)

		err := useCase.DeleteAccount(ctx, repo.user.ID, constants.RoleWali, dto.DeleteAccountRequest{Password: "correct-password"})

		require.NoError(t, err)
		require.Equal(t, repo.user.PasswordHash, repo.verifiedHash)
	})

	t.Run("a password longer than bcrypt reads cannot match a shorter one", func(t *testing.T) {
		registered := strings.Repeat("a", 70) + "é"
		repo := &fakeRepo{user: accountOf(t, constants.RoleWali, registered)}
		useCase := usecase.NewUserUseCase(repo, s3.Disabled{}, &fakeCache{}, hasher)

		err := useCase.DeleteAccount(ctx, repo.user.ID, constants.RoleWali, dto.DeleteAccountRequest{Password: registered + "z"})

		require.Equal(t, "AUTH_PASSWORD_INCORRECT", apperror.As(err).Code)
		require.Zero(t, repo.deleted)
	})

	t.Run("a role that does not match the account is refused", func(t *testing.T) {
		repo := &fakeRepo{user: accountOf(t, constants.RoleWali, "correct-password")}
		useCase := usecase.NewUserUseCase(repo, s3.Disabled{}, &fakeCache{}, hasher)

		err := useCase.DeleteAccount(ctx, repo.user.ID, constants.RoleGuru, dto.DeleteAccountRequest{Password: "correct-password"})

		require.Equal(t, "AUTH_FORBIDDEN", apperror.As(err).Code)
		require.Zero(t, repo.deleted)
	})

	t.Run("an account that is already gone is reported", func(t *testing.T) {
		useCase := usecase.NewUserUseCase(&fakeRepo{}, s3.Disabled{}, &fakeCache{}, hasher)

		err := useCase.DeleteAccount(ctx, uuid.New(), constants.RoleWali, dto.DeleteAccountRequest{Password: "anything"})

		require.Equal(t, "PROFILE_NOT_FOUND", apperror.As(err).Code)
	})

	t.Run("the right password deletes and clears the leaderboard of every class", func(t *testing.T) {
		classA, classB := uuid.New(), uuid.New()
		repo := &fakeRepo{user: accountOf(t, constants.RoleWali, "correct-password"), classes: []uuid.UUID{classA, classB}}
		cache := &fakeCache{}
		useCase := usecase.NewUserUseCase(repo, s3.Disabled{}, cache, hasher)

		err := useCase.DeleteAccount(ctx, repo.user.ID, constants.RoleWali, dto.DeleteAccountRequest{Password: "correct-password"})

		require.NoError(t, err)
		require.Equal(t, 1, repo.deleted)
		require.Equal(t, []string{
			constants.LeaderboardKeyPrefix + classA.String(),
			constants.LeaderboardKeyPrefix + classB.String(),
		}, cache.deleted)
	})

	t.Run("the successor email is trimmed and lowercased before it reaches the database", func(t *testing.T) {
		repo := &fakeRepo{user: accountOf(t, constants.RoleGuru, "correct-password")}
		useCase := usecase.NewUserUseCase(repo, s3.Disabled{}, &fakeCache{}, hasher)

		err := useCase.DeleteAccount(ctx, repo.user.ID, constants.RoleGuru, dto.DeleteAccountRequest{
			Password: "correct-password", SuccessorEmail: "  Next.Teacher@Example.com ",
		})

		require.NoError(t, err)
		require.Equal(t, "next.teacher@example.com", repo.successor)
	})

	t.Run("a refusal of the database reaches the caller as it is", func(t *testing.T) {
		repo := &fakeRepo{user: accountOf(t, constants.RoleGuru, "correct-password"), err: apperror.ErrSuccessorRequired}
		useCase := usecase.NewUserUseCase(repo, s3.Disabled{}, &fakeCache{}, hasher)

		err := useCase.DeleteAccount(ctx, repo.user.ID, constants.RoleGuru, dto.DeleteAccountRequest{Password: "correct-password"})

		appErr := apperror.As(err)
		require.Equal(t, http.StatusConflict, appErr.Status)
		require.Equal(t, "SUCCESSOR_REQUIRED", appErr.Code)
	})

	t.Run("a wrapped refusal is still an answer and not a failure", func(t *testing.T) {
		repo := &fakeRepo{
			user: accountOf(t, constants.RoleGuru, "correct-password"),
			err:  fmt.Errorf("transaction: %w", apperror.ErrTeacherNotFound),
		}
		useCase := usecase.NewUserUseCase(repo, s3.Disabled{}, &fakeCache{}, hasher)

		err := useCase.DeleteAccount(ctx, repo.user.ID, constants.RoleGuru, dto.DeleteAccountRequest{Password: "correct-password"})

		require.Equal(t, "TEACHER_NOT_FOUND", apperror.As(err).Code)
	})

	t.Run("a database failure becomes an internal error", func(t *testing.T) {
		repo := &fakeRepo{user: accountOf(t, constants.RoleGuru, "correct-password"), err: errors.New("boom")}
		useCase := usecase.NewUserUseCase(repo, s3.Disabled{}, &fakeCache{}, hasher)

		err := useCase.DeleteAccount(ctx, repo.user.ID, constants.RoleGuru, dto.DeleteAccountRequest{Password: "correct-password"})

		require.Equal(t, http.StatusInternalServerError, apperror.As(err).Status)
	})
}

type fakeStorage struct {
	s3.Disabled

	mu      sync.Mutex
	uploads int
	keys    []string
	deleted []string
	// failing lists the keys whose deletion is refused
	failing map[string]bool
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

	if f.failing[key] {
		return errors.New("bucket refused the delete")
	}

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
	useCase := usecase.NewUserUseCase(&fakeRepo{}, storage, &fakeCache{}, hasher)

	_, err := useCase.UploadAvatar(context.Background(), uuid.New(), constants.RoleGuru, nil, tinyPNG)

	require.Equal(t, "PROFILE_NOT_FOUND", apperror.As(err).Code)
	require.Zero(t, storage.uploads)
}

func TestUploadAvatarStoresForAnActiveAccount(t *testing.T) {
	storage := &fakeStorage{}
	repo := &fakeRepo{user: accountOf(t, constants.RoleGuru, "any-password")}
	useCase := usecase.NewUserUseCase(repo, storage, &fakeCache{}, hasher)

	res, err := useCase.UploadAvatar(context.Background(), repo.user.ID, constants.RoleGuru, nil, tinyPNG)

	require.NoError(t, err)
	require.Equal(t, "https://example.com/avatar.png", res.AvatarURL)
	require.Equal(t, 1, storage.uploads)
}

func TestUploadAvatarForAnotherParentsChildLeavesNoFile(t *testing.T) {
	storage := &fakeStorage{}
	waliID := uuid.New()
	repo := &fakeRepo{waliID: &waliID}
	useCase := usecase.NewUserUseCase(repo, storage, &fakeCache{}, hasher)
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
	useCase := usecase.NewUserUseCase(repo, storage, &fakeCache{}, hasher)
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
		useCase := usecase.NewUserUseCase(repo, storage, &fakeCache{}, hasher)

		_, err := useCase.UploadAvatar(context.Background(), repo.user.ID, constants.RoleGuru, nil, tinyPNG)

		require.NoError(t, err)
		require.Equal(t, []string{old}, storage.deleted)
	})

	t.Run("child avatar", func(t *testing.T) {
		storage := &fakeStorage{}
		waliID, classID := uuid.New(), uuid.New()
		repo := &fakeRepo{waliID: &waliID, studentClass: &classID, previous: "https://example.com/" + old}
		useCase := usecase.NewUserUseCase(repo, storage, &fakeCache{}, hasher)
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
			useCase := usecase.NewUserUseCase(repo, storage, &fakeCache{}, hasher)

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
	useCase := usecase.NewUserUseCase(repo, storage, &fakeCache{}, hasher)

	_, err := useCase.UploadAvatar(context.Background(), repo.user.ID, constants.RoleGuru, nil, tinyPNG)

	require.Error(t, err)
	require.Equal(t, storage.keys, storage.deleted, "only the new file is removed again")
}

// the address that fakeStorage hands out for every upload
const uploadedAvatar = "https://example.com/avatar.png"

func TestUploadAvatarKeepsTheNewFileWhenTheErrorCameAfterTheCommit(t *testing.T) {
	childID := uuid.New()
	classID := uuid.New()

	for name, call := range map[string]func(repo *fakeRepo, useCase usecase.UserUseCaseItf) (dto.AvatarResponse, error){
		"an account": func(repo *fakeRepo, useCase usecase.UserUseCaseItf) (dto.AvatarResponse, error) {
			return useCase.UploadAvatar(context.Background(), repo.user.ID, constants.RoleGuru, nil, tinyPNG)
		},
		"a child": func(repo *fakeRepo, useCase usecase.UserUseCaseItf) (dto.AvatarResponse, error) {
			return useCase.UploadAvatar(context.Background(), repo.user.ID, constants.RoleWali, &childID, tinyPNG)
		},
	} {
		t.Run(name, func(t *testing.T) {
			storage := &fakeStorage{}
			waliID := uuid.New()
			repo := &fakeRepo{
				user:         accountOf(t, constants.RoleGuru, "any-password"),
				waliID:       &waliID,
				studentClass: &classID,
				updateErr:    errors.New("the connection dropped while the commit was sent"),
				readURL:      uploadedAvatar,
			}
			cache := &fakeCache{}
			useCase := usecase.NewUserUseCase(repo, storage, cache, hasher)

			res, err := call(repo, useCase)

			require.NoError(t, err)
			require.Equal(t, uploadedAvatar, res.AvatarURL)
			require.Empty(t, storage.deleted, "the address is live, so its file must stay")

			if name == "a child" {
				require.Equal(t, []uuid.UUID{waliID, childID}, repo.readFor, "the read asks about this child of this parent")
				require.Equal(t, []string{constants.LeaderboardKeyPrefix + classID.String()}, cache.deleted, "the leaderboard shows the new picture")
			} else {
				require.Equal(t, []uuid.UUID{repo.user.ID}, repo.readFor)
			}
		})
	}
}

func TestUploadAvatarReadsAgainEvenWhenTheClientLeft(t *testing.T) {
	storage := &fakeStorage{}
	repo := &fakeRepo{
		user:      accountOf(t, constants.RoleGuru, "any-password"),
		updateErr: context.Canceled,
		readURL:   uploadedAvatar,
	}
	useCase := usecase.NewUserUseCase(repo, storage, &fakeCache{}, hasher)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res, err := useCase.UploadAvatar(ctx, repo.user.ID, constants.RoleGuru, nil, tinyPNG)

	require.NoError(t, err)
	require.Equal(t, uploadedAvatar, res.AvatarURL)
	require.NoError(t, repo.readCtxErr, "the read does not inherit the cancellation of the request")
	require.Empty(t, storage.deleted)
}

func TestUploadAvatarRemovesTheNewFileWhenTheUpdateDidNotHappen(t *testing.T) {
	for name, repo := range map[string]*fakeRepo{
		"another address is live": {readURL: "https://example.com/avatars/x/other.png"},
		"the account is gone":     {readGone: true},
	} {
		t.Run(name, func(t *testing.T) {
			storage := &fakeStorage{}
			repo.user = accountOf(t, constants.RoleGuru, "any-password")
			repo.updateErr = errors.New("boom")
			useCase := usecase.NewUserUseCase(repo, storage, &fakeCache{}, hasher)

			_, err := useCase.UploadAvatar(context.Background(), repo.user.ID, constants.RoleGuru, nil, tinyPNG)

			require.Error(t, err)
			require.Equal(t, storage.keys, storage.deleted)
		})
	}
}

func TestUploadAvatarKeepsTheNewFileWhenItCannotTellWhatHappened(t *testing.T) {
	storage := &fakeStorage{}
	repo := &fakeRepo{
		user:      accountOf(t, constants.RoleGuru, "any-password"),
		updateErr: errors.New("the connection dropped while the commit was sent"),
		readErr:   errors.New("the database is gone"),
	}
	useCase := usecase.NewUserUseCase(repo, storage, &fakeCache{}, hasher)

	_, err := useCase.UploadAvatar(context.Background(), repo.user.ID, constants.RoleGuru, nil, tinyPNG)

	require.Error(t, err)
	require.Empty(t, storage.deleted, "a leaked file is cheaper than a deleted live picture")
}

func TestUploadAvatarForAnAccountDeletedMeanwhileLeavesNoFile(t *testing.T) {
	storage := &fakeStorage{}
	repo := &fakeRepo{user: accountOf(t, constants.RoleGuru, "any-password"), userGone: true}
	useCase := usecase.NewUserUseCase(repo, storage, &fakeCache{}, hasher)

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
		useCase := usecase.NewUserUseCase(repo, storage, &fakeCache{}, hasher)

		require.NoError(t, useCase.DeleteAccount(ctx, repo.user.ID, constants.RoleWali, dto.DeleteAccountRequest{Password: "correct-password"}))

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
		useCase := usecase.NewUserUseCase(repo, storage, &fakeCache{}, hasher)

		require.NoError(t, useCase.DeleteAccount(ctx, repo.user.ID, constants.RoleWali, dto.DeleteAccountRequest{Password: "correct-password"}))
		require.Len(t, storage.deleted, 40)
	})

	t.Run("nothing is deleted when the account was not", func(t *testing.T) {
		repo := &fakeRepo{
			user:  accountOf(t, constants.RoleGuru, "correct-password"),
			files: []string{"https://example.com/avatars/user/a.png"},
			err:   errors.New("boom"),
		}
		storage := &fakeStorage{}
		useCase := usecase.NewUserUseCase(repo, storage, &fakeCache{}, hasher)

		require.Error(t, useCase.DeleteAccount(ctx, repo.user.ID, constants.RoleGuru, dto.DeleteAccountRequest{Password: "correct-password"}))
		require.Empty(t, storage.deleted)
	})

	t.Run("a file that cannot be deleted stays on the queue and the deletion still succeeds", func(t *testing.T) {
		repo := &fakeRepo{
			user: accountOf(t, constants.RoleWali, "correct-password"),
			files: []string{
				"https://example.com/avatars/user/a.png",
				"https://example.com/trash-scans/child/stuck.png",
			},
		}
		storage := &fakeStorage{failing: map[string]bool{"trash-scans/child/stuck.png": true}}
		useCase := usecase.NewUserUseCase(repo, storage, &fakeCache{}, hasher)

		require.NoError(t, useCase.DeleteAccount(ctx, repo.user.ID, constants.RoleWali, dto.DeleteAccountRequest{Password: "correct-password"}))
		require.ElementsMatch(t, []string{"avatars/user/a.png", "trash-scans/child/stuck.png"}, repo.queued)
		require.Equal(t, []string{"avatars/user/a.png"}, repo.unqueued, "only the file that is gone leaves the queue")
	})

	t.Run("every deleted file leaves the queue", func(t *testing.T) {
		repo := &fakeRepo{
			user:  accountOf(t, constants.RoleWali, "correct-password"),
			files: []string{"https://example.com/avatars/user/a.png", "https://elsewhere.test/avatars/user/c.png"},
		}
		useCase := usecase.NewUserUseCase(repo, &fakeStorage{}, &fakeCache{}, hasher)

		require.NoError(t, useCase.DeleteAccount(ctx, repo.user.ID, constants.RoleWali, dto.DeleteAccountRequest{Password: "correct-password"}))
		require.Equal(t, []string{"avatars/user/a.png"}, repo.queued, "an address of another host is not queued")
		require.Equal(t, repo.queued, repo.unqueued)
	})

	t.Run("a queue that cannot be updated does not fail the deletion", func(t *testing.T) {
		repo := &fakeRepo{
			user:       accountOf(t, constants.RoleWali, "correct-password"),
			files:      []string{"https://example.com/avatars/user/a.png"},
			unqueueErr: errors.New("database gone"),
		}
		useCase := usecase.NewUserUseCase(repo, &fakeStorage{}, &fakeCache{}, hasher)

		require.NoError(t, useCase.DeleteAccount(ctx, repo.user.ID, constants.RoleWali, dto.DeleteAccountRequest{Password: "correct-password"}))
	})

	t.Run("a disabled storage deletes nothing and does not fail", func(t *testing.T) {
		repo := &fakeRepo{
			user:  accountOf(t, constants.RoleGuru, "correct-password"),
			files: []string{"https://example.com/avatars/user/a.png"},
		}
		useCase := usecase.NewUserUseCase(repo, s3.Disabled{}, &fakeCache{}, hasher)

		require.NoError(t, useCase.DeleteAccount(ctx, repo.user.ID, constants.RoleGuru, dto.DeleteAccountRequest{Password: "correct-password"}))
	})
}

func TestChangePassword(t *testing.T) {
	ctx := context.Background()

	newUseCase := func(repo *fakeRepo) usecase.UserUseCaseItf {
		return usecase.NewUserUseCase(repo, s3.Disabled{}, &fakeCache{}, hasher)
	}

	t.Run("the new password is stored as a hash", func(t *testing.T) {
		repo := &fakeRepo{user: accountOf(t, constants.RoleWali, "correct-password")}

		err := newUseCase(repo).ChangePassword(ctx, repo.user.ID, constants.RoleWali,
			dto.ChangePasswordRequest{CurrentPassword: "correct-password", NewPassword: "another-password"})

		require.NoError(t, err)
		require.Equal(t, 1, repo.passwordUpdates)
		require.NoError(t, bcrypt.CompareHashAndPassword([]byte(repo.newHash), []byte("another-password")))
	})

	t.Run("a wrong current password changes nothing", func(t *testing.T) {
		repo := &fakeRepo{user: accountOf(t, constants.RoleWali, "correct-password")}

		err := newUseCase(repo).ChangePassword(ctx, repo.user.ID, constants.RoleWali,
			dto.ChangePasswordRequest{CurrentPassword: "wrong-password", NewPassword: "another-password"})

		require.Equal(t, "AUTH_PASSWORD_INCORRECT", apperror.As(err).Code)
		require.Zero(t, repo.passwordUpdates)
	})

	t.Run("a current password longer than bcrypt reads cannot match a shorter one", func(t *testing.T) {
		registered := strings.Repeat("a", 70) + "é"
		repo := &fakeRepo{user: accountOf(t, constants.RoleWali, registered)}

		err := newUseCase(repo).ChangePassword(ctx, repo.user.ID, constants.RoleWali,
			dto.ChangePasswordRequest{CurrentPassword: registered + "z", NewPassword: "another-password"})

		require.Equal(t, "AUTH_PASSWORD_INCORRECT", apperror.As(err).Code)
		require.Zero(t, repo.passwordUpdates)
	})

	t.Run("a new password that bcrypt would cut is refused", func(t *testing.T) {
		repo := &fakeRepo{user: accountOf(t, constants.RoleWali, "correct-password")}

		err := newUseCase(repo).ChangePassword(ctx, repo.user.ID, constants.RoleWali,
			dto.ChangePasswordRequest{CurrentPassword: "correct-password", NewPassword: strings.Repeat("a", 71) + "é"})

		appErr := apperror.As(err)
		require.Equal(t, "VALIDATION_ERROR", appErr.Code)
		require.Contains(t, appErr.Details, "newPassword")
		require.Zero(t, repo.passwordUpdates)
	})

	t.Run("the same password is refused", func(t *testing.T) {
		repo := &fakeRepo{user: accountOf(t, constants.RoleWali, "correct-password")}

		err := newUseCase(repo).ChangePassword(ctx, repo.user.ID, constants.RoleWali,
			dto.ChangePasswordRequest{CurrentPassword: "correct-password", NewPassword: "correct-password"})

		require.Equal(t, "VALIDATION_ERROR", apperror.As(err).Code)
		require.Zero(t, repo.passwordUpdates)
	})

	t.Run("a role that does not match the account is refused", func(t *testing.T) {
		repo := &fakeRepo{user: accountOf(t, constants.RoleWali, "correct-password")}

		err := newUseCase(repo).ChangePassword(ctx, repo.user.ID, constants.RoleGuru,
			dto.ChangePasswordRequest{CurrentPassword: "correct-password", NewPassword: "another-password"})

		require.Equal(t, "AUTH_FORBIDDEN", apperror.As(err).Code)
		require.Zero(t, repo.passwordUpdates)
	})

	t.Run("an account that is already gone is reported", func(t *testing.T) {
		err := newUseCase(&fakeRepo{}).ChangePassword(ctx, uuid.New(), constants.RoleWali,
			dto.ChangePasswordRequest{CurrentPassword: "anything", NewPassword: "another-password"})

		require.Equal(t, "PROFILE_NOT_FOUND", apperror.As(err).Code)
	})

	t.Run("a password that was changed meanwhile makes the old one wrong", func(t *testing.T) {
		repo := &fakeRepo{user: accountOf(t, constants.RoleWali, "correct-password"), passwordStale: true}

		err := newUseCase(repo).ChangePassword(ctx, repo.user.ID, constants.RoleWali,
			dto.ChangePasswordRequest{CurrentPassword: "correct-password", NewPassword: "another-password"})

		require.Equal(t, "AUTH_PASSWORD_INCORRECT", apperror.As(err).Code)
	})

	t.Run("a failing store is an internal error", func(t *testing.T) {
		repo := &fakeRepo{user: accountOf(t, constants.RoleWali, "correct-password"), err: errors.New("down")}

		err := newUseCase(repo).ChangePassword(ctx, repo.user.ID, constants.RoleWali,
			dto.ChangePasswordRequest{CurrentPassword: "correct-password", NewPassword: "another-password"})

		require.Equal(t, http.StatusInternalServerError, apperror.As(err).Status)
	})
}
