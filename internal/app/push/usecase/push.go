// Package usecase holds the rules of the push tokens
package usecase

import (
	"context"
	"strings"
	"time"

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

// clean trims the token and refuses one with a blank inside, which Firebase never gives
func clean(token string) (string, bool) {
	token = strings.TrimSpace(token)

	return token, token != "" && !strings.ContainsAny(token, " \t\r\n")
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
