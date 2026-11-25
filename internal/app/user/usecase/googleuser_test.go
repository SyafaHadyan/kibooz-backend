package usecase_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/user/usecase"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/s3"
)

func TestPasswordlessGoogleAccountReceivesAuthPasswordNotSet(t *testing.T) {
	googleUser := &entity.User{ID: uuid.New(), Role: constants.RoleWali, PasswordHash: ""}
	repo := &fakeRepo{user: googleUser}
	uc := usecase.NewUserUseCase(repo, s3.Disabled{}, &fakeCache{}, hasher)
	ctx := context.Background()

	candidateCurrent := "cur-" + uuid.NewString()
	candidateNext := "nxt-" + uuid.NewString()

	err := uc.ChangePassword(
		ctx, googleUser.ID, constants.RoleWali,
		dto.ChangePasswordRequest{CurrentPassword: candidateCurrent, NewPassword: candidateNext},
	)
	require.Equal(t, "AUTH_PASSWORD_NOT_SET", apperror.As(err).Code)

	delErr := uc.DeleteAccount(
		ctx, googleUser.ID, constants.RoleWali,
		dto.DeleteAccountRequest{Password: candidateCurrent},
	)
	require.Equal(t, "AUTH_PASSWORD_NOT_SET", apperror.As(delErr).Code)
}
