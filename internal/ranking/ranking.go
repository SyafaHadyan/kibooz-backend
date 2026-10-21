// Package ranking keeps students.rank_position in sync with the points of the current semester inside a class
package ranking

import (
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const recomputeSQL = `
UPDATE students AS s
SET rank_position = ranked.position
FROM (
    SELECT id, ROW_NUMBER() OVER (ORDER BY season_points DESC, full_name ASC, id ASC) AS position
    FROM students
    WHERE class_id = ? AND deleted_at IS NULL
) AS ranked
WHERE s.id = ranked.id AND s.rank_position <> ranked.position`

// Lock serializes every ranking change of one class until the transaction ends
func Lock(tx *gorm.DB, classID uuid.UUID) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", classID.String()).Error
}

// Recompute renumbers every student of the class by points, then name, then id
func Recompute(tx *gorm.DB, classID uuid.UUID) error {
	err := Lock(tx, classID)
	if err != nil {
		return err
	}

	return tx.Exec(recomputeSQL, classID).Error
}

// ErrMoved says that a child moved to another class while the request waited for the lock of its old class. The transaction has
// to start again from the top, because taking the lock of the new class now, with the old one held, could wait for a move that
// holds the new one and wants the old one.
var ErrMoved = errors.New("the child moved to another class")

// Retry runs a transaction again, three times at most, while it ends with ErrMoved
func Retry(run func() error) error {
	var err error

	for range 3 {
		err = run()
		if !errors.Is(err, ErrMoved) {
			return err
		}
	}

	return err
}
