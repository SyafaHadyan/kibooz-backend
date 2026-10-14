// Package sessions ends sessions by deleting their refresh tokens without losing a token that a refresh stores at the same moment
package sessions

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// EndFamily deletes every token of one session, and EndUser deletes every token of an account. They have to run in a
// transaction, and they take the locks before the delete on purpose.
//
// A refresh marks its token as used and stores the next one in a single transaction. A DELETE that starts while that
// transaction is open sees the old rows only, because every statement reads the rows that were committed when it
// started. It waits for the lock on the used token, deletes that one when the refresh commits, and never sees the new
// token, so the session would live on. The statement that locks the live tokens waits the same way, but the DELETE that
// follows starts after the refresh committed and reads its result. Locking the live tokens in the order of their id
// keeps two sessions that end at once from waiting on each other.
func EndFamily(tx *gorm.DB, familyID uuid.UUID) error {
	return end(tx, "family_id", familyID)
}

func EndUser(tx *gorm.DB, userID uuid.UUID) error {
	return end(tx, "user_id", userID)
}

// end only locks the tokens that have not been used. A refresh has to mark one of those first, and a token that was used
// long ago would only add locks that a refresh needs to clean up after itself, which could make the two wait on each other.
func end(tx *gorm.DB, column string, id uuid.UUID) error {
	err := tx.Exec("SELECT id FROM refresh_tokens WHERE "+column+" = ? AND used_at IS NULL ORDER BY id FOR UPDATE", id).Error
	if err != nil {
		return err
	}

	return tx.Exec("DELETE FROM refresh_tokens WHERE "+column+" = ?", id).Error
}
