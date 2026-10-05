// Package repository handles the profile and avatar database operations
package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
)

type UserDBItf interface {
	UpdateUserAvatar(ctx context.Context, userID uuid.UUID, url string) error
	FindWaliIDByUserID(ctx context.Context, userID uuid.UUID) (*uuid.UUID, error)
	// UpdateStudentAvatar returns the class of the child, or nil when the wali has no such child
	UpdateStudentAvatar(ctx context.Context, waliID uuid.UUID, studentID uuid.UUID, url string) (*uuid.UUID, error)
}

type UserDB struct {
	db *gorm.DB
}

func NewUserDB(db *gorm.DB) UserDBItf {
	return &UserDB{db: db}
}

func (r *UserDB) UpdateUserAvatar(ctx context.Context, userID uuid.UUID, url string) error {
	return r.db.WithContext(ctx).Model(&entity.User{}).Where("id = ?", userID).Update("avatar_url", url).Error
}

func (r *UserDB) FindWaliIDByUserID(ctx context.Context, userID uuid.UUID) (*uuid.UUID, error) {
	var wali entity.Wali

	err := r.db.WithContext(ctx).Select("id").Where("user_id = ?", userID).Take(&wali).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &wali.ID, nil
}

func (r *UserDB) UpdateStudentAvatar(ctx context.Context, waliID uuid.UUID, studentID uuid.UUID, url string) (*uuid.UUID, error) {
	var student entity.Student

	err := r.db.WithContext(ctx).Select("id", "class_id").
		Where("id = ? AND wali_id = ?", studentID, waliID).Take(&student).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	err = r.db.WithContext(ctx).Model(&entity.Student{}).Where("id = ?", student.ID).Update("avatar_url", url).Error
	if err != nil {
		return nil, err
	}

	return &student.ClassID, nil
}
