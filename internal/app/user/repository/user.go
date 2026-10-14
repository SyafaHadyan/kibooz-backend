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
	"github.com/SyafaHadyan/kibooz-backend/internal/sessions"
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
	// addresses of their files. fileKey turns an address into the object key of a file to delete, and false for an
	// address that is not ours. The keys are queued for cleanup in the same transaction, so a file is never forgotten
	// after the address that pointed at it is gone. It returns the classes whose ranking changed and the queued keys,
	// so the caller can delete the files and then take them off the queue.
	SoftDeleteAccount(ctx context.Context, userID uuid.UUID, role constants.Role, fileKey FileKeyFunc) (DeletedAccount, error)
	// UnqueueFileCleanups takes keys off the cleanup queue once their files are gone from the bucket
	UnqueueFileCleanups(ctx context.Context, keys []string) error
}

// FileKeyFunc turns the public address of a file into its object key
type FileKeyFunc func(url string) (key string, ok bool)

// DeletedAccount is what a deleted account leaves behind for the caller to clean up
type DeletedAccount struct {
	ClassIDs []uuid.UUID
	// FileKeys are the files to delete from the bucket, they are already on the cleanup queue
	FileKeys []string
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

func (r *UserDB) SoftDeleteAccount(
	ctx context.Context, userID uuid.UUID, role constants.Role, fileKey FileKeyFunc,
) (DeletedAccount, error) {
	var deleted DeletedAccount

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// a retried transaction starts from nothing
		deleted = DeletedAccount{}

		var urls []string

		// refresh tokens have no soft delete, removing them ends every session. This comes first because it waits for a
		// refresh that is under way, and nothing it waits for may hold a lock that the rest of the deletion needs.
		err := sessions.EndUser(tx, userID)
		if err != nil {
			return err
		}

		switch role {
		case constants.RoleWali:
			deleted.ClassIDs, err = softDeleteWali(tx, userID, &urls)
		case constants.RoleGuru:
			err = tx.Where("user_id = ?", userID).Delete(&entity.Guru{}).Error
		}

		if err != nil {
			return err
		}

		err = takeAddresses(&urls, tx.Model(&entity.User{}).Where("id = ?", userID), "avatar_url")
		if err == nil {
			err = tx.Where("id = ?", userID).Delete(&entity.User{}).Error
		}

		if err != nil {
			return err
		}

		deleted.FileKeys = keysOf(urls, fileKey)

		return queueKeys(tx, deleted.FileKeys)
	})
	if err != nil {
		return DeletedAccount{}, err
	}

	return deleted, nil
}

// takeAddresses adds the file addresses that rows hold in column to files and clears them, so no row points at a file
// that is about to be deleted
func takeAddresses(files *[]string, rows *gorm.DB, column string) error {
	var urls []string

	err := rows.Session(&gorm.Session{}).Where(column+" IS NOT NULL AND "+column+" <> ''").Pluck(column, &urls).Error
	if err != nil || len(urls) == 0 {
		return err
	}

	*files = append(*files, urls...)

	return rows.Session(&gorm.Session{}).Update(column, nil).Error
}

// softDeleteWali hides the parent and their children, then renumbers the ranking of the affected classes. It also
// adds the addresses of the avatars of the children and of the photos of their trash scans to files and clears them.
func softDeleteWali(tx *gorm.DB, userID uuid.UUID, files *[]string) ([]uuid.UUID, error) {
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

	children := tx.Unscoped().Model(&entity.Student{}).Select("id").Where("wali_id = ?", wali.ID)

	err = takeAddresses(files, tx.Unscoped().Model(&entity.Student{}).Where("wali_id = ?", wali.ID), "avatar_url")
	if err == nil {
		err = takeAddresses(files, tx.Model(&entity.TrashScan{}).Where("student_id IN (?)", children), "photo_url")
	}

	if err == nil {
		err = tx.Where("wali_id = ?", wali.ID).Delete(&entity.Student{}).Error
	}

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

// keysOf maps the addresses to object keys and leaves out the ones that are not files of this storage
func keysOf(urls []string, fileKey FileKeyFunc) []string {
	keys := make([]string, 0, len(urls))

	for _, url := range urls {
		if key, ok := fileKey(url); ok {
			keys = append(keys, key)
		}
	}

	return keys
}

// queueKeys adds the keys to the cleanup queue and keeps the ones that are already there as they are
func queueKeys(tx *gorm.DB, keys []string) error {
	if len(keys) == 0 {
		return nil
	}

	rows := make([]fileCleanup, 0, len(keys))
	for _, key := range keys {
		rows = append(rows, fileCleanup{ObjectKey: key})
	}

	// attempts and queued_at are left to the defaults of the table
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

func (r *UserDB) UnqueueFileCleanups(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}

	return r.db.WithContext(ctx).Where("object_key IN ?", keys).Delete(&fileCleanup{}).Error
}

// fileCleanup is a row of the queue of files that still have to be removed from the bucket
type fileCleanup struct {
	ObjectKey string `gorm:"primaryKey"`
}

func (fileCleanup) TableName() string { return "file_cleanups" }
