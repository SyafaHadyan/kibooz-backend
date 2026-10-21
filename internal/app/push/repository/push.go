// Package repository handles the database operations of the push tokens
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
)

// MaxTokensPerUser is how many devices of one account keep a token. A phone that is replaced leaves its token behind until
// Firebase reports it as unregistered, so the oldest ones are dropped when an account goes over.
const MaxTokensPerUser = 10

// ErrAccountGone is what SaveToken returns when the account was deleted, because a token must not outlive its account
var ErrAccountGone = errors.New("the account is deleted")

type PushDBItf interface {
	// SaveToken stores the token for the account, or moves it to the account when another one had it, and keeps only the
	// newest MaxTokensPerUser tokens of the account. It returns ErrAccountGone for an account that is deleted.
	SaveToken(ctx context.Context, userID uuid.UUID, token string, platform string, now time.Time) error
	// DeleteToken removes the token of the account, and a token that is not there or belongs to somebody else is left alone
	DeleteToken(ctx context.Context, userID uuid.UUID, token string) error
}

type PushDB struct {
	db *gorm.DB
}

func NewPushDB(db *gorm.DB) PushDBItf {
	return &PushDB{db: db}
}

func (r *PushDB) SaveToken(ctx context.Context, userID uuid.UUID, token string, platform string, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// the user row first, the same order as the deletion of an account, so a token cannot be added after the deletion
		// removed the tokens of the account
		var active []uuid.UUID

		err := tx.Raw("SELECT id FROM users WHERE id = ? AND deleted_at IS NULL FOR NO KEY UPDATE", userID).Scan(&active).Error
		if err != nil {
			return err
		}

		if len(active) == 0 {
			return ErrAccountGone
		}

		err = tx.Exec(
			`INSERT INTO push_tokens (id, user_id, token, platform, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (token) DO UPDATE SET user_id = EXCLUDED.user_id, platform = EXCLUDED.platform, updated_at = EXCLUDED.updated_at`,
			uuid.New(), userID, token, platform, now, now,
		).Error
		if err != nil {
			return err
		}

		return tx.Exec(
			`DELETE FROM push_tokens WHERE id IN (
				SELECT id FROM push_tokens WHERE user_id = ? ORDER BY updated_at DESC, id OFFSET ?
			)`,
			userID, MaxTokensPerUser,
		).Error
	})
}

func (r *PushDB) DeleteToken(ctx context.Context, userID uuid.UUID, token string) error {
	return r.db.WithContext(ctx).Where("user_id = ? AND token = ?", userID, token).Delete(&entity.PushToken{}).Error
}
