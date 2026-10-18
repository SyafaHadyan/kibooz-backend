// Package classteam keeps every class with an active teacher and an owner. The owner can remove other teachers and hand the
// class over, and when the owner leaves or deletes the account the teacher who joined next becomes the owner. A teacher who is
// the only one of a class has to name a successor to delete the account, so a class is never left without a teacher.
package classteam

import (
	"slices"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
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

// Leave readies the deletion of a teacher account so that no class is left without an active teacher or without an owner.
// A class that another teacher also teaches goes on with that teacher, who becomes the owner when this account owns it. A
// class that nobody else teaches goes to the teacher with the email successorEmail, who is added as its owner, and when the
// account is the only teacher of such a class and no active teacher has that email nothing is changed and an error says so.
//
// The rows of the two accounts are locked first, in the order of their ids, and the classes after them in the order of their
// ids. A successor who deletes the account at the same moment is then either seen as deleted here or finishes its own
// handover first, so a class is never given to an account that is on its way out, and two deletions cannot wait for each other.
func Leave(tx *gorm.DB, userID uuid.UUID, successorEmail string) error {
	var own []uuid.UUID

	err := tx.Raw("SELECT id FROM gurus WHERE user_id = ? AND deleted_at IS NULL", userID).Scan(&own).Error
	if err != nil || len(own) == 0 {
		return err
	}

	guruID := own[0]

	var successor *uuid.UUID

	if successorEmail != "" {
		var found []uuid.UUID

		err = tx.Raw(`
			SELECT g.id
			FROM users AS u
			JOIN gurus AS g ON g.user_id = u.id AND g.deleted_at IS NULL
			WHERE u.email = ? AND u.role = ? AND u.deleted_at IS NULL
			LIMIT 1`, successorEmail, constants.RoleGuru).Scan(&found).Error
		if err != nil {
			return err
		}

		if len(found) > 0 {
			successor = &found[0]
		}
	}

	ids := []uuid.UUID{guruID}
	if successor != nil && *successor != guruID {
		ids = append(ids, *successor)
	}

	var locked []uuid.UUID

	err = tx.Raw("SELECT id FROM gurus WHERE id IN ? AND deleted_at IS NULL ORDER BY id FOR UPDATE", ids).Scan(&locked).Error
	if err != nil {
		return err
	}

	// the successor was deleted between the lookup and the lock
	if successor != nil && !slices.Contains(locked, *successor) {
		successor = nil
	}

	var taught []uuid.UUID

	err = tx.Raw("SELECT class_id FROM class_teachers WHERE guru_id = ? ORDER BY class_id", guruID).Scan(&taught).Error
	if err != nil {
		return err
	}

	for _, classID := range taught {
		err = Lock(tx, classID)
		if err != nil {
			return err
		}
	}

	for _, classID := range taught {
		err = leaveClass(tx, classID, guruID, successorEmail, successor)
		if err != nil {
			return err
		}
	}

	return nil
}

// leaveClass moves one class away from the account that is going, the caller holds Lock for it
func leaveClass(tx *gorm.DB, classID uuid.UUID, guruID uuid.UUID, successorEmail string, successor *uuid.UUID) error {
	var others int64

	err := tx.Raw(`
		SELECT COUNT(*)
		FROM class_teachers AS ct
		JOIN gurus AS g ON g.id = ct.guru_id AND g.deleted_at IS NULL
		WHERE ct.class_id = ? AND ct.guru_id <> ?`, classID, guruID).Scan(&others).Error
	if err != nil {
		return err
	}

	if others > 0 {
		return HandOver(tx, classID, guruID)
	}

	if successorEmail == "" {
		return archiveEmpty(tx, classID)
	}

	if successor == nil {
		return apperror.ErrTeacherNotFound
	}

	if *successor == guruID {
		return apperror.Validation(map[string]string{"successorEmail": "must be another teacher"})
	}

	// the owner index is checked after each statement, so the account steps down before the successor takes the class
	err = tx.Exec("UPDATE class_teachers SET is_owner = false WHERE class_id = ? AND guru_id = ?", classID, guruID).Error
	if err != nil {
		return err
	}

	return tx.Create(&entity.ClassTeacher{ClassID: classID, GuruID: *successor, IsOwner: true}).Error
}

// archiveEmpty archives a class that nobody else teaches when it has no active child, because no successor is needed for
// a class without children. A class with children still needs a successor. The caller holds Lock for the class, which
// child registration takes too, so a child cannot join between the count and the archive.
func archiveEmpty(tx *gorm.DB, classID uuid.UUID) error {
	var children int64

	err := tx.Raw("SELECT COUNT(*) FROM students WHERE class_id = ? AND deleted_at IS NULL", classID).Scan(&children).Error
	if err != nil {
		return err
	}

	if children > 0 {
		return apperror.ErrSuccessorRequired
	}

	return tx.Exec("UPDATE classes SET archived_at = now() WHERE id = ? AND archived_at IS NULL", classID).Error
}

// LockLive takes Lock for a class and tells whether it was archived, which a teacher who is leaving does under the same lock,
// so the answer holds until the transaction ends.
func LockLive(tx *gorm.DB, classID uuid.UUID) (archived bool, err error) {
	err = Lock(tx, classID)
	if err != nil {
		return false, err
	}

	err = tx.Raw("SELECT archived_at IS NOT NULL FROM classes WHERE id = ?", classID).Scan(&archived).Error

	return archived, err
}

// LockOpen takes the lock for a class that a join code led to and then checks again that the class was not archived while
// the request waited. An archived class answers like a code that does not exist.
func LockOpen(tx *gorm.DB, classID uuid.UUID) error {
	archived, err := LockLive(tx, classID)
	if err != nil {
		return err
	}

	if archived {
		return apperror.ErrClassCodeNotFound
	}

	return nil
}
