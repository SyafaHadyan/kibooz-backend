// Package repository handles the auth related database operations
package repository

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/classcode"
	"github.com/SyafaHadyan/kibooz-backend/internal/classteam"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/nisn"
	"github.com/SyafaHadyan/kibooz-backend/internal/ranking"
	"github.com/SyafaHadyan/kibooz-backend/internal/sessions"
)

const uniqueViolation = "23505"

type AuthDBItf interface {
	FindUserByEmail(ctx context.Context, email string) (*entity.User, error)
	FindUserByID(ctx context.Context, id uuid.UUID) (*entity.User, error)
	FindUserByGoogleSub(ctx context.Context, sub string) (*entity.User, error)
	LinkGoogleSub(ctx context.Context, userID uuid.UUID, sub string) error
	// ClassCodeExists tells whether a class uses joinCode, so a registration with a code that nobody has can be refused
	// before it spends the time of a password hash
	ClassCodeExists(ctx context.Context, joinCode string) (bool, error)
	// RegistrationTaken returns the conflict error of the first of the email, the NIP and the NISN that an active account already
	// uses, and nil when none is. An empty NIP or NISN is not looked up. It lets a registration that cannot succeed be refused
	// before the password hash, and the unique indexes still decide when two registrations race.
	RegistrationTaken(ctx context.Context, email string, nip string, nisn string) error
	// CreateGuru stores the user, the guru profile, a fresh class, the teaching link and the first refresh token atomically,
	// so an account never exists without the session it was registered with
	CreateGuru(ctx context.Context, user *entity.User, guru *entity.Guru, class *entity.Class, session *entity.RefreshToken) error
	// CreateWali stores the user, the wali profile, the child inside the class that owns joinCode and the first refresh token
	CreateWali(
		ctx context.Context, user *entity.User, wali *entity.Wali, joinCode string, student *entity.Student, session *entity.RefreshToken,
	) (classID uuid.UUID, err error)
	// CreateRefreshToken stores the first token of a session after a sign in. verifiedHash is the password hash that the
	// sign in checked, and a user whose hash is another one by now, or who is gone, gets ErrCredentialsChanged and no session.
	CreateRefreshToken(ctx context.Context, token *entity.RefreshToken, verifiedHash string, sessionMax time.Duration) error
	// RotateRefreshToken uses a valid token and stores its replacement in the same session atomically. The row of the
	// used token stays, so a replay can be recognized. A token that is unknown or expired is only refused, one that
	// was used within the leeway gets a new token of the same session, and one that was used earlier ends its whole session.
	RotateRefreshToken(
		ctx context.Context, tokenHash string, now time.Time, next *entity.RefreshToken, policy RotationPolicy,
	) (Rotation, error)
	// DeleteRefreshToken ends the whole session that the token belongs to
	DeleteRefreshToken(ctx context.Context, tokenHash string) error
	// StoreVerification keeps the hash of a new code for an account that waits for its email, and replaces the live one. It
	// refuses with the time to wait when the last code is newer than the cooldown or the codes of the hour are used up, and
	// then changes nothing.
	StoreVerification(ctx context.Context, userID uuid.UUID, codeHash string, now time.Time, rule SendRule) (wait time.Duration, err error)
	// CheckVerification compares the hash with the live code of the account. A code that is wrong counts as an attempt, and one
	// that is expired or used up is only refused. A code that matches ends the wait of the account in the same transaction, so
	// it works once.
	CheckVerification(ctx context.Context, userID uuid.UUID, codeHash string, now time.Time, maxAttempts int) (matched bool, err error)
	// ReleaseVerification gives back the wait and the send of the hour of a code whose mail was not taken by the mail service, so the
	// person can ask again at once. It only touches the row of that code, a newer code is left alone.
	ReleaseVerification(ctx context.Context, userID uuid.UUID, codeHash string, now time.Time, cooldown time.Duration) error
}

// SendRule holds the limits of the codes that one account can have sent
type SendRule struct {
	// TTL is how long the new code works
	TTL time.Duration
	// Cooldown is the shortest time between two codes
	Cooldown time.Duration
	// PerHour is the most codes in an hour, the new one included
	PerHour int
}

// RotationPolicy holds the limits of swapping a refresh token
type RotationPolicy struct {
	// SessionMax is the longest a session can last, however often its token is swapped
	SessionMax time.Duration
	// ReuseLeeway is how long after its use a token can be shown again and get a new token of the same session, because a
	// client that lost the answer retries with the same token. Later it is taken for a theft and the session ends.
	ReuseLeeway time.Duration
}

// Rotation is the outcome of swapping a refresh token
type Rotation struct {
	// User is nil when the token cannot be used
	User *entity.User
	// Reused tells that the token had been used before and its whole session was ended, UserID is the owner
	Reused bool
	UserID uuid.UUID
	// Replayed tells that the token had been used a moment before, within the leeway, and User holds the new token of the same
	// session that it got
	Replayed bool
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

func (r *AuthDB) ClassCodeExists(ctx context.Context, joinCode string) (bool, error) {
	var found int64

	err := r.db.WithContext(ctx).Model(&entity.Class{}).
		Where("join_code = ? AND archived_at IS NULL", joinCode).Limit(1).Count(&found).Error

	return found > 0, err
}

func (r *AuthDB) RegistrationTaken(ctx context.Context, email string, nip string, nisn string) error {
	checks := []struct {
		value string
		query string
		err   error
	}{
		{email, "SELECT 1 FROM users WHERE email = ? AND deleted_at IS NULL LIMIT 1", apperror.ErrEmailTaken},
		{nip, "SELECT 1 FROM gurus WHERE nip = ? AND deleted_at IS NULL LIMIT 1", apperror.ErrNIPTaken},
		{nisn, "SELECT 1 FROM students WHERE nisn = ? AND nisn_verified_at IS NOT NULL AND deleted_at IS NULL LIMIT 1", apperror.ErrNISNTaken},
	}

	for _, check := range checks {
		if check.value == "" {
			continue
		}

		var found []int

		err := r.db.WithContext(ctx).Raw(check.query, check.value).Scan(&found).Error
		if err != nil {
			return err
		}

		if len(found) > 0 {
			return check.err
		}
	}

	return nil
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

// createSession stores the first session of an account, and does nothing for an account that waits for the code of its email
// and gets its session when the code is right
func createSession(tx *gorm.DB, session *entity.RefreshToken) error {
	if session == nil {
		return nil
	}

	return tx.Create(session).Error
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

		err = createAll(tx, &entity.ClassTeacher{ClassID: class.ID, GuruID: guru.ID, IsOwner: true})
		if err != nil {
			return err
		}

		// a new account has no earlier tokens, so there is nothing to purge
		return createSession(tx, session)
	})

	return mapUniqueViolation(err)
}

func (r *AuthDB) CreateWali(
	ctx context.Context, user *entity.User, wali *entity.Wali, joinCode string, student *entity.Student, session *entity.RefreshToken,
) (uuid.UUID, error) {
	var classID uuid.UUID

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var class entity.Class

		err := tx.Where("join_code = ? AND archived_at IS NULL", joinCode).Take(&class).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperror.ErrClassCodeNotFound
		}

		if err != nil {
			return err
		}

		classID = class.ID

		// the teachers of the class are counted under the lock that a teacher takes when leaving, so a child cannot join
		// while the class is being archived
		err = classteam.LockOpen(tx, class.ID, joinCode)
		if err != nil {
			return err
		}

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

		err = classteam.CheckRoom(tx, class.ID, constants.MaxChildrenPerClass)
		if err != nil {
			return err
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

		// the lock of the class comes before the lock of the NISN, the order that a verification and a change of the number take them in
		err = ranking.Lock(tx, class.ID)
		if err != nil {
			return err
		}

		// only a verified child holds a NISN alone, so a number that an unverified child holds may be registered again
		err = nisn.Lock(tx, student.NISN)
		if err != nil {
			return err
		}

		err = nisn.RefuseVerifiedHolder(tx, student.NISN, student.ID)
		if err != nil {
			return err
		}

		err = createAll(tx, student)
		if err != nil {
			return err
		}

		err = createSession(tx, session)
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
	case "idx_users_google_sub":
		return apperror.ErrGoogleAlreadyRegistered
	case "gurus_nip_key":
		return apperror.ErrNIPTaken
	case "students_nisn_verified_key":
		return apperror.ErrNISNTaken
	default:
		return err
	}
}

// ErrCredentialsChanged means that the password of the user changed, or the account was deleted, after it was checked
var ErrCredentialsChanged = errors.New("credentials changed")

func (r *AuthDB) CreateRefreshToken(
	ctx context.Context, token *entity.RefreshToken, verifiedHash string, sessionMax time.Duration,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// A password change ends the sessions and then updates the hash, and checking a password takes longer than that,
		// so a sign in that was checked against the old hash could store its session after the change. Reading the hash
		// under a share lock waits for a change that has updated the row and still sees its new value, and a change that
		// has not reached the row waits for this transaction. The window that is left is the time between the two
		// statements of the change.
		var current []string

		err := tx.Raw(
			"SELECT password_hash FROM users WHERE id = ? AND deleted_at IS NULL FOR SHARE", token.UserID,
		).Scan(&current).Error
		if err != nil {
			return err
		}

		if len(current) == 0 || current[0] != verifiedHash {
			return ErrCredentialsChanged
		}

		err = tx.Create(token).Error
		if err != nil {
			return err
		}

		return purgeExpiredTokens(tx, token.UserID, token.CreatedAt, sessionMax)
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
			rotation, err = answerReuse(tx, tokenHash, now, next, policy)

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
	ExpiresAt        time.Time
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

	return &found, purgeExpiredTokens(tx, found.ID, now, policy.SessionMax)
}

// answerReuse decides what a token that cannot be used is. A token that is unknown, or was never used, is only refused. A token
// that was used a moment ago, within the leeway, belongs to a client that lost the answer, so it gets a new token of the same
// session, unless it has expired since. A token that was used earlier is taken for a theft and its whole session ends.
func answerReuse(tx *gorm.DB, tokenHash string, now time.Time, next *entity.RefreshToken, policy RotationPolicy) (Rotation, error) {
	const columns = "SELECT user_id, family_id, session_started_at, used_at, expires_at FROM refresh_tokens WHERE token_hash = ?"

	var seen []sessionRow

	// This first read takes no lock. A token that was used before the leeway is a theft, nothing is stored for it, and ending the
	// session takes the locks that it needs. Two requests that show the same stale token would otherwise both hold a share lock on
	// it and both wait for the other to let go when the session is deleted, which PostgreSQL ends as a deadlock after a second.
	// The time of use never changes once it is set, so a decision on this read cannot be undone by a request in between.
	err := tx.Raw(columns, tokenHash).Scan(&seen).Error
	if err != nil || len(seen) == 0 || seen[0].UsedAt == nil {
		return Rotation{}, err
	}

	// a leeway of 0 means none, even for a request whose clock is a little behind the one that used the token
	if policy.ReuseLeeway <= 0 || now.Sub(*seen[0].UsedAt) > policy.ReuseLeeway {
		return Rotation{Reused: true, UserID: seen[0].UserID}, sessions.EndFamily(tx, seen[0].FamilyID)
	}

	// A session that has reached its longest lifetime ends here and does not go on. Ending it takes the locks of the end of a
	// session, and doing that while this request holds the share lock below would wait on a request that waits for this one, so
	// it is decided before the lock is taken. The expiry is capped the same way as in continueSession.
	expires := next.ExpiresAt
	if limit := seen[0].SessionStartedAt.Add(policy.SessionMax); expires.After(limit) {
		expires = limit
	}

	if !expires.After(now) {
		return Rotation{}, sessions.EndFamily(tx, seen[0].FamilyID)
	}

	// A token that was used a moment ago belongs to a client that lost the answer, so it gets a new token of the same session.
	// FOR SHARE makes a logout, a password change or a deletion that ends the family wait for this request, so the token that it
	// stores is seen and removed by the second pass of that deletion, the same way as for a token that a refresh has just used.
	// The row is read again, because a request that ended the session may have removed it since the first read.
	seen = nil

	err = tx.Raw(columns+" FOR SHARE", tokenHash).Scan(&seen).Error
	if err != nil || len(seen) == 0 || seen[0].UsedAt == nil {
		return Rotation{}, err
	}

	if !seen[0].ExpiresAt.After(now) {
		// a client that retries after its token expired has nothing to be taken for a thief, it signs in again
		return Rotation{}, nil
	}

	user, err := continueSession(tx, seen[0], now, next, policy)

	return Rotation{User: user, Replayed: user != nil}, err
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
// expired tokens that were never used and the used ones whose session has reached its longest lifetime, because such a
// session cannot be refreshed any more and a replay of its tokens has nothing left to end. A used token of a session that is
// still alive stays even when it has expired, because showing it again, however late, must still end the session.
//
// A row that another transaction has locked is left for the next time. The cleanup runs while the refresh that called it
// holds the lock of the token it used, so waiting here could close a circle with a deletion that holds the lock of an
// expired token and waits for that refresh. See package sessions.
func purgeExpiredTokens(tx *gorm.DB, userID uuid.UUID, now time.Time, sessionMax time.Duration) error {
	return tx.Exec(
		"DELETE FROM refresh_tokens WHERE id IN (SELECT id FROM refresh_tokens WHERE user_id = ? AND "+
			"((used_at IS NULL AND expires_at <= ?) OR (used_at IS NOT NULL AND session_started_at <= ?)) FOR UPDATE SKIP LOCKED)",
		userID, now, now.Add(-sessionMax),
	).Error
}

func (r *AuthDB) StoreVerification(
	ctx context.Context, userID uuid.UUID, codeHash string, now time.Time, rule SendRule,
) (time.Duration, error) {
	var wait time.Duration

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []struct {
			LastSentAt      time.Time
			WindowStartedAt time.Time
			SendsInWindow   int
		}

		// the row of the account is locked, so two requests for a code cannot both pass the cooldown
		err := tx.Raw(
			"SELECT last_sent_at, window_started_at, sends_in_window FROM email_verifications WHERE user_id = ? FOR UPDATE", userID,
		).Scan(&rows).Error
		if err != nil {
			return err
		}

		expires := now.Add(rule.TTL)

		if len(rows) == 0 {
			return tx.Exec(
				`INSERT INTO email_verifications (user_id, code_hash, expires_at, attempts, last_sent_at, window_started_at, sends_in_window)
				VALUES (?, ?, ?, 0, ?, ?, 1)`, userID, codeHash, expires, now, now,
			).Error
		}

		last := rows[0]

		if elapsed := now.Sub(last.LastSentAt); elapsed < rule.Cooldown {
			wait = rule.Cooldown - elapsed

			return nil
		}

		windowStart, sent := last.WindowStartedAt, last.SendsInWindow
		if now.Sub(windowStart) >= time.Hour {
			windowStart, sent = now, 0
		}

		if sent >= rule.PerHour {
			wait = windowStart.Add(time.Hour).Sub(now)

			return nil
		}

		return tx.Exec(
			`UPDATE email_verifications SET code_hash = ?, expires_at = ?, attempts = 0, last_sent_at = ?, window_started_at = ?,
				sends_in_window = ? WHERE user_id = ?`, codeHash, expires, now, windowStart, sent+1, userID,
		).Error
	})

	// two first codes that raced for the same account: the other one won and the cooldown applies to this one
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return time.Minute, nil
	}

	return wait, err
}

func (r *AuthDB) CheckVerification(
	ctx context.Context, userID uuid.UUID, codeHash string, now time.Time, maxAttempts int,
) (bool, error) {
	var matched bool

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []struct {
			CodeHash  string
			ExpiresAt time.Time
			Attempts  int
		}

		err := tx.Raw("SELECT code_hash, expires_at, attempts FROM email_verifications WHERE user_id = ? FOR UPDATE", userID).Scan(&rows).Error
		if err != nil {
			return err
		}

		// no code, an expired one or one that took all its attempts is refused without counting, so it cannot be probed further
		if len(rows) == 0 || !rows[0].ExpiresAt.After(now) || rows[0].Attempts >= maxAttempts {
			return nil
		}

		if subtle.ConstantTimeCompare([]byte(rows[0].CodeHash), []byte(codeHash)) != 1 {
			return tx.Exec("UPDATE email_verifications SET attempts = attempts + 1 WHERE user_id = ?", userID).Error
		}

		// the account may have been deleted or verified while the code was on its way
		done := tx.Exec(
			"UPDATE users SET email_verification_pending = FALSE, updated_at = ? WHERE id = ? AND deleted_at IS NULL AND email_verification_pending",
			now, userID,
		)
		if done.Error != nil {
			return done.Error
		}

		err = tx.Exec("DELETE FROM email_verifications WHERE user_id = ?", userID).Error
		if err != nil {
			return err
		}

		matched = done.RowsAffected == 1

		return nil
	})

	return matched && err == nil, err
}

func (r *AuthDB) ReleaseVerification(ctx context.Context, userID uuid.UUID, codeHash string, now time.Time, cooldown time.Duration) error {
	return r.db.WithContext(ctx).Exec(
		`UPDATE email_verifications SET last_sent_at = ?, sends_in_window = GREATEST(sends_in_window - 1, 0)
		WHERE user_id = ? AND code_hash = ?`, now.Add(-cooldown), userID, codeHash,
	).Error
}
