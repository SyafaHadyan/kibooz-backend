// Package usecase holds the profile business rules
package usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/user/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/imageutil"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/passwords"
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
	DeleteAccount(ctx context.Context, userID uuid.UUID, role constants.Role, req dto.DeleteAccountRequest) error
	// ChangePassword swaps the password of the caller after checking the current one and ends every session, so each
	// device has to sign in again with the new password.
	ChangePassword(ctx context.Context, userID uuid.UUID, role constants.Role, req dto.ChangePasswordRequest) error
}

type UserUseCase struct {
	repo    repository.UserDBItf
	storage s3.StorageItf
	cache   redis.CacheItf
	// hasher is shared with the other use cases, because its slots are the budget of the whole process
	hasher *passwords.Hasher
}

func NewUserUseCase(repo repository.UserDBItf, storage s3.StorageItf, cache redis.CacheItf, hasher *passwords.Hasher) UserUseCaseItf {
	return &UserUseCase{repo: repo, storage: storage, cache: cache, hasher: hasher}
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

func (u *UserUseCase) DeleteAccount(
	ctx context.Context, userID uuid.UUID, role constants.Role, req dto.DeleteAccountRequest,
) error {
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
	if len([]byte(req.Password)) > bcryptMaxBytes {
		return apperror.ErrPasswordIncorrect
	}

	matches, err := u.hasher.Matches(ctx, user.PasswordHash, req.Password)
	if err != nil {
		// the server is busy hashing, or the request ended
		return apperror.As(err)
	}

	if !matches {
		return apperror.ErrPasswordIncorrect
	}

	// a refusal such as SUCCESSOR_REQUIRED is an answer for the caller, anything else is a failure
	deleted, err := u.repo.SoftDeleteAccount(ctx, userID, role, user.PasswordHash, strings.ToLower(strings.TrimSpace(req.SuccessorEmail)), u.fileKey)
	if err != nil {
		var refusal *apperror.Error
		if errors.As(err, &refusal) {
			return refusal
		}

		return apperror.Internal(err)
	}

	// the cache is optional, entries also expire by themselves
	for _, classID := range deleted.ClassIDs {
		_ = u.cache.Del(ctx, constants.LeaderboardKeyPrefix+classID.String())
	}

	u.discardFiles(ctx, deleted.FileKeys)

	return nil
}

func (u *UserUseCase) ChangePassword(
	ctx context.Context, userID uuid.UUID, role constants.Role, req dto.ChangePasswordRequest,
) error {
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
	if len([]byte(req.CurrentPassword)) > bcryptMaxBytes {
		return apperror.ErrPasswordIncorrect
	}

	matches, err := u.hasher.Matches(ctx, user.PasswordHash, req.CurrentPassword)
	if err != nil {
		return apperror.As(err)
	}

	if !matches {
		return apperror.ErrPasswordIncorrect
	}

	if len([]byte(req.NewPassword)) > bcryptMaxBytes {
		//nolint:gosec // this is a validation message and not a credential
		return apperror.Validation(map[string]string{"newPassword": "too long, maximum 72 bytes"})
	}

	if req.NewPassword == req.CurrentPassword {
		return apperror.Validation(map[string]string{"newPassword": "must differ from the current password"})
	}

	hash, err := u.hasher.Hash(ctx, req.NewPassword)
	if err != nil {
		return apperror.As(err)
	}

	changed, err := u.repo.UpdatePassword(ctx, userID, user.PasswordHash, hash)
	if err != nil {
		return apperror.Internal(err)
	}

	// the password was changed from another device while this request hashed, so the one that was sent is out of date
	if !changed {
		return apperror.ErrPasswordIncorrect
	}

	return nil
}

// fileKey picks the avatars and the trash photos of this storage out of the addresses a deleted account held
func (u *UserUseCase) fileKey(url string) (string, bool) {
	key, ok := u.storage.KeyFromURL(url)

	return key, ok && inDirectory(key, constants.AvatarDirectory, constants.TrashDirectory)
}

// cleanupBudget is the longest the response of a deletion waits for the bucket. The files it did not reach stay on the
// queue for the sweep.
const cleanupBudget = 10 * time.Second

// queueTimeout bounds the write that takes deleted files off the queue
const queueTimeout = 5 * time.Second

// discardFiles removes the avatars and the trash photos of a deleted account. Their keys are already on the cleanup
// queue, so a file that cannot be deleted now, or that the time budget did not leave room for, stays there and a later
// sweep removes it. The ones that are gone leave the queue.
func (u *UserUseCase) discardFiles(ctx context.Context, keys []string) {
	var (
		group sync.WaitGroup
		mu    sync.Mutex
		gone  []string
	)

	// the client may give up, the cleanup still has to run, but not for longer than the budget
	budget, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupBudget)
	defer cancel()

	limit := make(chan struct{}, discardConcurrency)

	for _, key := range keys {
		if budget.Err() != nil {
			break
		}

		group.Add(1)

		limit <- struct{}{}

		go func() {
			defer group.Done()
			defer func() { <-limit }()

			err := u.storage.Delete(budget, key)
			if err != nil {
				log.Printf("storage cleanup of %s failed, it will be tried again %v", key, err)

				return
			}

			mu.Lock()
			gone = append(gone, key)
			mu.Unlock()
		}()
	}

	group.Wait()

	// the account is already deleted, so a queue that cannot be updated only means a later sweep deletes the file again
	queueCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), queueTimeout)
	defer stop()

	err := u.repo.UnqueueFileCleanups(queueCtx, gone)
	if err != nil {
		log.Printf("take the deleted storage files off the cleanup queue %v", err)
	}
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
