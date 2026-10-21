// Package childremoval hides a child the way a deleted account does, for a teacher who takes a child out of a class and for
// a parent who deletes a child. The child is soft deleted, the class ranking is renumbered, and the avatar and the photos of the
// scans of the child leave their rows and go on the cleanup queue in the same transaction.
package childremoval

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/filecleanup"
	"github.com/SyafaHadyan/kibooz-backend/internal/ranking"
)

// Scope narrows the query of the child, for example to the class of a teacher or to the children of a parent
type Scope func(rows *gorm.DB) *gorm.DB

// Remove hides the active child with this id that the scope lets through, and returns the class of the child. It returns
// false when there is no such child. The class lock comes first, the order that the deletion of a parent and a join take it
// in, and it makes the renumbering the only one that runs for the class.
func Remove(tx *gorm.DB, studentID uuid.UUID, scope Scope, fileKey filecleanup.KeyFunc) (uuid.UUID, bool, error) {
	var classes []uuid.UUID

	err := scope(tx.Model(&entity.Student{}).Where("id = ?", studentID)).Pluck("class_id", &classes).Error
	if err != nil || len(classes) == 0 {
		return uuid.Nil, false, err
	}

	classID := classes[0]

	err = ranking.Lock(tx, classID)
	if err != nil {
		return uuid.Nil, false, err
	}

	// a move may have taken the child to another class while the request waited for the lock, and then the new class is the one
	// that is renumbered, a scope that names a class lets the child through only where it still is
	classes = classes[:0]

	err = scope(tx.Model(&entity.Student{}).Where("id = ?", studentID)).Pluck("class_id", &classes).Error
	if err != nil || len(classes) == 0 {
		return uuid.Nil, false, err
	}

	if classes[0] != classID {
		classID = classes[0]

		err = ranking.Lock(tx, classID)
		if err != nil {
			return uuid.Nil, false, err
		}
	}

	child := scope(tx.Model(&entity.Student{}).Where("id = ?", studentID))

	// the child may have been removed while the request waited for the lock
	var found int64

	err = child.Session(&gorm.Session{}).Count(&found).Error
	if err != nil || found == 0 {
		return uuid.Nil, false, err
	}

	var urls []string

	err = filecleanup.TakeAddresses(&urls, child, "avatar_url")
	if err == nil {
		err = filecleanup.TakeAddresses(&urls, tx.Model(&entity.TrashScan{}).Where("student_id = ?", studentID), "photo_url")
	}

	if err == nil {
		err = child.Session(&gorm.Session{}).Delete(&entity.Student{}).Error
	}

	if err == nil {
		err = ranking.Recompute(tx, classID)
	}

	if err == nil {
		err = filecleanup.Queue(tx, filecleanup.KeysOf(urls, fileKey))
	}

	if err != nil {
		return uuid.Nil, false, err
	}

	return classID, true, nil
}
