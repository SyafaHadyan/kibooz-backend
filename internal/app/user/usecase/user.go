// Package usecase holds the profile business rules
package usecase

import (
	"context"
	"fmt"

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

const bcryptMaxBytes = 72

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
		err = u.repo.UpdateUserAvatar(ctx, userID, url)
		if err != nil {
			return dto.AvatarResponse{}, apperror.Internal(err)
		}

		return dto.AvatarResponse{AvatarURL: url}, nil
	}

	classID, err := u.repo.UpdateStudentAvatar(ctx, waliID, *studentID, url)
	if err != nil {
		return dto.AvatarResponse{}, apperror.Internal(err)
	}

	if classID == nil {
		return dto.AvatarResponse{}, apperror.ErrStudentNotFound
	}

	// the cache is optional, entries also expire by themselves
	_ = u.cache.Del(ctx, constants.LeaderboardKeyPrefix+classID.String())

	return dto.AvatarResponse{AvatarURL: url}, nil
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

	classIDs, err := u.repo.SoftDeleteAccount(ctx, userID, role)
	if err != nil {
		return apperror.Internal(err)
	}

	// the cache is optional, entries also expire by themselves
	for _, classID := range classIDs {
		_ = u.cache.Del(ctx, constants.LeaderboardKeyPrefix+classID.String())
	}

	return nil
}
