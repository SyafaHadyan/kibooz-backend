package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
)

func (r *AuthDB) FindUserByGoogleSub(ctx context.Context, sub string) (*entity.User, error) {
	var user entity.User

	err := r.db.WithContext(ctx).Where("google_sub = ?", sub).Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &user, nil
}

func (r *AuthDB) LinkGoogleSub(ctx context.Context, userID uuid.UUID, sub string) error {
	err := r.db.WithContext(ctx).Model(&entity.User{}).
		Where("id = ? AND google_sub IS NULL", userID).
		Update("google_sub", sub).Error

	return mapUniqueViolation(err)
}
