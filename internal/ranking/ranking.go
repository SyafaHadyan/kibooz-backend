// Package ranking keeps students.rank_position in sync with points inside a class
package ranking

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const recomputeSQL = `
UPDATE students AS s
SET rank_position = ranked.position
FROM (
    SELECT id, ROW_NUMBER() OVER (ORDER BY current_points DESC, full_name ASC, id ASC) AS position
    FROM students
    WHERE class_id = ?
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
