// Package repository handles the profile and avatar database operations
package repository

import (
	"context"
	"errors"
	"sort"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/ranking"
)

type UserDBItf interface {
	UpdateUserAvatar(ctx context.Context, userID uuid.UUID, url string) error
	FindWaliIDByUserID(ctx context.Context, userID uuid.UUID) (*uuid.UUID, error)
	// UpdateStudentAvatar returns the class of the child, or nil when the wali has no such child
	UpdateStudentAvatar(ctx context.Context, waliID uuid.UUID, studentID uuid.UUID, url string) (*uuid.UUID, error)
	// FindUserByID returns nil when the account does not exist or is already deleted
	FindUserByID(ctx context.Context, id uuid.UUID) (*entity.User, error)
	// SoftDeleteAccount hides the account with its profile and children, revokes every session and
	// returns the classes whose ranking changed
	SoftDeleteAccount(ctx context.Context, userID uuid.UUID, role constants.Role) ([]uuid.UUID, error)
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

func (r *UserDB) FindUserByID(ctx context.Context, id uuid.UUID) (*entity.User, error) {
	var user entity.User

	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &user, nil
}

func (r *UserDB) SoftDeleteAccount(ctx context.Context, userID uuid.UUID, role constants.Role) ([]uuid.UUID, error) {
	var classIDs []uuid.UUID

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error

		switch role {
		case constants.RoleWali:
			classIDs, err = softDeleteWali(tx, userID)
		case constants.RoleGuru:
			err = tx.Where("user_id = ?", userID).Delete(&entity.Guru{}).Error
		}

		if err != nil {
			return err
		}

		err = tx.Where("id = ?", userID).Delete(&entity.User{}).Error
		if err != nil {
			return err
		}

		// refresh tokens have no soft delete, removing them ends every session
		return tx.Where("user_id = ?", userID).Delete(&entity.RefreshToken{}).Error
	})

	return classIDs, err
}

// softDeleteWali hides the parent and their children, then renumbers the ranking of the affected classes
func softDeleteWali(tx *gorm.DB, userID uuid.UUID) ([]uuid.UUID, error) {
	var wali entity.Wali

	err := tx.Select("id").Where("user_id = ?", userID).Take(&wali).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	var classIDs []uuid.UUID

	err = tx.Model(&entity.Student{}).Where("wali_id = ?", wali.ID).Distinct().Pluck("class_id", &classIDs).Error
	if err != nil {
		return nil, err
	}

	// one fixed order for the class locks, so two deletions in the same classes cannot deadlock
	sort.Slice(classIDs, func(i, j int) bool { return classIDs[i].String() < classIDs[j].String() })

	for _, classID := range classIDs {
		err = ranking.Lock(tx, classID)
		if err != nil {
			return nil, err
		}
	}

	err = tx.Where("wali_id = ?", wali.ID).Delete(&entity.Student{}).Error
	if err != nil {
		return nil, err
	}

	for _, classID := range classIDs {
		err = ranking.Recompute(tx, classID)
		if err != nil {
			return nil, err
		}
	}

	err = tx.Where("id = ?", wali.ID).Delete(&entity.Wali{}).Error
	if err != nil {
		return nil, err
	}

	return classIDs, nil
}
