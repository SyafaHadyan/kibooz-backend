package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/wali/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
)

func text(value string) *string { return &value }

func TestUpdateProfileStoresTheChangedFieldsAndReturnsTheNewDetails(t *testing.T) {
	repo := parentRepo()
	repo.guardian = &repository.GuardianRow{FullName: "Sarah Baru", Email: "sarah@example.com", PhoneNumber: text("+62 812-1111-2222")}

	res, err := newUseCase(repo, &fakeCache{}).UpdateProfile(context.Background(), uuid.New(), dto.UpdateWaliProfileRequest{
		FullName: text("  Sarah Baru  "), PhoneNumber: text(" +62 812-1111-2222 "), WhatsappNumber: text(""), Address: text("  Jl. Mawar 1  "),
	})

	require.NoError(t, err)
	require.Equal(t, "Sarah Baru", res.FullName)
	require.Equal(t, "sarah@example.com", res.Email)

	require.NotNil(t, repo.updated)
	require.True(t, repo.updated.SetName)
	require.Equal(t, "Sarah Baru", repo.updated.FullName)
	require.True(t, repo.updated.SetPhone)
	require.Equal(t, "+62 812-1111-2222", *repo.updated.Phone)
	require.True(t, repo.updated.SetWhatsapp)
	require.Nil(t, repo.updated.Whatsapp, "an empty text clears the number")
	require.True(t, repo.updated.SetAddress)
	require.Equal(t, "Jl. Mawar 1", *repo.updated.Address)
}

func TestUpdateProfileLeavesAMissingFieldAlone(t *testing.T) {
	repo := parentRepo()
	repo.guardian = &repository.GuardianRow{FullName: "Sarah"}

	_, err := newUseCase(repo, &fakeCache{}).UpdateProfile(context.Background(), uuid.New(), dto.UpdateWaliProfileRequest{Address: text("")})

	require.NoError(t, err)
	require.False(t, repo.updated.SetName)
	require.False(t, repo.updated.SetPhone)
	require.False(t, repo.updated.SetWhatsapp)
	require.True(t, repo.updated.SetAddress)
	require.Nil(t, repo.updated.Address)
}

func TestUpdateProfileRefusesBadInput(t *testing.T) {
	cases := map[string]dto.UpdateWaliProfileRequest{
		"nothing to change":    {},
		"an empty name":        {FullName: text("   ")},
		"a bad phone number":   {PhoneNumber: text("call me")},
		"a bad WhatsApp":       {WhatsappNumber: text("12")},
		"one good and one bad": {FullName: text("Sarah"), PhoneNumber: text("abc")},
	}

	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			repo := parentRepo()

			_, err := newUseCase(repo, &fakeCache{}).UpdateProfile(context.Background(), uuid.New(), req)

			requireAppError(t, err, apperror.Validation(nil))
			require.Nil(t, repo.updated, "nothing is stored")
		})
	}
}

func TestUpdateProfileNamesEveryInvalidField(t *testing.T) {
	_, err := newUseCase(parentRepo(), &fakeCache{}).UpdateProfile(context.Background(), uuid.New(), dto.UpdateWaliProfileRequest{
		FullName: text(""), PhoneNumber: text("x"), WhatsappNumber: text("y"),
	})

	var got *apperror.Error

	require.ErrorAs(t, err, &got)
	require.Contains(t, got.Details, "fullName")
	require.Contains(t, got.Details, "phoneNumber")
	require.Contains(t, got.Details, "whatsappNumber")
}

func TestUpdateProfileRefusesAnAccountWithoutAParentProfile(t *testing.T) {
	repo := &fakeRepo{}

	_, err := newUseCase(repo, &fakeCache{}).UpdateProfile(context.Background(), uuid.New(), dto.UpdateWaliProfileRequest{FullName: text("Sarah")})

	requireAppError(t, err, apperror.ErrProfileNotFound)

	repo = parentRepo()

	_, err = newUseCase(repo, &fakeCache{}).UpdateProfile(context.Background(), uuid.New(), dto.UpdateWaliProfileRequest{FullName: text("Sarah")})

	requireAppError(t, err, apperror.ErrProfileNotFound)
}

func TestUpdateProfileReportsAFailedStore(t *testing.T) {
	repo := parentRepo()
	repo.updateErr = errors.New("connection lost")

	_, err := newUseCase(repo, &fakeCache{}).UpdateProfile(context.Background(), uuid.New(), dto.UpdateWaliProfileRequest{FullName: text("Sarah")})

	var got *apperror.Error

	require.ErrorAs(t, err, &got)
	require.Equal(t, 500, got.Status)
}
