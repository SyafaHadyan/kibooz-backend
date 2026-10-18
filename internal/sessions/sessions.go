// Package sessions ends sessions by deleting their refresh tokens without losing a token that a refresh stores at the same moment
package sessions

import (
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// LockUser takes the row of the user for the rest of the transaction, and it has to be the first lock that a password change
// or an account deletion takes. A sign in reads that row under a share lock before it stores its session, so a sign in that
// is under way makes the change wait for it, and the change then deletes the session it stored. A sign in that starts later
// waits for the change and finds the new hash. The lock is FOR NO KEY UPDATE, which is what an UPDATE of the row takes
// anyway and which a refresh does not wait for, because a new token only takes a key share of the row.
//
// The order of locks is the user row first, then the tokens, then everything else. A transaction that takes the tokens
// before the user row can wait for one that holds the row and waits for those tokens, and PostgreSQL has to abort one of
// them.
func LockUser(tx *gorm.DB, userID uuid.UUID) error {
	return tx.Exec("SELECT id FROM users WHERE id = ? FOR NO KEY UPDATE", userID).Error
}

// EndFamily deletes every token of one session, and EndUser deletes every token of an account. They have to run in a
// transaction, and they take the locks before the delete on purpose.
//
// A refresh marks its token as used and stores the next one in a single transaction. A DELETE that starts while that
// transaction is open sees the old rows only, because every statement reads the rows that were committed when it
// started. It waits for the lock on the used token, deletes that one when the refresh commits, and never sees the new
// token, so the session would live on. The statement that locks the live tokens waits the same way, but the DELETE that
// follows starts after the refresh committed and reads its result. Locking the live tokens in the order of their id
// keeps two sessions that end at once from waiting on each other. The cleanup that a refresh does after it stored the
// next token skips the rows that are locked, so it never waits for a transaction that waits for the refresh.
func EndFamily(tx *gorm.DB, familyID uuid.UUID) error {
	return end(tx, "family_id", familyID)
}

func EndUser(tx *gorm.DB, userID uuid.UUID) error {
	return end(tx, "user_id", userID)
}

// maxPasses is how many times end may lock and delete again, which only a session that is refreshed over and over while it
// is ended can use up
const maxPasses = 8

// end deletes the tokens until a delete finds none. The lock only holds the tokens that were live when its statement
// started. A refresh that begins after that, on a token that an earlier refresh stored a moment ago, marks it as used while
// the DELETE is running, and the DELETE reads the rows from before it, so it removes the used token and leaves the one that
// the refresh stored. A delete that finds nothing proves that no token was left to refresh, because every token a refresh
// could still be holding was either deleted by the pass before or waited for by this one.
func end(tx *gorm.DB, column string, id uuid.UUID) error {
	for range maxPasses {
		deleted, err := endPass(tx, column, id)
		if err != nil {
			return err
		}

		if deleted == 0 {
			return nil
		}
	}

	return errors.New("the tokens of a session were refreshed again and again while it was ended")
}

// endPass locks every token that has not been used, whether it has expired or not, because a refresh has to mark one of those
// first and it judges the expiry by the clock of the API, which can differ from the clock of PostgreSQL. A token that
// was used cannot be taken by a refresh again, so it needs no lock. It returns how many tokens it deleted.
func endPass(tx *gorm.DB, column string, id uuid.UUID) (int64, error) {
	err := tx.Exec("SELECT id FROM refresh_tokens WHERE "+column+" = ? AND used_at IS NULL ORDER BY id FOR UPDATE", id).Error
	if err != nil {
		return 0, err
	}

	deleted := tx.Exec("DELETE FROM refresh_tokens WHERE "+column+" = ?", id)

	return deleted.RowsAffected, deleted.Error
}
