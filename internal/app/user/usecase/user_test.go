package usecase_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/user/usecase"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/s3"
)

func TestUploadAvatarReportsDisabledStorage(t *testing.T) {
	useCase := usecase.NewUserUseCase(nil, s3.Disabled{}, nil)

	_, err := useCase.UploadAvatar(context.Background(), uuid.New(), constants.RoleGuru, nil, []byte("anything"))

	appErr := apperror.As(err)
	require.Equal(t, http.StatusServiceUnavailable, appErr.Status)
	require.Equal(t, "STORAGE_UNAVAILABLE", appErr.Code)
}
