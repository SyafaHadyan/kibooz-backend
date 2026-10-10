// Package repository handles the profile and avatar database operations
package repository

import (
	"context"
	"errors"
	"sort"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/ranking"
)

type UserDBItf interface {
	// UpdateUserAvatar returns the avatar URL it replaced, and false when the account does not exist or is deleted
	UpdateUserAvatar(ctx context.Context, userID uuid.UUID, url string) (previous string, found bool, err error)
	FindWaliIDByUserID(ctx context.Context, userID uuid.UUID) (*uuid.UUID, error)
	// UpdateStudentAvatar returns the class of the child and the avatar URL it replaced, or a nil class when the
	// wali has no such child
	UpdateStudentAvatar(ctx context.Context, waliID uuid.UUID, studentID uuid.UUID, url string) (*uuid.UUID, string, error)
	// FindUserByID returns nil when the account does not exist or is already deleted
	FindUserByID(ctx context.Context, id uuid.UUID) (*entity.User, error)
	// SoftDeleteAccount hides the account with its profile and children, revokes every session and clears the
	// addresses of their files. It returns the classes whose ranking changed and the addresses that were cleared,
	// so the caller can delete the files.
	SoftDeleteAccount(ctx context.Context, userID uuid.UUID, role constants.Role) (DeletedAccount, error)
}

// DeletedAccount is what a deleted account leaves behind for the caller to clean up
type DeletedAccount struct {
	ClassIDs []uuid.UUID
	FileURLs []string
}

type UserDB struct {
	db *gorm.DB
}

func NewUserDB(db *gorm.DB) UserDBItf {
	return &UserDB{db: db}
}

func (r *UserDB) UpdateUserAvatar(ctx context.Context, userID uuid.UUID, url string) (string, bool, error) {
	var previous string

	found := true

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user entity.User

		// the row lock makes two uploads at once replace each other one after the other, so no file is forgotten
		err := tx.Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
			Select("id", "avatar_url").Where("id = ?", userID).Take(&user).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			found = false

			return nil
		}

		if err != nil {
			return err
		}

		if user.AvatarURL != nil {
			previous = *user.AvatarURL
		}

		return tx.Model(&entity.User{}).Where("id = ?", userID).Update("avatar_url", url).Error
	})
	if err != nil {
		return "", false, err
	}

	return previous, found, nil
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

func (r *UserDB) UpdateStudentAvatar(
	ctx context.Context, waliID uuid.UUID, studentID uuid.UUID, url string,
) (*uuid.UUID, string, error) {
	var (
		classID  *uuid.UUID
		previous string
	)

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var student entity.Student

		err := tx.Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
			Select("id", "class_id", "avatar_url").
			Where("id = ? AND wali_id = ?", studentID, waliID).Take(&student).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}

		if err != nil {
			return err
		}

		err = tx.Model(&entity.Student{}).Where("id = ?", student.ID).Update("avatar_url", url).Error
		if err != nil {
			return err
		}

		classID = &student.ClassID

		if student.AvatarURL != nil {
			previous = *student.AvatarURL
		}

		return nil
	})
	if err != nil {
		return nil, "", err
	}

	return classID, previous, nil
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

func (r *UserDB) SoftDeleteAccount(ctx context.Context, userID uuid.UUID, role constants.Role) (DeletedAccount, error) {
	var deleted DeletedAccount

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// a retried transaction starts from nothing
		deleted = DeletedAccount{}

		var err error

		switch role {
		case constants.RoleWali:
			deleted.ClassIDs, deleted.FileURLs, err = softDeleteWali(tx, userID)
		case constants.RoleGuru:
			err = tx.Where("user_id = ?", userID).Delete(&entity.Guru{}).Error
		}

		if err != nil {
			return err
		}

		avatars, err := takeAddresses(tx.Model(&entity.User{}).Where("id = ?", userID), "avatar_url")
		if err != nil {
			return err
		}

		deleted.FileURLs = append(deleted.FileURLs, avatars...)

		err = tx.Where("id = ?", userID).Delete(&entity.User{}).Error
		if err != nil {
			return err
		}

		// refresh tokens have no soft delete, removing them ends every session
		return tx.Where("user_id = ?", userID).Delete(&entity.RefreshToken{}).Error
	})
	if err != nil {
		return DeletedAccount{}, err
	}

	return deleted, nil
}

// takeAddresses returns the file addresses that rows hold in column and clears them, so no row points at a file that is
// about to be deleted
func takeAddresses(rows *gorm.DB, column string) ([]string, error) {
	var urls []string

	err := rows.Session(&gorm.Session{}).Where(column+" IS NOT NULL AND "+column+" <> ''").Pluck(column, &urls).Error
	if err != nil {
		return nil, err
	}

	if len(urls) == 0 {
		return nil, nil
	}

	err = rows.Session(&gorm.Session{}).Update(column, nil).Error
	if err != nil {
		return nil, err
	}

	return urls, nil
}

// softDeleteWali hides the parent and their children, then renumbers the ranking of the affected classes. It also
// returns the addresses of the avatars of the children and of the photos of their trash scans, which are cleared.
func softDeleteWali(tx *gorm.DB, userID uuid.UUID) ([]uuid.UUID, []string, error) {
	var wali entity.Wali

	err := tx.Select("id").Where("user_id = ?", userID).Take(&wali).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, nil
	}

	if err != nil {
		return nil, nil, err
	}

	var classIDs []uuid.UUID

	err = tx.Model(&entity.Student{}).Where("wali_id = ?", wali.ID).Distinct().Pluck("class_id", &classIDs).Error
	if err != nil {
		return nil, nil, err
	}

	// one fixed order for the class locks, so two deletions in the same classes cannot deadlock
	sort.Slice(classIDs, func(i, j int) bool { return classIDs[i].String() < classIDs[j].String() })

	for _, classID := range classIDs {
		err = ranking.Lock(tx, classID)
		if err != nil {
			return nil, nil, err
		}
	}

	avatars, err := takeAddresses(tx.Unscoped().Model(&entity.Student{}).Where("wali_id = ?", wali.ID), "avatar_url")
	if err != nil {
		return nil, nil, err
	}

	children := tx.Unscoped().Model(&entity.Student{}).Select("id").Where("wali_id = ?", wali.ID)

	photos, err := takeAddresses(tx.Model(&entity.TrashScan{}).Where("student_id IN (?)", children), "photo_url")
	if err != nil {
		return nil, nil, err
	}

	err = tx.Where("wali_id = ?", wali.ID).Delete(&entity.Student{}).Error
	if err != nil {
		return nil, nil, err
	}

	for _, classID := range classIDs {
		err = ranking.Recompute(tx, classID)
		if err != nil {
			return nil, nil, err
		}
	}

	err = tx.Where("id = ?", wali.ID).Delete(&entity.Wali{}).Error
	if err != nil {
		return nil, nil, err
	}

	return classIDs, append(avatars, photos...), nil
}
