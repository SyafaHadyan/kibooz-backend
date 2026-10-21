package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/push/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/push"
)

type saved struct {
	user     uuid.UUID
	token    string
	platform string
	at       time.Time
}

type fakeRepo struct {
	saves   []saved
	removed []string
	err     error
}

func (f *fakeRepo) SaveToken(_ context.Context, userID uuid.UUID, token string, platform string, now time.Time) error {
	f.saves = append(f.saves, saved{userID, token, platform, now})

	return f.err
}

func (f *fakeRepo) TokensOf(context.Context, []uuid.UUID) ([]push.TokenRow, error) { return nil, nil }

func (f *fakeRepo) DeleteTokenValues(context.Context, []string) error { return nil }

func (f *fakeRepo) DeleteToken(_ context.Context, _ uuid.UUID, token string) error {
	f.removed = append(f.removed, token)

	return f.err
}

func newUseCase(repo *fakeRepo) *PushUseCase {
	return &PushUseCase{repo: repo, now: func() time.Time { return time.Date(2026, 10, 21, 9, 0, 0, 123456789, time.UTC) }}
}

func requireAppError(t *testing.T, err error, want *apperror.Error) {
	t.Helper()

	var got *apperror.Error

	require.ErrorAs(t, err, &got)
	require.Equal(t, want.Code, got.Code)
}

func TestARegisteredTokenIsTrimmedAndDefaultsToAndroid(t *testing.T) {
	repo := &fakeRepo{}
	user := uuid.New()
	token := strings.Repeat("t", 40)

	require.NoError(t, newUseCase(repo).Register(context.Background(), user, dto.PushTokenRequest{Token: "  " + token + "\n"}))

	require.Equal(t, []saved{{user, token, "android", time.Date(2026, 10, 21, 9, 0, 0, 123456000, time.UTC)}}, repo.saves)
}

func TestATokenWithABlankInsideOrNothingInItIsRefused(t *testing.T) {
	repo := &fakeRepo{}
	useCase := newUseCase(repo)

	for _, token := range []string{"", "   ", strings.Repeat("a", 20) + " " + strings.Repeat("b", 20), "     " + strings.Repeat("c", 15), strings.Repeat("d", 4097)} {
		requireAppError(t, useCase.Register(context.Background(), uuid.New(), dto.PushTokenRequest{Token: token}), apperror.Validation(nil))
		requireAppError(t, useCase.Remove(context.Background(), uuid.New(), dto.RemovePushTokenRequest{Token: token}), apperror.Validation(nil))
	}

	require.Empty(t, repo.saves)
	require.Empty(t, repo.removed)
}

func TestARemovedTokenIsPassedOnAndAFailureIsAnInternalError(t *testing.T) {
	repo := &fakeRepo{}
	token := strings.Repeat("r", 30)

	require.NoError(t, newUseCase(repo).Remove(context.Background(), uuid.New(), dto.RemovePushTokenRequest{Token: token}))
	require.Equal(t, []string{token}, repo.removed)

	repo.err = errors.New("database down")

	requireAppError(t, newUseCase(repo).Remove(context.Background(), uuid.New(), dto.RemovePushTokenRequest{Token: token}), apperror.Internal(nil))
	requireAppError(
		t, newUseCase(repo).Register(context.Background(), uuid.New(), dto.PushTokenRequest{Token: token, Platform: "android"}), apperror.Internal(nil),
	)
}

func TestATokenOfADeletedAccountIsRefusedAsAnEndedSession(t *testing.T) {
	repo := &fakeRepo{err: repository.ErrAccountGone}

	requireAppError(
		t, newUseCase(repo).Register(context.Background(), uuid.New(), dto.PushTokenRequest{Token: strings.Repeat("g", 30)}), apperror.ErrTokenInvalid,
	)
}
