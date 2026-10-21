// Package usecase holds the rules of the push tokens
package usecase

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/push/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
)

// platformAndroid is the only platform for now, and what a request without one means
const platformAndroid = "android"

type PushUseCaseItf interface {
	// Register stores the token of an installed app for the account
	Register(ctx context.Context, userID uuid.UUID, req dto.PushTokenRequest) error
	// Remove forgets the token of a phone that signed out, and removing it twice changes nothing
	Remove(ctx context.Context, userID uuid.UUID, req dto.RemovePushTokenRequest) error
}

type PushUseCase struct {
	repo repository.PushDBItf
	now  func() time.Time
}

func NewPushUseCase(repo repository.PushDBItf) PushUseCaseItf {
	return &PushUseCase{repo: repo, now: time.Now}
}

// token lengths that the table accepts, counted after the trim
const (
	minTokenLength = 20
	maxTokenLength = 4096
)

// clean trims the token and refuses one with a blank inside, which Firebase never gives, or one that is too short or too
// long once it is trimmed
func clean(token string) (string, bool) {
	token = strings.TrimSpace(token)
	length := utf8.RuneCountInString(token)

	return token, length >= minTokenLength && length <= maxTokenLength && !strings.ContainsAny(token, " \t\r\n")
}

func (u *PushUseCase) Register(ctx context.Context, userID uuid.UUID, req dto.PushTokenRequest) error {
	token, ok := clean(req.Token)
	if !ok {
		return apperror.Validation(map[string]string{"token": "is not a push token"})
	}

	platform := req.Platform
	if platform == "" {
		platform = platformAndroid
	}

	err := u.repo.SaveToken(ctx, userID, token, platform, u.now().UTC().Truncate(time.Microsecond))
	if errors.Is(err, repository.ErrAccountGone) {
		return apperror.ErrTokenInvalid
	}

	if err != nil {
		return apperror.Internal(err)
	}

	return nil
}

func (u *PushUseCase) Remove(ctx context.Context, userID uuid.UUID, req dto.RemovePushTokenRequest) error {
	token, ok := clean(req.Token)
	if !ok {
		return apperror.Validation(map[string]string{"token": "is not a push token"})
	}

	err := u.repo.DeleteToken(ctx, userID, token)
	if err != nil {
		return apperror.Internal(err)
	}

	return nil
}
