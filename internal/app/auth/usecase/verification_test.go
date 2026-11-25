package usecase_test

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/auth/usecase"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
)

// fakeMail keeps what would have been mailed
type fakeMail struct {
	to    []string
	codes []string
	err   error
}

func (m *fakeMail) SendVerificationCode(_ context.Context, to string, _ string, code string, _ int) error {
	if m.err != nil {
		return m.err
	}

	m.to = append(m.to, to)
	m.codes = append(m.codes, code)

	return nil
}

func (m *fakeMail) last(t *testing.T) string {
	t.Helper()
	require.NotEmpty(t, m.codes, "no mail was sent")

	return m.codes[len(m.codes)-1]
}

// buildVerifying builds a use case that requires the verification of a new email
func buildVerifying(repo *fakeRepo, mail *fakeMail, required bool) usecase.AuthUseCaseItf {
	cfg := &env.Env{
		JWTRefreshExpiredDays: 30, JWTSessionMaxDays: 90, JWTSecretKey: "a-secret-key-that-is-long-enough-for-the-tests",
		DeviceTokenTTLDays: 90, EmailVerificationRequired: required,
	}

	return usecase.NewAuthUseCase(repo, fakeJWT{}, newFakeCache(), cfg, testHasher, usecase.WithMailer(mail))
}

func newParentRegistration() dto.RegisterRequest {
	return dto.RegisterRequest{
		Email: "New.Parent@Example.com", Password: "correct horse", FullName: "New Parent", Role: constants.RoleWali,
		ClassCode: "ABC234", Student: &dto.RegisterStudent{NISN: "12345", FullName: "New Child"},
	}
}

// pendingAccount is an account that waits for its code, and its row is what the repository finds by email
func pendingAccount(t *testing.T, repo *fakeRepo) *entity.User {
	t.Helper()

	user := accountWith(t, "correct horse")
	user.EmailVerificationPending = true
	repo.user = user

	return user
}

func TestARegistrationThatRequiresTheCodeStoresNoSessionAndMailsTheCode(t *testing.T) {
	repo, mail := &fakeRepo{}, &fakeMail{}

	res, err := buildVerifying(repo, mail, true).Register(context.Background(), newParentRegistration())
	require.NoError(t, err)

	require.Nil(t, repo.registered, "an account that waits gets no session")
	require.True(t, repo.createdUser.EmailVerificationPending)
	require.True(t, res.VerificationRequired)
	require.Empty(t, res.Token)
	require.Empty(t, res.RefreshToken)
	require.NotEmpty(t, res.DeviceToken, "the device token only affects the rate limit, so it is still handed out")
	require.Equal(t, "new.parent@example.com", res.User.Email)

	require.NotNil(t, res.Verification)
	require.True(t, res.Verification.Sent)
	require.Equal(t, constants.EmailCodeTTLMinutes*60, res.Verification.ExpiresInSeconds)
	require.Equal(t, constants.EmailResendCooldownSeconds, res.Verification.ResendAfterSeconds)

	require.Equal(t, []string{"new.parent@example.com"}, mail.to)
	require.Regexp(t, regexp.MustCompile(`^\d{6}$`), mail.last(t))
	require.NotContains(t, repo.live.hash, mail.last(t), "the code itself is never stored")
}

func TestARegistrationThatDoesNotRequireTheCodeIsAsBefore(t *testing.T) {
	repo, mail := &fakeRepo{}, &fakeMail{}

	res, err := buildVerifying(repo, mail, false).Register(context.Background(), newParentRegistration())
	require.NoError(t, err)

	require.NotNil(t, repo.registered)
	require.False(t, repo.createdUser.EmailVerificationPending)
	require.False(t, res.VerificationRequired)
	require.Nil(t, res.Verification)
	require.NotEmpty(t, res.RefreshToken)
	require.Empty(t, mail.codes)
	require.Zero(t, repo.stores)
}

func TestARegistrationStillSucceedsWhenTheMailCannotBeSent(t *testing.T) {
	for name, mail := range map[string]*fakeMail{
		"the mail service answers an error": {err: errors.New("resend answered 429")},
	} {
		t.Run(name, func(t *testing.T) {
			repo := &fakeRepo{}
			useCase := buildVerifying(repo, mail, true)

			res, err := useCase.Register(context.Background(), newParentRegistration())
			require.NoError(t, err, "the account exists, so the registration is not a failure")

			require.True(t, res.VerificationRequired)
			require.False(t, res.Verification.Sent)
			require.Empty(t, res.RefreshToken)
		})
	}

	t.Run("the code could not be stored", func(t *testing.T) {
		repo, mail := &fakeRepo{storeErr: errors.New("database is down")}, &fakeMail{}

		res, err := buildVerifying(repo, mail, true).Register(context.Background(), newParentRegistration())
		require.NoError(t, err)
		require.False(t, res.Verification.Sent)
		require.Empty(t, mail.codes, "no mail for a code that is not stored")
	})
}

func TestASignInIsRefusedForAnAccountThatWaitsOnlyAfterThePasswordMatched(t *testing.T) {
	repo := &fakeRepo{}
	pendingAccount(t, repo)
	useCase := buildVerifying(repo, &fakeMail{}, true)

	_, err := useCase.Login(context.Background(), dto.LoginRequest{Email: "a@example.com", Password: "correct horse", Role: constants.RoleWali})
	require.Equal(t, "EMAIL_NOT_VERIFIED", apperror.As(err).Code)
	require.Equal(t, 403, apperror.As(err).Status)
	require.Zero(t, repo.separateTokens, "no session is stored")

	_, err = useCase.Login(context.Background(), dto.LoginRequest{Email: "a@example.com", Password: "wrong password", Role: constants.RoleWali})
	require.Equal(t, "AUTH_INVALID_CREDENTIALS", apperror.As(err).Code, "a wrong password does not learn that the account waits")

	_, err = useCase.Login(context.Background(), dto.LoginRequest{Email: "a@example.com", Password: "correct horse", Role: constants.RoleGuru})
	require.Equal(t, "AUTH_INVALID_CREDENTIALS", apperror.As(err).Code, "a wrong role does not learn it either")
}

func TestAWaitingAccountSignsInWhenTheSettingIsOff(t *testing.T) {
	repo := &fakeRepo{}
	pendingAccount(t, repo)

	res, err := buildVerifying(repo, &fakeMail{}, false).Login(
		context.Background(), dto.LoginRequest{Email: "a@example.com", Password: "correct horse", Role: constants.RoleWali},
	)
	require.NoError(t, err)
	require.NotEmpty(t, res.RefreshToken, "switching the setting off must not lock the waiting accounts out")
}

func TestTheCodeOfTheMailOpensTheFirstSession(t *testing.T) {
	repo, mail := &fakeRepo{}, &fakeMail{}
	user := pendingAccount(t, repo)
	useCase := buildVerifying(repo, mail, true)

	_, err := useCase.ResendVerification(context.Background(), dto.ResendVerificationRequest{Email: "a@example.com", Password: "correct horse"})
	require.NoError(t, err)

	res, err := useCase.VerifyEmail(context.Background(), dto.VerifyEmailRequest{Email: "A@example.com", Code: mail.last(t)})
	require.NoError(t, err)
	require.NotEmpty(t, res.RefreshToken)
	require.Equal(t, 1, repo.separateTokens)
	require.Equal(t, user.PasswordHash, repo.verifiedHash, "the session is stored for the hash that was read")
	require.False(t, res.VerificationRequired)

	t.Run("the same code works once", func(t *testing.T) {
		repo.user.EmailVerificationPending = true

		_, err := useCase.VerifyEmail(context.Background(), dto.VerifyEmailRequest{Email: "a@example.com", Code: mail.last(t)})
		require.Equal(t, "VERIFICATION_CODE_INVALID", apperror.As(err).Code)
	})
}

func TestEveryRefusedCodeGivesTheSameAnswer(t *testing.T) {
	repo, mail := &fakeRepo{}, &fakeMail{}
	pendingAccount(t, repo)
	useCase := buildVerifying(repo, mail, true)

	_, err := useCase.ResendVerification(context.Background(), dto.ResendVerificationRequest{Email: "a@example.com", Password: "correct horse"})
	require.NoError(t, err)

	right := mail.last(t)
	wrong := "000000"

	if right == wrong {
		wrong = "000001"
	}

	verify := func(email string, code string) error {
		_, err := useCase.VerifyEmail(context.Background(), dto.VerifyEmailRequest{Email: email, Code: code})

		return err
	}

	t.Run("a wrong code", func(t *testing.T) {
		err := verify("a@example.com", wrong)
		require.Equal(t, "VERIFICATION_CODE_INVALID", apperror.As(err).Code)
		require.Equal(t, 400, apperror.As(err).Status)
	})

	t.Run("an email that no account has", func(t *testing.T) {
		repo.user = nil

		require.Equal(t, "VERIFICATION_CODE_INVALID", apperror.As(verify("nobody@example.com", right)).Code)
	})

	t.Run("an account that is not waiting", func(t *testing.T) {
		repo.user = accountWith(t, "correct horse")

		require.Equal(t, "VERIFICATION_CODE_INVALID", apperror.As(verify("a@example.com", right)).Code)
	})
}

func TestACodeStopsWorkingAfterItsAttemptsAreUsedUp(t *testing.T) {
	repo, mail := &fakeRepo{}, &fakeMail{}
	pendingAccount(t, repo)
	useCase := buildVerifying(repo, mail, true)

	_, err := useCase.ResendVerification(context.Background(), dto.ResendVerificationRequest{Email: "a@example.com", Password: "correct horse"})
	require.NoError(t, err)

	right := mail.last(t)
	wrong := "000000"

	if right == wrong {
		wrong = "000001"
	}

	for range constants.EmailCodeMaxAttempts {
		_, err = useCase.VerifyEmail(context.Background(), dto.VerifyEmailRequest{Email: "a@example.com", Code: wrong})
		require.Equal(t, "VERIFICATION_CODE_INVALID", apperror.As(err).Code)
	}

	_, err = useCase.VerifyEmail(context.Background(), dto.VerifyEmailRequest{Email: "a@example.com", Code: right})
	require.Equal(t, "VERIFICATION_CODE_INVALID", apperror.As(err).Code, "the right code is refused once the attempts are used up")
	require.Zero(t, repo.separateTokens)
}

func TestAnErrorOfTheRepositoryIsNotAWrongCode(t *testing.T) {
	repo := &fakeRepo{checkErr: errors.New("database is down")}
	pendingAccount(t, repo)

	_, err := buildVerifying(repo, &fakeMail{}, true).VerifyEmail(context.Background(), dto.VerifyEmailRequest{Email: "a@example.com", Code: "123456"})
	require.Equal(t, "INTERNAL_ERROR", apperror.As(err).Code)
}

func TestAResendNeedsThePasswordAndAWaitingAccount(t *testing.T) {
	t.Run("a wrong password mails nobody", func(t *testing.T) {
		repo, mail := &fakeRepo{}, &fakeMail{}
		pendingAccount(t, repo)

		_, err := buildVerifying(repo, mail, true).ResendVerification(
			context.Background(), dto.ResendVerificationRequest{Email: "a@example.com", Password: "wrong password"},
		)
		require.Equal(t, "AUTH_INVALID_CREDENTIALS", apperror.As(err).Code)
		require.Empty(t, mail.codes)
		require.Zero(t, repo.stores)
	})

	t.Run("an email that no account has gives the same answer as a wrong password", func(t *testing.T) {
		repo, mail := &fakeRepo{}, &fakeMail{}

		_, err := buildVerifying(repo, mail, true).ResendVerification(
			context.Background(), dto.ResendVerificationRequest{Email: "nobody@example.com", Password: "correct horse"},
		)
		require.Equal(t, "AUTH_INVALID_CREDENTIALS", apperror.As(err).Code)
		require.Empty(t, mail.codes)
	})

	t.Run("an account that is verified already", func(t *testing.T) {
		repo, mail := &fakeRepo{user: accountWith(t, "correct horse")}, &fakeMail{}

		_, err := buildVerifying(repo, mail, true).ResendVerification(
			context.Background(), dto.ResendVerificationRequest{Email: "a@example.com", Password: "correct horse"},
		)
		require.Equal(t, "EMAIL_ALREADY_VERIFIED", apperror.As(err).Code)
		require.Empty(t, mail.codes)
	})

	t.Run("a code is sent to the waiting account", func(t *testing.T) {
		repo, mail := &fakeRepo{}, &fakeMail{}
		pendingAccount(t, repo)

		res, err := buildVerifying(repo, mail, true).ResendVerification(
			context.Background(), dto.ResendVerificationRequest{Email: "a@example.com", Password: "correct horse"},
		)
		require.NoError(t, err)
		require.True(t, res.Sent)
		require.Equal(t, []string{"a@example.com"}, mail.to)
	})
}

func TestAResendTooSoonSaysHowLongToWaitAndSendsNothing(t *testing.T) {
	repo, mail := &fakeRepo{storeWait: 42*1000*1000*1000 + 1}, &fakeMail{}
	pendingAccount(t, repo)

	_, err := buildVerifying(repo, mail, true).ResendVerification(
		context.Background(), dto.ResendVerificationRequest{Email: "a@example.com", Password: "correct horse"},
	)

	appErr := apperror.As(err)
	require.Equal(t, "VERIFICATION_RESEND_TOO_SOON", appErr.Code)
	require.Equal(t, 429, appErr.Status)
	require.Equal(t, 43, appErr.RetryAfterSeconds, "a wait that is part of a second is rounded up")
	require.Empty(t, mail.codes)
}

func TestAResendTellsTheCallerWhenTheMailFails(t *testing.T) {
	repo, mail := &fakeRepo{}, &fakeMail{err: errors.New("resend answered 503")}
	pendingAccount(t, repo)

	_, err := buildVerifying(repo, mail, true).ResendVerification(
		context.Background(), dto.ResendVerificationRequest{Email: "a@example.com", Password: "correct horse"},
	)
	require.Equal(t, "EMAIL_SEND_FAILED", apperror.As(err).Code)
	require.Equal(t, 503, apperror.As(err).Status)
}

func TestACodeOfOneAccountNeverMatchesTheRowOfAnother(t *testing.T) {
	repo, mail := &fakeRepo{}, &fakeMail{}
	first := pendingAccount(t, repo)
	useCase := buildVerifying(repo, mail, true)

	_, err := useCase.ResendVerification(context.Background(), dto.ResendVerificationRequest{Email: "a@example.com", Password: "correct horse"})
	require.NoError(t, err)

	code := mail.last(t)

	other := accountWith(t, "correct horse")
	other.ID = uuid.New()
	other.EmailVerificationPending = true
	repo.user = other

	_, err = useCase.VerifyEmail(context.Background(), dto.VerifyEmailRequest{Email: "a@example.com", Code: code})
	require.Equal(t, "VERIFICATION_CODE_INVALID", apperror.As(err).Code)
	require.NotEqual(t, first.ID, other.ID)
}
