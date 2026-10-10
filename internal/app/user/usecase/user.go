// Package usecase holds the profile business rules
package usecase

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/user/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/imageutil"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/redis"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/s3"
)

const (
	bcryptMaxBytes = 72

	// a parent can have many trash photos, so the files of a deleted account are removed a few at a time
	discardConcurrency = 8
)

type UserUseCaseItf interface {
	// UploadAvatar stores the image for the caller, or for one of their children when studentID is set
	UploadAvatar(ctx context.Context, userID uuid.UUID, role constants.Role, studentID *uuid.UUID, data []byte) (dto.AvatarResponse, error)
	// DeleteAccount soft deletes the caller after checking their password. A parent's children are deleted with them.
	DeleteAccount(ctx context.Context, userID uuid.UUID, role constants.Role, password string) error
}

type UserUseCase struct {
	repo    repository.UserDBItf
	storage s3.StorageItf
	cache   redis.CacheItf
}

func NewUserUseCase(repo repository.UserDBItf, storage s3.StorageItf, cache redis.CacheItf) UserUseCaseItf {
	return &UserUseCase{repo: repo, storage: storage, cache: cache}
}

func (u *UserUseCase) UploadAvatar(
	ctx context.Context, userID uuid.UUID, role constants.Role, studentID *uuid.UUID, data []byte,
) (dto.AvatarResponse, error) {
	if !u.storage.Enabled() {
		return dto.AvatarResponse{}, apperror.ErrStorageDisabled
	}

	image, err := imageutil.Inspect(data, constants.AvatarMaxBytes)
	if err != nil {
		return dto.AvatarResponse{}, err
	}

	if studentID != nil && role != constants.RoleWali {
		return dto.AvatarResponse{}, apperror.ErrForbidden
	}

	var waliID uuid.UUID

	if studentID != nil {
		found, err := u.repo.FindWaliIDByUserID(ctx, userID)
		if err != nil {
			return dto.AvatarResponse{}, apperror.Internal(err)
		}

		if found == nil {
			return dto.AvatarResponse{}, apperror.ErrProfileNotFound
		}

		waliID = *found
	}

	if studentID == nil {
		user, err := u.repo.FindUserByID(ctx, userID)
		if err != nil {
			return dto.AvatarResponse{}, apperror.Internal(err)
		}

		if user == nil {
			return dto.AvatarResponse{}, apperror.ErrProfileNotFound
		}
	}

	owner := userID
	if studentID != nil {
		owner = *studentID
	}

	key := fmt.Sprintf("%s/%s/%s%s", constants.AvatarDirectory, owner, uuid.NewString(), image.Extension)

	url, err := u.storage.Upload(ctx, key, image.ContentType, image.Data)
	if err != nil {
		return dto.AvatarResponse{}, err
	}

	if studentID == nil {
		previous, found, err := u.repo.UpdateUserAvatar(ctx, userID, url)
		if err != nil {
			s3.Discard(ctx, u.storage, key)

			return dto.AvatarResponse{}, apperror.Internal(err)
		}

		if !found {
			// the account was deleted after the check above, so the file belongs to nobody
			s3.Discard(ctx, u.storage, key)

			return dto.AvatarResponse{}, apperror.ErrProfileNotFound
		}

		u.discardReplaced(ctx, previous)

		return dto.AvatarResponse{AvatarURL: url}, nil
	}

	classID, previous, err := u.repo.UpdateStudentAvatar(ctx, waliID, *studentID, url)
	if err != nil {
		s3.Discard(ctx, u.storage, key)

		return dto.AvatarResponse{}, apperror.Internal(err)
	}

	if classID == nil {
		// the child is not this parent's, so the file belongs to nobody
		s3.Discard(ctx, u.storage, key)

		return dto.AvatarResponse{}, apperror.ErrStudentNotFound
	}

	u.discardReplaced(ctx, previous)

	// the cache is optional, entries also expire by themselves
	_ = u.cache.Del(ctx, constants.LeaderboardKeyPrefix+classID.String())

	return dto.AvatarResponse{AvatarURL: url}, nil
}

// discardReplaced removes the file an avatar update replaced. It only touches files this storage serves under the
// avatar directory, so an address that was set some other way is never deleted.
func (u *UserUseCase) discardReplaced(ctx context.Context, previous string) {
	if previous == "" {
		return
	}

	key, ok := u.storage.KeyFromURL(previous)
	if !ok || !inDirectory(key, constants.AvatarDirectory) {
		return
	}

	s3.Discard(ctx, u.storage, key)
}

func (u *UserUseCase) DeleteAccount(ctx context.Context, userID uuid.UUID, role constants.Role, password string) error {
	user, err := u.repo.FindUserByID(ctx, userID)
	if err != nil {
		return apperror.Internal(err)
	}

	if user == nil {
		return apperror.ErrProfileNotFound
	}

	if user.Role != role {
		return apperror.ErrForbidden
	}

	// bcrypt only reads the first 72 bytes, so a longer password could match a shorter one that shares them
	if len([]byte(password)) > bcryptMaxBytes {
		return apperror.ErrPasswordIncorrect
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err != nil {
		return apperror.ErrPasswordIncorrect
	}

	deleted, err := u.repo.SoftDeleteAccount(ctx, userID, role)
	if err != nil {
		return apperror.Internal(err)
	}

	// the cache is optional, entries also expire by themselves
	for _, classID := range deleted.ClassIDs {
		_ = u.cache.Del(ctx, constants.LeaderboardKeyPrefix+classID.String())
	}

	u.discardFiles(ctx, deleted.FileURLs)

	return nil
}

// discardFiles removes the avatars and the trash photos of a deleted account. The rows no longer point at them, so a
// file that cannot be deleted now is only logged and left in the bucket.
func (u *UserUseCase) discardFiles(ctx context.Context, urls []string) {
	var keys []string

	for _, url := range urls {
		key, ok := u.storage.KeyFromURL(url)
		if ok && inDirectory(key, constants.AvatarDirectory, constants.TrashDirectory) {
			keys = append(keys, key)
		}
	}

	var group sync.WaitGroup

	limit := make(chan struct{}, discardConcurrency)

	for _, key := range keys {
		group.Add(1)

		limit <- struct{}{}

		go func() {
			defer group.Done()
			defer func() { <-limit }()

			s3.Discard(ctx, u.storage, key)
		}()
	}

	group.Wait()
}

// inDirectory tells whether key lies under one of the directories this service writes to
func inDirectory(key string, directories ...constants.StorageDirectory) bool {
	for _, directory := range directories {
		if strings.HasPrefix(key, string(directory)+"/") {
			return true
		}
	}

	return false
}
