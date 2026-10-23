// Package repository handles the profile and avatar database operations
package repository

import (
	"context"
	"errors"
	"slices"
	"sort"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/classteam"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/filecleanup"
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
	// UserAvatar returns the avatar URL of an active account, empty when it has none, and false when there is no such account
	UserAvatar(ctx context.Context, userID uuid.UUID) (url string, found bool, err error)
	// StudentAvatar returns the avatar URL of an active child of the parent, empty when it has none, and false when there is no such child
	StudentAvatar(ctx context.Context, waliID uuid.UUID, studentID uuid.UUID) (url string, found bool, err error)
	// FindUserByID returns nil when the account does not exist or is already deleted
	FindUserByID(ctx context.Context, id uuid.UUID) (*entity.User, error)
	// SoftDeleteAccount hides the account with its profile and children, revokes every session and clears the
	// addresses of their files. fileKey turns an address into the object key of a file to delete, and false for an
	// address that is not ours. The keys are queued for cleanup in the same transaction, so a file is never forgotten
	// after the address that pointed at it is gone. It returns the classes whose ranking changed and the queued keys,
	// so the caller can delete the files and then take them off the queue. The deletion only goes through while the stored
	// hash is still verifiedHash, the one the caller checked the password against, and otherwise it returns
	// apperror.ErrPasswordIncorrect, so a password that was replaced in between does not confirm a deletion.
	SoftDeleteAccount(
		ctx context.Context, userID uuid.UUID, role constants.Role, verifiedHash string, successorEmail string, fileKey FileKeyFunc,
	) (DeletedAccount, error)
	// UpdatePassword stores newHash and ends every session of the account, but only while the stored hash is still
	// oldHash, so a change that was made in between is not overwritten. It returns false when nothing was changed.
	UpdatePassword(ctx context.Context, userID uuid.UUID, oldHash string, newHash string) (bool, error)
	// UnqueueFileCleanups takes keys off the cleanup queue once their files are gone from the bucket
	UnqueueFileCleanups(ctx context.Context, keys []string) error
}

// FileKeyFunc turns the public address of a file into its object key
type FileKeyFunc = filecleanup.KeyFunc

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

func (r *UserDB) UserAvatar(ctx context.Context, userID uuid.UUID) (string, bool, error) {
	var urls []*string

	err := r.db.WithContext(ctx).Model(&entity.User{}).Where("id = ?", userID).Pluck("avatar_url", &urls).Error
	if err != nil || len(urls) == 0 {
		return "", false, err
	}

	return derefURL(urls[0]), true, nil
}

func (r *UserDB) StudentAvatar(ctx context.Context, waliID uuid.UUID, studentID uuid.UUID) (string, bool, error) {
	var urls []*string

	err := r.db.WithContext(ctx).Model(&entity.Student{}).Where("id = ? AND wali_id = ?", studentID, waliID).Pluck("avatar_url", &urls).Error
	if err != nil || len(urls) == 0 {
		return "", false, err
	}

	return derefURL(urls[0]), true, nil
}

func derefURL(url *string) string {
	if url == nil {
		return ""
	}

	return *url
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
	ctx context.Context, userID uuid.UUID, role constants.Role, verifiedHash string, successorEmail string, fileKey FileKeyFunc,
) (DeletedAccount, error) {
	var deleted DeletedAccount

	err := ranking.Retry(func() error {
		return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// a retried transaction starts from nothing
			deleted = DeletedAccount{}

			var urls []string

			// The user row comes first, so a sign in that is under way finishes before the sessions are ended and one that
			// starts later finds the account gone. A password change takes the locks in the same order.
			err := sessions.LockUser(tx, userID)
			if err != nil {
				return err
			}

			// The password was checked before this transaction, so it only counts while it is still the password of the account.
			// A change that committed in between, or a deletion that came first, ends the request without deleting anything.
			var current int64

			err = tx.Model(&entity.User{}).Where("id = ? AND password_hash = ?", userID, verifiedHash).Count(&current).Error
			if err == nil && current == 0 {
				err = apperror.ErrPasswordIncorrect
			}

			if err != nil {
				return err
			}

			// refresh tokens have no soft delete, removing them ends every session. This comes before the rest because it
			// waits for a refresh that is under way, and nothing it waits for may hold a lock that the rest of the deletion needs.
			err = sessions.EndUser(tx, userID)
			if err != nil {
				return err
			}

			// a phone that stays signed in on a deleted account must not be woken for it, and the tokens are personal data of the account
			err = tx.Exec("DELETE FROM push_tokens WHERE user_id = ?", userID).Error
			if err != nil {
				return err
			}

			switch role {
			case constants.RoleWali:
				deleted.ClassIDs, err = softDeleteWali(tx, userID, &urls)
			case constants.RoleGuru:
				// every class goes on with another teacher before the account is hidden, or the deletion is refused
				err = classteam.Leave(tx, userID, successorEmail)
				if err == nil {
					err = tx.Where("user_id = ?", userID).Delete(&entity.Guru{}).Error
				}
			}

			if err != nil {
				return err
			}

			err = filecleanup.TakeAddresses(&urls, tx.Model(&entity.User{}).Where("id = ?", userID), "avatar_url")
			if err == nil {
				err = tx.Where("id = ?", userID).Delete(&entity.User{}).Error
			}

			if err != nil {
				return err
			}

			deleted.FileKeys = filecleanup.KeysOf(urls, fileKey)

			return filecleanup.Queue(tx, deleted.FileKeys)

		})
	})
	if err != nil {
		return DeletedAccount{}, err
	}

	return deleted, nil
}

// softDeleteWali hides the parent and their children, then renumbers the ranking of the affected classes. It also
// adds the addresses of the avatars of the children and of the photos of their trash scans to files and clears them.
func softDeleteWali(tx *gorm.DB, userID uuid.UUID, files *[]string) ([]uuid.UUID, error) {
	var wali entity.Wali

	// The parent row is locked before the class locks below, in the order that adding a child takes them. A child that is
	// being added then either finishes first and is deleted with the others, or waits and finds the parent gone.
	err := tx.Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
		Select("id").Where("user_id = ?", userID).Take(&wali).Error
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

	// a move may have taken a child to another class while this waited for the locks, then the deletion starts again
	var current []uuid.UUID

	err = tx.Model(&entity.Student{}).Where("wali_id = ?", wali.ID).Distinct().Pluck("class_id", &current).Error
	if err != nil {
		return nil, err
	}

	sort.Slice(current, func(i, j int) bool { return current[i].String() < current[j].String() })

	if !slices.Equal(classIDs, current) {
		return nil, ranking.ErrMoved
	}

	children := tx.Unscoped().Model(&entity.Student{}).Select("id").Where("wali_id = ?", wali.ID)

	err = filecleanup.TakeAddresses(files, tx.Unscoped().Model(&entity.Student{}).Where("wali_id = ?", wali.ID), "avatar_url")
	if err == nil {
		err = filecleanup.TakeAddresses(files, tx.Model(&entity.TrashScan{}).Where("student_id IN (?)", children), "photo_url")
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

// errPasswordStale rolls the transaction of UpdatePassword back when the stored hash is no longer the one that was checked
var errPasswordStale = errors.New("password hash changed")

func (r *UserDB) UpdatePassword(ctx context.Context, userID uuid.UUID, oldHash string, newHash string) (bool, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// The same order of locks as SoftDeleteAccount, the user row, then the sessions. A sign in that checked the old
		// password and has not stored its session yet either finishes first, and its session is ended here, or waits
		// until this change is done and is refused.
		err := sessions.LockUser(tx, userID)
		if err != nil {
			return err
		}

		err = sessions.EndUser(tx, userID)
		if err != nil {
			return err
		}

		res := tx.Model(&entity.User{}).Where("id = ? AND password_hash = ?", userID, oldHash).Update("password_hash", newHash)
		if res.Error != nil {
			return res.Error
		}

		// nothing was changed, so the rollback also keeps the sessions that were just ended
		if res.RowsAffected == 0 {
			return errPasswordStale
		}

		return nil
	})
	if errors.Is(err, errPasswordStale) {
		return false, nil
	}

	return err == nil, err
}
