package validation_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/validation"
)

func TestACommonPasswordIsRecognizedWhateverItsCase(t *testing.T) {
	for _, password := range []string{"password123", "Password123", "QWERTYUIOP", "iloveyou1", "Kibooz123"} {
		require.True(t, validation.IsCommonPassword(password), password)
	}

	for _, password := range []string{"correct horse battery", "Tr0ub4dor&3x", "kibooz-9fK2q"} {
		require.False(t, validation.IsCommonPassword(password), password)
	}
}

func TestAChangedPasswordFromTheListIsRefusedWithItsField(t *testing.T) {
	err := validation.Struct(&dto.ChangePasswordRequest{CurrentPassword: "x", NewPassword: "password123"})

	var appErr *apperror.Error

	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "is too common, choose another password", appErr.Details["newPassword"])

	require.NoError(t, validation.Struct(&dto.ChangePasswordRequest{CurrentPassword: "x", NewPassword: "a long phrase nobody guesses"}))
}

func TestARegisteredPasswordFromTheListIsRefusedWithItsField(t *testing.T) {
	request := &dto.RegisterRequest{
		Email: "a@example.com", Password: "Password123", FullName: "Ana", Role: "GURU", Class: &dto.RegisterClass{Name: "A"},
	}

	var appErr *apperror.Error

	require.ErrorAs(t, validation.Struct(request), &appErr)
	require.Equal(t, "is too common, choose another password", appErr.Details["password"])

	request.Password = "a long phrase nobody guesses"
	require.NoError(t, validation.Struct(request))
}
