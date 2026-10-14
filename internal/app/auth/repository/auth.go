// Package repository handles the auth related database operations
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/classcode"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/ranking"
	"github.com/SyafaHadyan/kibooz-backend/internal/sessions"
)

const uniqueViolation = "23505"

type AuthDBItf interface {
	FindUserByEmail(ctx context.Context, email string) (*entity.User, error)
	FindUserByID(ctx context.Context, id uuid.UUID) (*entity.User, error)
	// ClassCodeExists tells whether a class uses joinCode, so a registration with a code that nobody has can be refused
	// before it spends the time of a password hash
	ClassCodeExists(ctx context.Context, joinCode string) (bool, error)
	// CreateGuru stores the user, the guru profile, a fresh class, the teaching link and the first refresh token atomically,
	// so an account never exists without the session it was registered with
	CreateGuru(ctx context.Context, user *entity.User, guru *entity.Guru, class *entity.Class, session *entity.RefreshToken) error
	// CreateWali stores the user, the wali profile, the child inside the class that owns joinCode and the first refresh token
	CreateWali(
		ctx context.Context, user *entity.User, wali *entity.Wali, joinCode string, student *entity.Student, session *entity.RefreshToken,
	) (classID uuid.UUID, err error)
	CreateRefreshToken(ctx context.Context, token *entity.RefreshToken) error
	// RotateRefreshToken uses a valid token and stores its replacement in the same session atomically. The row of the
	// used token stays, so a replay can be recognized. A token that is unknown, expired or used a moment ago is only
	// refused, and one that was used earlier ends its whole session.
	RotateRefreshToken(
		ctx context.Context, tokenHash string, now time.Time, next *entity.RefreshToken, policy RotationPolicy,
	) (Rotation, error)
	// DeleteRefreshToken ends the whole session that the token belongs to
	DeleteRefreshToken(ctx context.Context, tokenHash string) error
}

// RotationPolicy holds the limits of swapping a refresh token
type RotationPolicy struct {
	// SessionMax is the longest a session can last, however often its token is swapped
	SessionMax time.Duration
	// ReuseGrace is how long after its use a token can be shown again without being taken for a theft, because a client
	// that lost the answer retries with the same token
	ReuseGrace time.Duration
}

// Rotation is the outcome of swapping a refresh token
type Rotation struct {
	// User is nil when the token cannot be used
	User *entity.User
	// Reused tells that the token had been used before and its whole session was ended, UserID is the owner
	Reused bool
	UserID uuid.UUID
}

// usedRetention is how long the row of a used token stays to recognize a replay
const usedRetention = 7 * 24 * time.Hour

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

func (r *AuthDB) ClassCodeExists(ctx context.Context, joinCode string) (bool, error) {
	var found int64

	err := r.db.WithContext(ctx).Model(&entity.Class{}).Where("join_code = ?", joinCode).Limit(1).Count(&found).Error

	return found > 0, err
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

// createAll inserts the rows in order and stops at the first one that fails
func createAll(tx *gorm.DB, rows ...any) error {
	for _, row := range rows {
		err := tx.Create(row).Error
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *AuthDB) CreateGuru(
	ctx context.Context, user *entity.User, guru *entity.Guru, class *entity.Class, session *entity.RefreshToken,
) error {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Create(user).Error
		if err != nil {
			return err
		}

		err = tx.Create(guru).Error
		if err != nil {
			return err
		}

		err = classcode.Create(tx, class)
		if err != nil {
			return err
		}

		// a new account has no earlier tokens, so there is nothing to purge
		return createAll(tx, &entity.ClassTeacher{ClassID: class.ID, GuruID: guru.ID}, session)
	})

	return mapUniqueViolation(err)
}

func (r *AuthDB) CreateWali(
	ctx context.Context, user *entity.User, wali *entity.Wali, joinCode string, student *entity.Student, session *entity.RefreshToken,
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

		// a class whose teachers were all deleted keeps its code but must not take in new children
		var teachers int64

		err = tx.Table("class_teachers AS ct").
			Joins("JOIN gurus AS g ON g.id = ct.guru_id AND g.deleted_at IS NULL").
			Where("ct.class_id = ?", class.ID).
			Count(&teachers).Error
		if err != nil {
			return err
		}

		if teachers == 0 {
			return apperror.ErrClassNoTeacher
		}

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

		err = createAll(tx, student, session)
		if err != nil {
			return err
		}

		return ranking.Recompute(tx, class.ID)
	})

	return classID, mapUniqueViolation(err)
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
	ctx context.Context, tokenHash string, now time.Time, next *entity.RefreshToken, policy RotationPolicy,
) (Rotation, error) {
	var rotation Rotation

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// a retried transaction starts from nothing
		rotation = Rotation{}

		// a single UPDATE guarantees one use even when two requests race, whatever Redis says
		var used []sessionRow

		err := tx.Raw(
			"UPDATE refresh_tokens SET used_at = ? WHERE token_hash = ? AND used_at IS NULL AND expires_at > ? "+
				"RETURNING user_id, family_id, session_started_at",
			now, tokenHash, now,
		).Scan(&used).Error
		if err != nil {
			return err
		}

		if len(used) == 0 {
			rotation.Reused, rotation.UserID, err = endReusedSession(tx, tokenHash, now, policy.ReuseGrace)

			return err
		}

		rotation.User, err = continueSession(tx, used[0], now, next, policy)

		return err
	})
	if err != nil {
		return Rotation{}, err
	}

	return rotation, nil
}

// sessionRow is what a refresh token says about its session
type sessionRow struct {
	UserID           uuid.UUID
	FamilyID         uuid.UUID
	SessionStartedAt time.Time
	UsedAt           *time.Time
}

// continueSession stores the next token of the session, capped at the longest lifetime of a session. It returns nil
// when the account is gone or the session has reached its longest lifetime.
func continueSession(tx *gorm.DB, used sessionRow, now time.Time, next *entity.RefreshToken, policy RotationPolicy) (*entity.User, error) {
	var found entity.User

	err := tx.Where("id = ?", used.UserID).Take(&found).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// the account was deleted, the token stays used and the caller gets no session
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	next.UserID = found.ID
	next.FamilyID = used.FamilyID
	next.SessionStartedAt = used.SessionStartedAt

	if limit := used.SessionStartedAt.Add(policy.SessionMax); next.ExpiresAt.After(limit) {
		next.ExpiresAt = limit
	}

	if !next.ExpiresAt.After(now) {
		// the session reached its longest lifetime, so it ends here and the user signs in again
		return nil, sessions.EndFamily(tx, used.FamilyID)
	}

	err = tx.Create(next).Error
	if err != nil {
		return nil, err
	}

	return &found, purgeExpiredTokens(tx, found.ID, now)
}

// endReusedSession ends the session of a token that was used before and is shown again. A token that is unknown,
// expired, or was used within the grace period is only refused.
func endReusedSession(tx *gorm.DB, tokenHash string, now time.Time, grace time.Duration) (bool, uuid.UUID, error) {
	var seen []sessionRow

	err := tx.Raw("SELECT user_id, family_id, used_at FROM refresh_tokens WHERE token_hash = ?", tokenHash).Scan(&seen).Error
	if err != nil || len(seen) == 0 || seen[0].UsedAt == nil || now.Sub(*seen[0].UsedAt) <= grace {
		return false, uuid.Nil, err
	}

	return true, seen[0].UserID, sessions.EndFamily(tx, seen[0].FamilyID)
}

func (r *AuthDB) DeleteRefreshToken(ctx context.Context, tokenHash string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var families []uuid.UUID

		err := tx.Raw("SELECT family_id FROM refresh_tokens WHERE token_hash = ?", tokenHash).Scan(&families).Error
		if err != nil || len(families) == 0 {
			return err
		}

		return sessions.EndFamily(tx, families[0])
	})
}

// purgeExpiredTokens keeps the table small without a background job by cleaning one user at a time. It removes the
// expired tokens that were never used and the used ones that are too old to recognize a replay. A used token stays for
// the whole retention even when it has expired, because showing it again must still end the session.
//
// A row that another transaction has locked is left for the next time. The cleanup runs while the refresh that called it
// holds the lock of the token it used, so waiting here could close a circle with a deletion that holds the lock of an
// expired token and waits for that refresh. See package sessions.
func purgeExpiredTokens(tx *gorm.DB, userID uuid.UUID, now time.Time) error {
	return tx.Exec(
		"DELETE FROM refresh_tokens WHERE id IN (SELECT id FROM refresh_tokens WHERE user_id = ? AND "+
			"((used_at IS NULL AND expires_at <= ?) OR (used_at IS NOT NULL AND used_at <= ?)) FOR UPDATE SKIP LOCKED)",
		userID, now, now.Add(-usedRetention),
	).Error
}
