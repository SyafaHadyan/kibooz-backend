// Package nisn holds the rules that decide who may hold a NISN. The number is unique only among the children that a teacher
// verified, so anyone can register a child with a number that an unverified child holds, and a verification takes the number from
// every unverified child that has it.
package nisn

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
)

// Lock serializes everything that sets, verifies or releases one NISN until the transaction ends. A caller that also takes the lock
// of a class takes the class first, so the two never wait for each other in turns.
func Lock(tx *gorm.DB, number string) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "nisn:"+number).Error
}

// VerifiedHolder tells whether an active child other than except holds the NISN as a verified one
func VerifiedHolder(tx *gorm.DB, number string, except uuid.UUID) (bool, error) {
	var found []int

	err := tx.Raw(`
		SELECT 1 FROM students
		WHERE nisn = ? AND nisn_verified_at IS NOT NULL AND deleted_at IS NULL AND id <> ?
		LIMIT 1`, number, except).Scan(&found).Error

	return len(found) > 0, err
}

// RefuseVerifiedHolder answers apperror.ErrNISNTaken when a verified child other than except holds the NISN. The caller holds Lock.
func RefuseVerifiedHolder(tx *gorm.DB, number string, except uuid.UUID) error {
	taken, err := VerifiedHolder(tx, number, except)
	if err != nil {
		return err
	}

	if taken {
		return apperror.ErrNISNTaken
	}

	return nil
}

// Verify marks the NISN of a child as verified and takes the number from every unverified child that holds it, in any class. They keep
// their records with an empty NISN. It answers apperror.ErrNISNNotSet for a child without a NISN and apperror.ErrNISNTaken when a
// verified child already holds the number. Verifying a child that is verified already changes nothing. The caller holds the lock of
// the class of the child, which a parent also takes to change the number of its child, so the parent cannot change it meanwhile. The
// lock of the NISN is taken here, and the number is read again under it, because a verification of the same number in another class,
// which holds the lock of its own class only, may have taken it from this child in the meantime. It returns how many children lost
// the number.
func Verify(tx *gorm.DB, studentID uuid.UUID, now time.Time) (displaced int64, err error) {
	var current []string

	err = tx.Raw("SELECT nisn FROM students WHERE id = ? AND deleted_at IS NULL", studentID).Scan(&current).Error
	if err != nil {
		return 0, err
	}

	if len(current) == 0 {
		return 0, apperror.ErrStudentNotFound
	}

	number := current[0]
	if number == "" {
		return 0, apperror.ErrNISNNotSet
	}

	err = Lock(tx, number)
	if err != nil {
		return 0, err
	}

	var again []string

	err = tx.Raw("SELECT nisn FROM students WHERE id = ? AND deleted_at IS NULL", studentID).Scan(&again).Error
	if err != nil {
		return 0, err
	}

	if len(again) == 0 || again[0] != number {
		return 0, apperror.ErrNISNNotSet
	}

	err = RefuseVerifiedHolder(tx, number, studentID)
	if err != nil {
		return 0, err
	}

	moved := tx.Exec(`
		UPDATE students SET nisn = ''
		WHERE nisn = ? AND id <> ? AND deleted_at IS NULL AND nisn_verified_at IS NULL`, number, studentID)
	if moved.Error != nil {
		return 0, moved.Error
	}

	err = tx.Exec("UPDATE students SET nisn_verified_at = COALESCE(nisn_verified_at, ?) WHERE id = ?", now, studentID).Error

	return moved.RowsAffected, err
}

// Unverify takes the mark off, so the number is unique no longer and a child that lost it can register it again. It does not give
// the number back to the children that lost it.
func Unverify(tx *gorm.DB, studentID uuid.UUID) error {
	return tx.Exec("UPDATE students SET nisn_verified_at = NULL WHERE id = ? AND deleted_at IS NULL", studentID).Error
}
