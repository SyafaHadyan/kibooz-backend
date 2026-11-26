package usecase_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/kiboozapp/kibooz-backend/internal/app/user/usecase"
	"github.com/kiboozapp/kibooz-backend/internal/apperror"
	"github.com/kiboozapp/kibooz-backend/internal/constants"
	"github.com/kiboozapp/kibooz-backend/internal/domain/dto"
	"github.com/kiboozapp/kibooz-backend/internal/domain/entity"
	"github.com/kiboozapp/kibooz-backend/internal/googleauth"
	"github.com/kiboozapp/kibooz-backend/internal/infra/s3"
)

type stubVerifier struct {
	claims googleauth.Claims
	err    error
}

func (s stubVerifier) Verify(context.Context, string) (googleauth.Claims, error) {
	return s.claims, s.err
}

func googleOnlyAccount(sub string) *entity.User {
	return &entity.User{ID: uuid.New(), Role: constants.RoleWali, PasswordHash: "", GoogleSub: &sub}
}

func freshClaims(sub string) googleauth.Claims {
	return googleauth.Claims{Subject: sub, Email: "a@example.org", EmailVerified: true, IssuedAt: time.Now().Add(-time.Minute)}
}

func withGoogle(repo *fakeRepo, claims googleauth.Claims, err error) usecase.UserUseCaseItf {
	return usecase.NewUserUseCase(repo, s3.Disabled{}, &fakeCache{}, hasher, usecase.WithGoogleVerifier(stubVerifier{claims: claims, err: err}))
}

func credential() string { return "assertion-" + uuid.NewString() }

func TestGoogleOnlyAccountCannotUsePasswordRoutes(t *testing.T) {
	user := googleOnlyAccount("sub-1")
	uc := withGoogle(&fakeRepo{user: user}, freshClaims("sub-1"), nil)
	ctx := context.Background()

	err := uc.ChangePassword(ctx, user.ID, user.Role, dto.ChangePasswordRequest{CurrentPassword: credential(), NewPassword: credential()})
	require.Equal(t, "AUTH_PASSWORD_NOT_SET", apperror.As(err).Code)

	err = uc.UnlinkGoogle(ctx, user.ID, user.Role, dto.UnlinkGoogleRequest{Password: credential()})
	require.Equal(t, "AUTH_PASSWORD_NOT_SET", apperror.As(err).Code)
}

func TestGoogleOnlyAccountDeletesWithFreshToken(t *testing.T) {
	ctx := context.Background()

	t.Run("a fresh token of the linked account is enough", func(t *testing.T) {
		user := googleOnlyAccount("sub-1")
		repo := &fakeRepo{user: user}

		err := withGoogle(repo, freshClaims("sub-1"), nil).DeleteAccount(ctx, user.ID, user.Role, dto.DeleteAccountRequest{IDToken: credential()})
		require.NoError(t, err)
		require.Equal(t, 1, repo.deleted)
	})

	t.Run("no token asks for one", func(t *testing.T) {
		user := googleOnlyAccount("sub-1")
		repo := &fakeRepo{user: user}

		err := withGoogle(repo, freshClaims("sub-1"), nil).DeleteAccount(ctx, user.ID, user.Role, dto.DeleteAccountRequest{})
		require.Equal(t, http.StatusBadRequest, apperror.As(err).Status)
		require.Zero(t, repo.deleted)
	})

	t.Run("a token of another Google account is refused", func(t *testing.T) {
		user := googleOnlyAccount("sub-1")
		repo := &fakeRepo{user: user}

		err := withGoogle(repo, freshClaims("sub-2"), nil).DeleteAccount(ctx, user.ID, user.Role, dto.DeleteAccountRequest{IDToken: credential()})
		require.Equal(t, "AUTH_GOOGLE_INVALID", apperror.As(err).Code)
		require.Zero(t, repo.deleted)
	})

	t.Run("a token older than five minutes is refused", func(t *testing.T) {
		user := googleOnlyAccount("sub-1")
		repo := &fakeRepo{user: user}
		old := freshClaims("sub-1")
		old.IssuedAt = time.Now().Add(-6 * time.Minute)

		err := withGoogle(repo, old, nil).DeleteAccount(ctx, user.ID, user.Role, dto.DeleteAccountRequest{IDToken: credential()})
		require.Equal(t, "AUTH_GOOGLE_STALE", apperror.As(err).Code)
		require.Zero(t, repo.deleted)
	})

	t.Run("a token without an issue time is never fresh", func(t *testing.T) {
		user := googleOnlyAccount("sub-1")
		repo := &fakeRepo{user: user}
		undated := freshClaims("sub-1")
		undated.IssuedAt = time.Time{}

		err := withGoogle(repo, undated, nil).DeleteAccount(ctx, user.ID, user.Role, dto.DeleteAccountRequest{IDToken: credential()})
		require.Equal(t, "AUTH_GOOGLE_STALE", apperror.As(err).Code)
	})

	t.Run("a token that does not verify is refused", func(t *testing.T) {
		user := googleOnlyAccount("sub-1")
		repo := &fakeRepo{user: user}

		err := withGoogle(repo, googleauth.Claims{}, googleauth.ErrInvalidToken).
			DeleteAccount(ctx, user.ID, user.Role, dto.DeleteAccountRequest{IDToken: credential()})
		require.Equal(t, "AUTH_GOOGLE_INVALID", apperror.As(err).Code)
	})

	t.Run("an account with a password ignores a token and needs the password", func(t *testing.T) {
		password := credential()
		user := accountOf(t, constants.RoleWali, password)
		sub := "sub-1"
		user.GoogleSub = &sub
		repo := &fakeRepo{user: user}
		uc := withGoogle(repo, freshClaims("sub-1"), nil)

		err := uc.DeleteAccount(ctx, user.ID, user.Role, dto.DeleteAccountRequest{IDToken: credential()})
		require.Equal(t, http.StatusBadRequest, apperror.As(err).Status)
		require.Zero(t, repo.deleted)

		err = uc.DeleteAccount(ctx, user.ID, user.Role, dto.DeleteAccountRequest{Password: password})
		require.NoError(t, err)
		require.Equal(t, 1, repo.deleted)
	})
}

func TestGoogleOnlyAccountSetsFirstPassword(t *testing.T) {
	ctx := context.Background()

	t.Run("a fresh token sets the password", func(t *testing.T) {
		user := googleOnlyAccount("sub-1")
		repo := &fakeRepo{user: user}

		err := withGoogle(repo, freshClaims("sub-1"), nil).SetPassword(ctx, user.ID, user.Role, dto.SetPasswordRequest{IDToken: credential(), NewPassword: credential()})
		require.NoError(t, err)
		require.Equal(t, 1, repo.passwordUpdates)
		require.Contains(t, repo.newHash, "$2")
	})

	t.Run("a stale token or another Google account is refused", func(t *testing.T) {
		user := googleOnlyAccount("sub-1")
		repo := &fakeRepo{user: user}
		old := freshClaims("sub-1")
		old.IssuedAt = time.Now().Add(-10 * time.Minute)

		err := withGoogle(repo, old, nil).SetPassword(ctx, user.ID, user.Role, dto.SetPasswordRequest{IDToken: credential(), NewPassword: credential()})
		require.Equal(t, "AUTH_GOOGLE_STALE", apperror.As(err).Code)

		err = withGoogle(repo, freshClaims("sub-9"), nil).SetPassword(ctx, user.ID, user.Role, dto.SetPasswordRequest{IDToken: credential(), NewPassword: credential()})
		require.Equal(t, "AUTH_GOOGLE_INVALID", apperror.As(err).Code)
		require.Zero(t, repo.passwordUpdates)
	})

	t.Run("an account that has a password is told to change it instead", func(t *testing.T) {
		user := accountOf(t, constants.RoleWali, credential())
		repo := &fakeRepo{user: user}

		err := withGoogle(repo, freshClaims("sub-1"), nil).SetPassword(ctx, user.ID, user.Role, dto.SetPasswordRequest{IDToken: credential(), NewPassword: credential()})
		require.Equal(t, "AUTH_PASSWORD_ALREADY_SET", apperror.As(err).Code)
	})

	t.Run("a password set in between is not overwritten", func(t *testing.T) {
		user := googleOnlyAccount("sub-1")
		repo := &fakeRepo{user: user, passwordStale: true}

		err := withGoogle(repo, freshClaims("sub-1"), nil).SetPassword(ctx, user.ID, user.Role, dto.SetPasswordRequest{IDToken: credential(), NewPassword: credential()})
		require.Equal(t, "AUTH_PASSWORD_ALREADY_SET", apperror.As(err).Code)
	})
}

func TestLinkAndUnlinkGoogle(t *testing.T) {
	ctx := context.Background()

	t.Run("link needs the password and a fresh token", func(t *testing.T) {
		password := credential()
		user := accountOf(t, constants.RoleWali, password)
		repo := &fakeRepo{user: user}
		uc := withGoogle(repo, freshClaims("sub-7"), nil)

		err := uc.LinkGoogle(ctx, user.ID, user.Role, dto.LinkGoogleRequest{IDToken: credential(), Password: credential()})
		require.Equal(t, "AUTH_PASSWORD_INCORRECT", apperror.As(err).Code)
		require.Empty(t, repo.linked)

		err = uc.LinkGoogle(ctx, user.ID, user.Role, dto.LinkGoogleRequest{IDToken: credential(), Password: password})
		require.NoError(t, err)
		require.Equal(t, "sub-7", repo.linked)
	})

	t.Run("link refuses a stale token and an account that is linked already", func(t *testing.T) {
		password := credential()
		user := accountOf(t, constants.RoleWali, password)
		repo := &fakeRepo{user: user}
		old := freshClaims("sub-7")
		old.IssuedAt = time.Now().Add(-time.Hour)

		err := withGoogle(repo, old, nil).LinkGoogle(ctx, user.ID, user.Role, dto.LinkGoogleRequest{IDToken: credential(), Password: password})
		require.Equal(t, "AUTH_GOOGLE_STALE", apperror.As(err).Code)

		sub := "sub-1"
		user.GoogleSub = &sub

		err = withGoogle(repo, freshClaims("sub-7"), nil).LinkGoogle(ctx, user.ID, user.Role, dto.LinkGoogleRequest{IDToken: credential(), Password: password})
		require.Equal(t, "GOOGLE_ALREADY_LINKED", apperror.As(err).Code)
	})

	t.Run("a Google account of another account is refused", func(t *testing.T) {
		password := credential()
		user := accountOf(t, constants.RoleWali, password)
		repo := &fakeRepo{user: user, linkTaken: true}

		err := withGoogle(repo, freshClaims("sub-7"), nil).LinkGoogle(ctx, user.ID, user.Role, dto.LinkGoogleRequest{IDToken: credential(), Password: password})
		require.Equal(t, "GOOGLE_ACCOUNT_IN_USE", apperror.As(err).Code)
	})

	t.Run("unlink needs the password and ends the sessions through the repository", func(t *testing.T) {
		password := credential()
		user := accountOf(t, constants.RoleWali, password)
		sub := "sub-1"
		user.GoogleSub = &sub
		repo := &fakeRepo{user: user}
		uc := withGoogle(repo, freshClaims("sub-1"), nil)

		err := uc.UnlinkGoogle(ctx, user.ID, user.Role, dto.UnlinkGoogleRequest{Password: credential()})
		require.Equal(t, "AUTH_PASSWORD_INCORRECT", apperror.As(err).Code)
		require.Zero(t, repo.unlinked)

		err = uc.UnlinkGoogle(ctx, user.ID, user.Role, dto.UnlinkGoogleRequest{Password: password})
		require.NoError(t, err)
		require.Equal(t, 1, repo.unlinked)
	})

	t.Run("unlink of an account that is not linked is refused", func(t *testing.T) {
		password := credential()
		user := accountOf(t, constants.RoleWali, password)

		err := withGoogle(&fakeRepo{user: user}, freshClaims("sub-1"), nil).UnlinkGoogle(ctx, user.ID, user.Role, dto.UnlinkGoogleRequest{Password: password})
		require.Equal(t, "GOOGLE_NOT_LINKED", apperror.As(err).Code)
	})

	t.Run("without a verifier the actions answer unavailable", func(t *testing.T) {
		user := googleOnlyAccount("sub-1")
		uc := usecase.NewUserUseCase(&fakeRepo{user: user}, s3.Disabled{}, &fakeCache{}, hasher)

		err := uc.SetPassword(ctx, user.ID, user.Role, dto.SetPasswordRequest{IDToken: credential(), NewPassword: credential()})
		require.Equal(t, "AUTH_GOOGLE_UNAVAILABLE", apperror.As(err).Code)
	})
}

func TestGoogleActionsRefuseTheWrongCaller(t *testing.T) {
	ctx := context.Background()
	user := googleOnlyAccount("sub-1")

	t.Run("an account that is gone", func(t *testing.T) {
		uc := withGoogle(&fakeRepo{}, freshClaims("sub-1"), nil)

		err := uc.UnlinkGoogle(ctx, uuid.New(), constants.RoleWali, dto.UnlinkGoogleRequest{Password: credential()})
		require.Equal(t, "PROFILE_NOT_FOUND", apperror.As(err).Code)

		err = uc.LinkGoogle(ctx, uuid.New(), constants.RoleWali, dto.LinkGoogleRequest{IDToken: credential(), Password: credential()})
		require.Equal(t, "PROFILE_NOT_FOUND", apperror.As(err).Code)

		err = uc.SetPassword(ctx, uuid.New(), constants.RoleWali, dto.SetPasswordRequest{IDToken: credential(), NewPassword: credential()})
		require.Equal(t, "PROFILE_NOT_FOUND", apperror.As(err).Code)
	})

	t.Run("another role", func(t *testing.T) {
		uc := withGoogle(&fakeRepo{user: user}, freshClaims("sub-1"), nil)

		err := uc.SetPassword(ctx, user.ID, constants.RoleGuru, dto.SetPasswordRequest{IDToken: credential(), NewPassword: credential()})
		require.Equal(t, "AUTH_FORBIDDEN", apperror.As(err).Code)
	})

	t.Run("a failed lookup", func(t *testing.T) {
		uc := withGoogle(&fakeRepo{user: user, lookupErr: errDown}, freshClaims("sub-1"), nil)

		err := uc.SetPassword(ctx, user.ID, user.Role, dto.SetPasswordRequest{IDToken: credential(), NewPassword: credential()})
		require.Equal(t, "INTERNAL_ERROR", apperror.As(err).Code)
	})
}

func TestSetPasswordChecksTheNewPassword(t *testing.T) {
	ctx := context.Background()

	t.Run("too long for bcrypt", func(t *testing.T) {
		user := googleOnlyAccount("sub-1")
		uc := withGoogle(&fakeRepo{user: user}, freshClaims("sub-1"), nil)

		err := uc.SetPassword(ctx, user.ID, user.Role, dto.SetPasswordRequest{IDToken: credential(), NewPassword: strings.Repeat("a", 80)})
		require.Equal(t, http.StatusBadRequest, apperror.As(err).Status)
	})

	t.Run("holding the email of the account", func(t *testing.T) {
		user := googleOnlyAccount("sub-1")
		user.Email = "ibu.sari@example.test"
		uc := withGoogle(&fakeRepo{user: user}, freshClaims("sub-1"), nil)

		err := uc.SetPassword(ctx, user.ID, user.Role, dto.SetPasswordRequest{IDToken: credential(), NewPassword: "x-" + user.Email + "-x"})
		require.Equal(t, http.StatusBadRequest, apperror.As(err).Status)
	})

	t.Run("a failed update", func(t *testing.T) {
		user := googleOnlyAccount("sub-1")
		uc := withGoogle(&fakeRepo{user: user, err: errDown}, freshClaims("sub-1"), nil)

		err := uc.SetPassword(ctx, user.ID, user.Role, dto.SetPasswordRequest{IDToken: credential(), NewPassword: credential()})
		require.Equal(t, "INTERNAL_ERROR", apperror.As(err).Code)
	})
}

func TestLinkAndUnlinkGoogleEdges(t *testing.T) {
	ctx := context.Background()

	t.Run("link without a password is refused", func(t *testing.T) {
		user := &entity.User{ID: uuid.New(), Role: constants.RoleWali}
		uc := withGoogle(&fakeRepo{user: user}, freshClaims("sub-1"), nil)

		err := uc.LinkGoogle(ctx, user.ID, user.Role, dto.LinkGoogleRequest{IDToken: credential(), Password: credential()})
		require.Equal(t, "AUTH_PASSWORD_NOT_SET", apperror.As(err).Code)
	})

	t.Run("link whose guarded update finds the account changed", func(t *testing.T) {
		password := credential()
		user := accountOf(t, constants.RoleWali, password)
		uc := withGoogle(&fakeRepo{user: user, linkStale: true}, freshClaims("sub-7"), nil)

		err := uc.LinkGoogle(ctx, user.ID, user.Role, dto.LinkGoogleRequest{IDToken: credential(), Password: password})
		require.Equal(t, "GOOGLE_ALREADY_LINKED", apperror.As(err).Code)
	})

	t.Run("link whose update fails", func(t *testing.T) {
		password := credential()
		user := accountOf(t, constants.RoleWali, password)
		uc := withGoogle(&fakeRepo{user: user, err: errDown}, freshClaims("sub-7"), nil)

		err := uc.LinkGoogle(ctx, user.ID, user.Role, dto.LinkGoogleRequest{IDToken: credential(), Password: password})
		require.Equal(t, "INTERNAL_ERROR", apperror.As(err).Code)
	})

	t.Run("unlink whose guarded update finds the account changed or fails", func(t *testing.T) {
		password := credential()
		sub := "sub-1"
		user := accountOf(t, constants.RoleWali, password)
		user.GoogleSub = &sub

		err := withGoogle(&fakeRepo{user: user, unlinkStale: true}, freshClaims(sub), nil).
			UnlinkGoogle(ctx, user.ID, user.Role, dto.UnlinkGoogleRequest{Password: password})
		require.Equal(t, "GOOGLE_NOT_LINKED", apperror.As(err).Code)

		err = withGoogle(&fakeRepo{user: user, err: errDown}, freshClaims(sub), nil).
			UnlinkGoogle(ctx, user.ID, user.Role, dto.UnlinkGoogleRequest{Password: password})
		require.Equal(t, "INTERNAL_ERROR", apperror.As(err).Code)
	})

	t.Run("a Google only account that lost its link cannot confirm with a token", func(t *testing.T) {
		user := &entity.User{ID: uuid.New(), Role: constants.RoleWali}
		repo := &fakeRepo{user: user}

		err := withGoogle(repo, freshClaims("sub-1"), nil).DeleteAccount(ctx, user.ID, user.Role, dto.DeleteAccountRequest{IDToken: credential()})
		require.Equal(t, "GOOGLE_NOT_LINKED", apperror.As(err).Code)
	})

	t.Run("a verifier that fails for another reason answers unavailable", func(t *testing.T) {
		user := googleOnlyAccount("sub-1")

		for _, verifyErr := range []error{googleauth.ErrNotConfigured, errDown} {
			err := withGoogle(&fakeRepo{user: user}, googleauth.Claims{}, verifyErr).
				SetPassword(ctx, user.ID, user.Role, dto.SetPasswordRequest{IDToken: credential(), NewPassword: credential()})
			require.Equal(t, "AUTH_GOOGLE_UNAVAILABLE", apperror.As(err).Code)
		}
	})
}

var errDown = errors.New("database is down")
