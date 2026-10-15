// Package classteam keeps the ownership of a class with one of its active teachers. The owner can remove other teachers and
// hand the class over, and when the owner leaves or deletes the account the teacher who joined next becomes the owner.
package classteam

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Lock serializes every change of the teachers of one class until the transaction ends, so two teachers that leave at the
// same moment cannot both find another teacher still there
func Lock(tx *gorm.DB, classID uuid.UUID) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "teachers:"+classID.String()).Error
}

// nextOwnerSQL finds the active teacher who joined first, leaving out the one who is going
const nextOwnerSQL = `
SELECT ct.guru_id
FROM class_teachers AS ct
JOIN gurus AS g ON g.id = ct.guru_id AND g.deleted_at IS NULL
WHERE ct.class_id = ? AND ct.guru_id <> ?
ORDER BY ct.created_at ASC, ct.guru_id ASC
LIMIT 1`

// Transfer makes to the owner of the class in place of from
func Transfer(tx *gorm.DB, classID uuid.UUID, from uuid.UUID, to uuid.UUID) error {
	// the one owner index is checked after each statement, so the old owner steps down first
	err := tx.Exec("UPDATE class_teachers SET is_owner = false WHERE class_id = ? AND guru_id = ?", classID, from).Error
	if err != nil {
		return err
	}

	return tx.Exec("UPDATE class_teachers SET is_owner = true WHERE class_id = ? AND guru_id = ?", classID, to).Error
}

// HandOver gives the class to the teacher who joined next when leaving owns it, and does nothing when leaving does not own
// it. A class where nobody else teaches keeps its owner. The caller holds Lock for the class.
func HandOver(tx *gorm.DB, classID uuid.UUID, leaving uuid.UUID) error {
	var owned int64

	err := tx.Table("class_teachers").Where("class_id = ? AND guru_id = ? AND is_owner", classID, leaving).Count(&owned).Error
	if err != nil || owned == 0 {
		return err
	}

	var next []uuid.UUID

	err = tx.Raw(nextOwnerSQL, classID, leaving).Scan(&next).Error
	if err != nil || len(next) == 0 {
		return err
	}

	return Transfer(tx, classID, leaving, next[0])
}

// ReleaseAll hands over every class that the account owns. The classes are locked in one fixed order, so two deletions
// that touch the same classes cannot wait for each other.
func ReleaseAll(tx *gorm.DB, userID uuid.UUID) error {
	var owned []struct {
		ClassID uuid.UUID
		GuruID  uuid.UUID
	}

	err := tx.Raw(`
		SELECT ct.class_id, ct.guru_id
		FROM class_teachers AS ct
		JOIN gurus AS g ON g.id = ct.guru_id
		WHERE g.user_id = ? AND ct.is_owner
		ORDER BY ct.class_id`, userID).Scan(&owned).Error
	if err != nil {
		return err
	}

	for _, item := range owned {
		err = Lock(tx, item.ClassID)
		if err != nil {
			return err
		}

		err = HandOver(tx, item.ClassID, item.GuruID)
		if err != nil {
			return err
		}
	}

	return nil
}
