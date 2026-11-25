package usecase_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/auth/usecase"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/googleauth"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
)

func (f *fakeRepo) FindUserByGoogleSub(context.Context, string) (*entity.User, error) {
	if f.user != nil && f.user.GoogleSub != nil {
		return f.user, nil
	}

	return nil, nil
}

func (f *fakeRepo) LinkGoogleSub(_ context.Context, _ uuid.UUID, sub string) error {
	if f.user != nil {
		f.user.GoogleSub = &sub
	}

	return nil
}

type emailOnlyLookupRepo struct {
	*fakeRepo
}

func (*emailOnlyLookupRepo) FindUserByGoogleSub(context.Context, string) (*entity.User, error) {
	return nil, nil
}

type stubGoogleVerifier struct {
	claims googleauth.Claims
	err    error
	calls  int
}

func (s *stubGoogleVerifier) Verify(context.Context, string) (googleauth.Claims, error) {
	s.calls++

	return s.claims, s.err
}

func testGoogleCredential() string {
	return "assertion-" + uuid.NewString()
}

func TestGoogleLoginFlowAndErrors(t *testing.T) {
	t.Run("existing account by google sub issues session and respects role", func(t *testing.T) {
		sub := "google-sub-1"
		user := accountWith(t, "passphrase-"+uuid.NewString())
		user.Role = constants.RoleGuru
		user.GoogleSub = &sub

		repo := &fakeRepo{}
		useCase := build(t, user, repo, newFakeCache())
		useCase.SetGoogleVerifier(&stubGoogleVerifier{
			claims: googleauth.Claims{Subject: sub, Email: user.Email, EmailVerified: true, FullName: "Bu Tika"},
		})

		credential := testGoogleCredential()
		res, err := useCase.GoogleLogin(context.Background(), dto.GoogleLoginRequest{
			IDToken: credential, Role: constants.RoleGuru,
		})
		require.NoError(t, err)
		require.False(t, res.NeedsRegistration)
		require.NotEmpty(t, res.Token)
		require.NotEmpty(t, res.RefreshToken)
		require.NotEmpty(t, res.DeviceToken)
		require.NotNil(t, res.User)
		require.Equal(t, constants.RoleGuru, res.User.Role)

		_, mismatchErr := useCase.GoogleLogin(context.Background(), dto.GoogleLoginRequest{
			IDToken: credential, Role: constants.RoleWali,
		})
		require.Equal(t, "AUTH_ROLE_MISMATCH", apperror.As(mismatchErr).Code)
	})

	t.Run("existing email without google sub links on verified google login", func(t *testing.T) {
		user := accountWith(t, "passphrase-"+uuid.NewString())
		user.Role = constants.RoleWali
		user.GoogleSub = nil

		repo := &fakeRepo{}
		useCase := build(t, user, repo, newFakeCache())
		useCase.SetGoogleVerifier(&stubGoogleVerifier{
			claims: googleauth.Claims{Subject: "new-sub-42", Email: "a@example.com", EmailVerified: true},
		})

		res, err := useCase.GoogleLogin(context.Background(), dto.GoogleLoginRequest{
			IDToken: testGoogleCredential(), Role: constants.RoleWali,
		})
		require.NoError(t, err)
		require.False(t, res.NeedsRegistration)
		require.NotNil(t, repo.user.GoogleSub)
		require.Equal(t, "new-sub-42", *repo.user.GoogleSub)
	})

	t.Run("unknown user triggers onboarding response without creating account", func(t *testing.T) {
		repo := &fakeRepo{}
		useCase := build(t, nil, repo, newFakeCache())
		useCase.SetGoogleVerifier(&stubGoogleVerifier{
			claims: googleauth.Claims{
				Subject: "fresh-sub", Email: " Baru@Gmail.com ", EmailVerified: true,
				FullName: "Bapak Anang", AvatarURL: "https://lh3.googleusercontent.com/a/photo",
			},
		})

		res, err := useCase.GoogleLogin(context.Background(), dto.GoogleLoginRequest{
			IDToken: testGoogleCredential(), Role: constants.RoleWali,
		})
		require.NoError(t, err)
		require.True(t, res.NeedsRegistration)
		require.Empty(t, res.Token)
		require.Nil(t, res.User)
		require.NotNil(t, res.GoogleProfile)
		require.Equal(t, "baru@gmail.com", res.GoogleProfile.Email)
		require.Equal(t, "Bapak Anang", res.GoogleProfile.FullName)
		require.NotNil(t, res.GoogleProfile.AvatarURL)
		require.Equal(t, "https://lh3.googleusercontent.com/a/photo", *res.GoogleProfile.AvatarURL)
	})

	t.Run("existing email bound to different google sub is refused", func(t *testing.T) {
		otherSub := "original-sub"
		user := accountWith(t, "passphrase-"+uuid.NewString())
		user.Role = constants.RoleWali
		user.GoogleSub = &otherSub

		// Simulate FindUserByGoogleSub returning nil when queried with another sub, while FindUserByEmail finds user.
		repo := &emailOnlyLookupRepo{fakeRepo: &fakeRepo{user: user}}
		useCase := usecase.NewAuthUseCase(
			repo, fakeJWT{}, newFakeCache(),
			&env.Env{
				JWTRefreshExpiredDays: 30, JWTSessionMaxDays: 90,
				JWTSecretKey:       strings.Repeat("k", 48),
				DeviceTokenTTLDays: 90, RefreshReuseLeewaySeconds: 10,
			},
			testHasher,
		)
		useCase.SetGoogleVerifier(&stubGoogleVerifier{
			claims: googleauth.Claims{Subject: "hijack-sub", Email: user.Email, EmailVerified: true},
		})

		_, err := useCase.GoogleLogin(context.Background(), dto.GoogleLoginRequest{
			IDToken: testGoogleCredential(), Role: constants.RoleWali,
		})
		require.Equal(t, "AUTH_GOOGLE_INVALID", apperror.As(err).Code)
	})

	t.Run("verifier errors map to clear status codes", func(t *testing.T) {
		cases := map[string]struct {
			err    error
			status int
			code   string
		}{
			"not configured": {err: googleauth.ErrNotConfigured, status: http.StatusServiceUnavailable, code: "AUTH_GOOGLE_UNAVAILABLE"},
			"invalid":        {err: googleauth.ErrInvalidToken, status: http.StatusUnauthorized, code: "AUTH_GOOGLE_INVALID"},
			"jwks outage":    {err: errors.New("timeout"), status: http.StatusServiceUnavailable, code: "AUTH_GOOGLE_UNAVAILABLE"},
		}

		for name, tc := range cases {
			t.Run(name, func(t *testing.T) {
				useCase := build(t, nil, &fakeRepo{}, newFakeCache())
				useCase.SetGoogleVerifier(&stubGoogleVerifier{err: tc.err})

				_, err := useCase.GoogleLogin(context.Background(), dto.GoogleLoginRequest{
					IDToken: testGoogleCredential(), Role: constants.RoleGuru,
				})
				appErr := apperror.As(err)
				require.Equal(t, tc.status, appErr.Status)
				require.Equal(t, tc.code, appErr.Code)
			})
		}
	})
}

func TestGoogleRegisterCreatesRoleAccountWithoutPassword(t *testing.T) {
	t.Run("guru onboarding succeeds and stores empty password hash", func(t *testing.T) {
		repo := &fakeRepo{}
		useCase := build(t, nil, repo, newFakeCache())
		useCase.SetGoogleVerifier(&stubGoogleVerifier{
			claims: googleauth.Claims{
				Subject: "sub-guru-1", Email: "ibu.guru@gmail.com", EmailVerified: true,
				FullName: "Ibu Guru", AvatarURL: "javascript:alert(1)",
			},
		})

		res, err := useCase.GoogleRegister(context.Background(), dto.GoogleRegisterRequest{
			IDToken: testGoogleCredential(), FullName: "Ibu Guru", Role: constants.RoleGuru,
			Class: &dto.RegisterClass{Name: "Melati"},
		})
		require.NoError(t, err)
		require.NotNil(t, repo.registered)
		require.NotEmpty(t, res.Token)
		require.NotEmpty(t, res.RefreshToken)
		require.NotEmpty(t, res.DeviceToken)
		require.Equal(t, constants.RoleGuru, res.User.Role)
		require.Nil(t, res.User.AvatarURL, "non-http google avatar URL should be discarded")
	})

	t.Run("wali onboarding validates class code and blank names before creation", func(t *testing.T) {
		repo := &fakeRepo{unknownClassCode: true}
		useCase := build(t, nil, repo, newFakeCache())
		useCase.SetGoogleVerifier(&stubGoogleVerifier{
			claims: googleauth.Claims{Subject: "sub-wali-1", Email: "wali@gmail.com", EmailVerified: true},
		})

		_, err := useCase.GoogleRegister(context.Background(), dto.GoogleRegisterRequest{
			IDToken: testGoogleCredential(), FullName: "Wali", Role: constants.RoleWali,
			ClassCode: "BADCODE1", Student: &dto.RegisterStudent{FullName: "Budi", NISN: "1234567890"},
		})
		require.ErrorIs(t, err, apperror.ErrClassCodeNotFound)
		require.Nil(t, repo.registered)

		repo.unknownClassCode = false
		res, err := useCase.GoogleRegister(context.Background(), dto.GoogleRegisterRequest{
			IDToken: testGoogleCredential(), FullName: "Wali", Role: constants.RoleWali,
			ClassCode: "ab12cd34", Student: &dto.RegisterStudent{FullName: "Budi", NISN: "1234567890"},
		})
		require.NoError(t, err)
		require.NotNil(t, repo.registered)
		require.Equal(t, constants.RoleWali, res.User.Role)
	})

	t.Run("already linked google sub returns conflict", func(t *testing.T) {
		sub := "taken-sub"
		existing := &entity.User{ID: uuid.New(), GoogleSub: &sub, Role: constants.RoleWali}
		repo := &fakeRepo{user: existing}
		useCase := build(t, existing, repo, newFakeCache())
		useCase.SetGoogleVerifier(&stubGoogleVerifier{
			claims: googleauth.Claims{Subject: sub, Email: "taken@gmail.com", EmailVerified: true},
		})

		_, err := useCase.GoogleRegister(context.Background(), dto.GoogleRegisterRequest{
			IDToken: testGoogleCredential(), FullName: "Test", Role: constants.RoleGuru,
			Class: &dto.RegisterClass{Name: "Bunga"},
		})
		require.ErrorIs(t, err, apperror.ErrGoogleAlreadyRegistered)
	})
}
