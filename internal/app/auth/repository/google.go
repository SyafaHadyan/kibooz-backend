package repository

import (
	"context"
	"errors"

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
