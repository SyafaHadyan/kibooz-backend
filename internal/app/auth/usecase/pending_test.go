package usecase_test

import (
	"context"
	"errors"
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
