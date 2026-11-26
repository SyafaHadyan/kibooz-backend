package usecase_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/auth/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/app/auth/usecase"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/mailer"
)

func buildReplacing(repo *fakeRepo, required bool, remove usecase.AccountRemover) usecase.AuthUseCaseItf {
	cfg := &env.Env{
		JWTRefreshExpiredDays: 30, JWTSessionMaxDays: 90, JWTSecretKey: "a-secret-key-that-is-long-enough-for-the-tests",
		DeviceTokenTTLDays: 90, EmailVerificationRequired: required,
	}

	return usecase.NewAuthUseCase(repo, fakeJWT{}, newFakeCache(), cfg, testHasher, usecase.WithMailer(&fakeMail{}), usecase.WithAccountRemover(remove))
}

func TestARegistrationReplacesAnAccountThatWaitedMoreThanADayForItsCode(t *testing.T) {
	ctx := context.Background()
	account := repository.PendingAccount{ID: uuid.New(), Role: constants.RoleWali}

	t.Run("the old account is removed and the registration goes on", func(t *testing.T) {
		repo := &fakeRepo{taken: apperror.ErrEmailTaken, stale: []repository.PendingAccount{account}}

		var removed []repository.PendingAccount

		useCase := buildReplacing(repo, true, func(_ context.Context, a repository.PendingAccount, _ time.Time) error {
			removed = append(removed, a)
			repo.taken = nil

			return nil
		})

		before := time.Now()
		res, err := useCase.Register(ctx, newParentRegistration())
		require.NoError(t, err)
		require.True(t, res.VerificationRequired)
		require.Equal(t, []repository.PendingAccount{account}, removed)

		// the lookup asked for accounts older than a day
		require.WithinDuration(t, before.Add(-constants.PendingReplaceAfterHours*time.Hour), repo.staleBefore, time.Minute)
	})

	t.Run("nothing is removed when something else is taken too", func(t *testing.T) {
		repo := &fakeRepo{taken: apperror.ErrEmailTaken, takenElse: apperror.ErrNIPTaken, stale: []repository.PendingAccount{account}}
		called := false

		_, err := buildReplacing(repo, true, func(context.Context, repository.PendingAccount, time.Time) error {
			called = true

			return nil
		}).Register(ctx, newParentRegistration())
		require.Equal(t, "NIP_ALREADY_REGISTERED", apperror.As(err).Code)
		require.Equal(t, []uuid.UUID{account.ID}, repo.ignored)
		require.False(t, called)
	})

	t.Run("nothing is removed when the class does not take the child", func(t *testing.T) {
		repo := &fakeRepo{taken: apperror.ErrEmailTaken, joinErr: apperror.ErrClassFull, stale: []repository.PendingAccount{account}}
		called := false

		_, err := buildReplacing(repo, true, func(context.Context, repository.PendingAccount, time.Time) error {
			called = true

			return nil
		}).Register(ctx, newParentRegistration())
		require.Equal(t, "CLASS_FULL", apperror.As(err).Code)
		require.NotEmpty(t, repo.joinChecks)
		require.False(t, called)
	})

	t.Run("an account that waited less than a day keeps its email", func(t *testing.T) {
		repo := &fakeRepo{taken: apperror.ErrEmailTaken}
		called := false

		_, err := buildReplacing(repo, true, func(context.Context, repository.PendingAccount, time.Time) error {
			called = true

			return nil
		}).Register(ctx, newParentRegistration())
		require.Equal(t, "EMAIL_ALREADY_REGISTERED", apperror.As(err).Code)
		require.False(t, called)
	})

	t.Run("nothing is replaced while the verification is off", func(t *testing.T) {
		repo := &fakeRepo{taken: apperror.ErrEmailTaken, stale: []repository.PendingAccount{account}}
		called := false

		_, err := buildReplacing(repo, false, func(context.Context, repository.PendingAccount, time.Time) error {
			called = true

			return nil
		}).Register(ctx, newParentRegistration())
		require.Equal(t, "EMAIL_ALREADY_REGISTERED", apperror.As(err).Code)
		require.False(t, called)
	})

	t.Run("a NISN that is taken is not a reason to replace anything", func(t *testing.T) {
		repo := &fakeRepo{taken: apperror.ErrNISNTaken, stale: []repository.PendingAccount{account}}
		called := false

		_, err := buildReplacing(repo, true, func(context.Context, repository.PendingAccount, time.Time) error {
			called = true

			return nil
		}).Register(ctx, newParentRegistration())
		require.Equal(t, "NISN_ALREADY_REGISTERED", apperror.As(err).Code)
		require.False(t, called)
	})

	t.Run("a failed removal or lookup leaves the answer as it was", func(t *testing.T) {
		repo := &fakeRepo{taken: apperror.ErrEmailTaken, stale: []repository.PendingAccount{account}}

		_, err := buildReplacing(repo, true, func(context.Context, repository.PendingAccount, time.Time) error {
			return errors.New("database is down")
		}).Register(ctx, newParentRegistration())
		require.Equal(t, "EMAIL_ALREADY_REGISTERED", apperror.As(err).Code)

		repo = &fakeRepo{taken: apperror.ErrEmailTaken, staleErr: errors.New("database is down")}

		_, err = buildReplacing(repo, true, func(context.Context, repository.PendingAccount, time.Time) error { return nil }).Register(ctx, newParentRegistration())
		require.Equal(t, "EMAIL_ALREADY_REGISTERED", apperror.As(err).Code)
	})

	t.Run("without a remover nothing is replaced", func(t *testing.T) {
		repo := &fakeRepo{taken: apperror.ErrEmailTaken, stale: []repository.PendingAccount{account}}

		_, err := buildVerifying(repo, &fakeMail{}, true).Register(ctx, newParentRegistration())
		require.Equal(t, "EMAIL_ALREADY_REGISTERED", apperror.As(err).Code)
	})
}

func TestAnAccountThatHadAllItsCodesGetsNoMore(t *testing.T) {
	repo := &fakeRepo{storeErr: repository.ErrCodeLimit}
	pendingAccount(t, repo)

	_, err := buildVerifying(repo, &fakeMail{}, true).ResendVerification(
		context.Background(), dto.ResendVerificationRequest{Email: "a@example.com", Password: "correct horse"},
	)
	require.Equal(t, "VERIFICATION_CODE_LIMIT_REACHED", apperror.As(err).Code)
	require.Equal(t, 429, apperror.As(err).Status)
}

func TestACodeIsNotStoredForAnAccountThatIsGone(t *testing.T) {
	repo := &fakeRepo{storeErr: repository.ErrNotWaiting}
	pendingAccount(t, repo)

	_, err := buildVerifying(repo, &fakeMail{}, true).ResendVerification(
		context.Background(), dto.ResendVerificationRequest{Email: "a@example.com", Password: "correct horse"},
	)
	require.Equal(t, "AUTH_INVALID_CREDENTIALS", apperror.As(err).Code)
}

// failingMail answers every send with its error
func failingMail(err error) *fakeMail { return &fakeMail{err: err} }

func TestAFailedMailIsScheduledForARetryAndAGoodOneStopsThem(t *testing.T) {
	ctx := context.Background()

	t.Run("a failed mail is scheduled", func(t *testing.T) {
		repo := &fakeRepo{}
		pendingAccount(t, repo)

		_, err := buildVerifying(repo, failingMail(errors.New("resend answered 500")), true).ResendVerification(
			ctx, dto.ResendVerificationRequest{Email: "a@example.com", Password: "correct horse"},
		)
		require.Equal(t, "EMAIL_SEND_FAILED", apperror.As(err).Code)
		require.Equal(t, 1, repo.markedFailed)
		require.Zero(t, repo.clearedRetry)
	})

	t.Run("a mail that cannot be sent without a key is not retried", func(t *testing.T) {
		repo := &fakeRepo{}
		pendingAccount(t, repo)

		_, _ = buildVerifying(repo, failingMail(mailer.ErrDisabled), true).ResendVerification(
			ctx, dto.ResendVerificationRequest{Email: "a@example.com", Password: "correct horse"},
		)
		require.Zero(t, repo.markedFailed)
	})

	t.Run("a good mail clears the retries", func(t *testing.T) {
		repo := &fakeRepo{}
		pendingAccount(t, repo)

		_, err := buildVerifying(repo, &fakeMail{}, true).ResendVerification(
			ctx, dto.ResendVerificationRequest{Email: "a@example.com", Password: "correct horse"},
		)
		require.NoError(t, err)
		require.Equal(t, 1, repo.clearedRetry)
		require.Zero(t, repo.markedFailed)
	})
}

func TestRetryVerification(t *testing.T) {
	ctx := context.Background()

	t.Run("a new code is mailed to an account that waits", func(t *testing.T) {
		repo, mail := &fakeRepo{}, &fakeMail{}
		user := pendingAccount(t, repo)

		require.NoError(t, buildVerifying(repo, mail, true).RetryVerification(ctx, user.ID))
		require.Len(t, mail.codes, 1)
		require.Equal(t, 1, repo.clearedRetry)
	})

	t.Run("an account that is verified, gone or not waiting any more only stops the retries", func(t *testing.T) {
		repo, mail := &fakeRepo{}, &fakeMail{}

		require.NoError(t, buildVerifying(repo, mail, true).RetryVerification(ctx, uuid.New()))
		require.Empty(t, mail.codes)
		require.Equal(t, 1, repo.clearedRetry)

		user := pendingAccount(t, repo)
		user.EmailVerificationPending = false

		require.NoError(t, buildVerifying(repo, mail, true).RetryVerification(ctx, user.ID))
		require.Empty(t, mail.codes)
		require.Equal(t, 2, repo.clearedRetry)
	})

	t.Run("nothing is retried while the verification is off", func(t *testing.T) {
		repo, mail := &fakeRepo{}, &fakeMail{}
		user := pendingAccount(t, repo)

		require.NoError(t, buildVerifying(repo, mail, false).RetryVerification(ctx, user.ID))
		require.Empty(t, mail.codes)
	})

	t.Run("an account that had all its codes is let go", func(t *testing.T) {
		repo, mail := &fakeRepo{storeErr: repository.ErrCodeLimit}, &fakeMail{}
		user := pendingAccount(t, repo)

		require.NoError(t, buildVerifying(repo, mail, true).RetryVerification(ctx, user.ID))
		require.Equal(t, 1, repo.clearedRetry)
	})

	t.Run("an account that was removed in the meantime is let go", func(t *testing.T) {
		repo, mail := &fakeRepo{storeErr: repository.ErrNotWaiting}, &fakeMail{}
		user := pendingAccount(t, repo)

		require.NoError(t, buildVerifying(repo, mail, true).RetryVerification(ctx, user.ID))
		require.Equal(t, 1, repo.clearedRetry)
		require.Empty(t, mail.codes)
	})

	t.Run("a code that was asked for in the meantime is not mailed again", func(t *testing.T) {
		repo, mail := &fakeRepo{storeWait: time.Minute}, &fakeMail{}
		user := pendingAccount(t, repo)

		require.NoError(t, buildVerifying(repo, mail, true).RetryVerification(ctx, user.ID))
		require.Empty(t, mail.codes)
		require.Zero(t, repo.clearedRetry)
	})

	t.Run("a quota answer is handed back and another failure is not", func(t *testing.T) {
		repo := &fakeRepo{}
		user := pendingAccount(t, repo)

		err := buildVerifying(repo, failingMail(fmt.Errorf("%w, resend answered 429", mailer.ErrQuota)), true).RetryVerification(ctx, user.ID)
		require.ErrorIs(t, err, mailer.ErrQuota)

		err = buildVerifying(repo, failingMail(errors.New("resend answered 500")), true).RetryVerification(ctx, user.ID)
		require.NoError(t, err)
		require.Equal(t, 2, repo.markedFailed, "both failures are scheduled again")
	})

	t.Run("a retry that is not wanted any more mails nothing", func(t *testing.T) {
		repo, mail := &fakeRepo{storeErr: repository.ErrNoRetry}, &fakeMail{}
		user := pendingAccount(t, repo)

		require.NoError(t, buildVerifying(repo, mail, true).RetryVerification(ctx, user.ID))
		require.Empty(t, mail.codes)
	})

	t.Run("an answer about the rate is handed back", func(t *testing.T) {
		repo := &fakeRepo{}
		user := pendingAccount(t, repo)

		err := buildVerifying(repo, failingMail(mailer.ErrRateLimit), true).RetryVerification(ctx, user.ID)
		require.ErrorIs(t, err, mailer.ErrRateLimit)
	})

	t.Run("a failed lookup is an error", func(t *testing.T) {
		repo := &fakeRepo{findErr: errors.New("database is down")}

		require.Error(t, buildVerifying(repo, &fakeMail{}, true).RetryVerification(ctx, uuid.New()))
	})
}
