package usecase

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
)

func text(value string) *string {
	return &value
}

func TestProfileUpdateNeedsAtLeastOneField(t *testing.T) {
	_, err := profileUpdate(dto.UpdateGuruProfileRequest{})

	var appErr *apperror.Error

	require.True(t, errors.As(err, &appErr))
	require.Equal(t, "VALIDATION_ERROR", appErr.Code)
	require.Contains(t, appErr.Details, "body")
}

func TestProfileUpdateLeavesAMissingFieldAlone(t *testing.T) {
	update, err := profileUpdate(dto.UpdateGuruProfileRequest{Address: text("  Jl. Merdeka 1  ")})

	require.NoError(t, err)
	require.False(t, update.SetPhone)
	require.True(t, update.SetAddress)
	require.Equal(t, "Jl. Merdeka 1", *update.Address)

	update, err = profileUpdate(dto.UpdateGuruProfileRequest{PhoneNumber: text(" +62 812-3456-7890 ")})

	require.NoError(t, err)
	require.True(t, update.SetPhone)
	require.False(t, update.SetAddress)
	require.Equal(t, "+62 812-3456-7890", *update.Phone)
}

func TestProfileUpdateClearsAFieldWithAnEmptyText(t *testing.T) {
	update, err := profileUpdate(dto.UpdateGuruProfileRequest{PhoneNumber: text(""), Address: text("   ")})

	require.NoError(t, err)
	require.True(t, update.SetPhone)
	require.Nil(t, update.Phone)
	require.True(t, update.SetAddress)
	require.Nil(t, update.Address)
}

func TestProfileUpdateRejectsAnInvalidPhoneNumber(t *testing.T) {
	for _, phone := range []string{"abc", "12345", "+", "0812 abc 456", "081234567890123456789012345678901", "(+62) 812", "--------"} {
		_, err := profileUpdate(dto.UpdateGuruProfileRequest{PhoneNumber: text(phone)})

		var appErr *apperror.Error

		require.True(t, errors.As(err, &appErr), phone)
		require.Contains(t, appErr.Details, "phoneNumber", phone)
	}
}

func TestPhonePatternAcceptsCommonWrittenForms(t *testing.T) {
	for _, phone := range []string{"081234567890", "+6281234567890", "+62 812-3456-7890", "(021) 555-0123", "0341 551 611"} {
		require.True(t, phonePattern.MatchString(phone), phone)
	}
}

func TestFirstNonEmptyTrimsAndSkipsBlankValues(t *testing.T) {
	require.Equal(t, "Class B", firstNonEmpty("", "  ", " Class B ", "Class A"))
	require.Empty(t, firstNonEmpty("", "   "))
	require.Empty(t, firstNonEmpty())
}

func TestOptionalTextDropsBlankText(t *testing.T) {
	require.Nil(t, optionalText(""))
	require.Nil(t, optionalText(" \n"))
	require.Equal(t, "x", *optionalText(" x "))
}
