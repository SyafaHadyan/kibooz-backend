// Package repository handles the auth related database operations
package repository

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/ranking"
)

const (
	joinCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	joinCodeLength   = 6
	joinCodeRetries  = 8

	uniqueViolation = "23505"
)

type AuthDBItf interface {
	FindUserByEmail(ctx context.Context, email string) (*entity.User, error)
	FindUserByID(ctx context.Context, id uuid.UUID) (*entity.User, error)
	// CreateGuru stores the user, the guru profile, a fresh class and the teaching link atomically
	CreateGuru(ctx context.Context, user *entity.User, guru *entity.Guru, class *entity.Class) error
	// CreateWali stores the user, the wali profile and the child inside the class that owns joinCode
	CreateWali(ctx context.Context, user *entity.User, wali *entity.Wali, joinCode string, student *entity.Student) (classID uuid.UUID, err error)
	CreateRefreshToken(ctx context.Context, token *entity.RefreshToken) error
	// RotateRefreshToken consumes a valid token and stores its replacement atomically.
	// It returns nil without an error when the token is unknown, already used or expired.
	RotateRefreshToken(ctx context.Context, tokenHash string, now time.Time, next *entity.RefreshToken) (*entity.User, error)
	DeleteRefreshToken(ctx context.Context, tokenHash string) error
}

type AuthDB struct {
	db *gorm.DB
}

func NewAuthDB(db *gorm.DB) AuthDBItf {
	return &AuthDB{db: db}
}

func (r *AuthDB) FindUserByEmail(ctx context.Context, email string) (*entity.User, error) {
	var user entity.User

	err := r.db.WithContext(ctx).Where("email = ?", email).Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &user, nil
}

func (r *AuthDB) FindUserByID(ctx context.Context, id uuid.UUID) (*entity.User, error) {
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

func (r *AuthDB) CreateGuru(ctx context.Context, user *entity.User, guru *entity.Guru, class *entity.Class) error {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Create(user).Error
		if err != nil {
			return err
		}

		err = tx.Create(guru).Error
		if err != nil {
			return err
		}

		err = createClassWithCode(tx, class)
		if err != nil {
			return err
		}

		return tx.Create(&entity.ClassTeacher{ClassID: class.ID, GuruID: guru.ID}).Error
	})

	return mapUniqueViolation(err)
}

func (r *AuthDB) CreateWali(
	ctx context.Context, user *entity.User, wali *entity.Wali, joinCode string, student *entity.Student,
) (uuid.UUID, error) {
	var classID uuid.UUID

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var class entity.Class

		err := tx.Where("join_code = ?", joinCode).Take(&class).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperror.ErrClassCodeNotFound
		}

		if err != nil {
			return err
		}

		classID = class.ID

		err = tx.Create(user).Error
		if err != nil {
			return err
		}

		err = tx.Create(wali).Error
		if err != nil {
			return err
		}

		student.WaliID = wali.ID
		student.ClassID = class.ID

		err = tx.Create(student).Error
		if err != nil {
			return err
		}

		return ranking.Recompute(tx, class.ID)
	})

	return classID, mapUniqueViolation(err)
}

// createClassWithCode retries with a new random code when it collides with an existing class
func createClassWithCode(tx *gorm.DB, class *entity.Class) error {
	for range joinCodeRetries {
		code, err := newJoinCode()
		if err != nil {
			return err
		}

		class.JoinCode = code

		// a savepoint keeps the outer transaction usable after a unique violation
		err = tx.Transaction(func(inner *gorm.DB) error {
			return inner.Create(class).Error
		})
		if err == nil {
			return nil
		}

		if !isUniqueViolation(err, "classes_join_code_key") {
			return err
		}
	}

	return errors.New("failed to generate a unique class code")
}

func newJoinCode() (string, error) {
	code := make([]byte, joinCodeLength)
	limit := big.NewInt(int64(len(joinCodeAlphabet)))

	for i := range code {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", err
		}

		code[i] = joinCodeAlphabet[n.Int64()]
	}

	return string(code), nil
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == constraint
}

func mapUniqueViolation(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != uniqueViolation {
		return err
	}

	switch pgErr.ConstraintName {
	case "users_email_key":
		return apperror.ErrEmailTaken
	case "gurus_nip_key":
		return apperror.ErrNIPTaken
	case "students_nisn_key":
		return apperror.ErrNISNTaken
	default:
		return err
	}
}

func (r *AuthDB) CreateRefreshToken(ctx context.Context, token *entity.RefreshToken) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Create(token).Error
		if err != nil {
			return err
		}

		return purgeExpiredTokens(tx, token.UserID, token.CreatedAt)
	})
}

func (r *AuthDB) RotateRefreshToken(
	ctx context.Context, tokenHash string, now time.Time, next *entity.RefreshToken,
) (*entity.User, error) {
	var user *entity.User

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// a single DELETE guarantees one use even when two requests race, whatever Redis says
		var userIDs []uuid.UUID

		err := tx.Raw(
			"DELETE FROM refresh_tokens WHERE token_hash = ? AND expires_at > ? RETURNING user_id",
			tokenHash, now,
		).Scan(&userIDs).Error
		if err != nil {
			return err
		}

		if len(userIDs) == 0 {
			return nil
		}

		var found entity.User

		err = tx.Where("id = ?", userIDs[0]).Take(&found).Error
		if err != nil {
			return err
		}

		next.UserID = found.ID

		err = tx.Create(next).Error
		if err != nil {
			return err
		}

		user = &found

		return purgeExpiredTokens(tx, found.ID, now)
	})
	if err != nil {
		return nil, err
	}

	return user, nil
}

func (r *AuthDB) DeleteRefreshToken(ctx context.Context, tokenHash string) error {
	return r.db.WithContext(ctx).Where("token_hash = ?", tokenHash).Delete(&entity.RefreshToken{}).Error
}

// purgeExpiredTokens keeps the table small without a background job by cleaning one user at a time
func purgeExpiredTokens(tx *gorm.DB, userID uuid.UUID, now time.Time) error {
	return tx.Where("user_id = ? AND expires_at <= ?", userID, now).Delete(&entity.RefreshToken{}).Error
}
