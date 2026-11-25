package repository

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
)

// WaliHistoryRow holds one timeline entry returned from the child history query
type WaliHistoryRow struct {
	ID            uuid.UUID
	EventType     string
	MoodType      *constants.Mood
	TrashType     *constants.TrashType
	PointsAwarded *int
	GuidanceID    *string
	Notes         *string
	OccurredAt    time.Time
}

func (r *WaliDB) ListChildHistory(
	ctx context.Context, studentID uuid.UUID, eventType string, limit int, offset int,
) ([]WaliHistoryRow, int, error) {
	branches := make([]string, 0, 3)
	args := make([]any, 0, 6)

	if eventType == "" || eventType == "all" || eventType == "mood" {
		branches = append(branches, `SELECT m.id, 'mood' AS event_type, m.recorded_at AS occurred_at
			FROM mood_logs AS m
			WHERE m.student_id = ?
				AND m.recorded_at >= COALESCE((
					SELECT e.activated_at FROM enrollments AS e
					WHERE e.student_id = ? AND e.status = 'active'
						AND EXISTS (SELECT 1 FROM enrollments AS p WHERE p.student_id = e.student_id AND p.status = 'ended')
				), '-infinity'::timestamptz)`)
		args = append(args, studentID, studentID)
	}

	if eventType == "" || eventType == "all" || eventType == "trash" {
		branches = append(branches, `SELECT t.id, 'trash' AS event_type, t.scanned_at AS occurred_at
			FROM trash_scans AS t
			WHERE t.student_id = ?
				AND t.scanned_at >= COALESCE((
					SELECT activated_at FROM enrollments WHERE student_id = ? AND status = 'active'
				), '-infinity'::timestamptz)`)
		args = append(args, studentID, studentID)
	}

	if eventType == "" || eventType == "all" || eventType == "guidance" {
		branches = append(branches, `SELECT g.id, 'guidance' AS event_type, g.applied_at AS occurred_at
			FROM guidance_applications AS g
			WHERE g.student_id = ?
				AND g.applied_at >= COALESCE((
					SELECT activated_at FROM enrollments WHERE student_id = ? AND status = 'active'
				), '-infinity'::timestamptz)`)
		args = append(args, studentID, studentID)
	}

	// the union only holds what is needed to filter and order, and the other columns are read for the rows of one page
	unionSQL := strings.Join(branches, " UNION ALL ")

	var (
		total int64
		rows  []WaliHistoryRow
	)

	// one snapshot for the count and the page, so the total and the rows agree
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Raw("SELECT COUNT(*) FROM ("+unionSQL+") AS events", args...).Scan(&total).Error
		if err != nil || total == 0 {
			return err
		}

		pageArgs := make([]any, 0, len(args)+2)
		pageArgs = append(pageArgs, args...)
		pageArgs = append(pageArgs, limit, offset)

		return tx.Raw(`WITH page AS (
			SELECT id, event_type, occurred_at FROM (`+unionSQL+`) AS events
			ORDER BY occurred_at DESC, id DESC LIMIT ? OFFSET ?
		)
		SELECT p.id, p.event_type, p.occurred_at, m.mood_type::text AS mood_type, t.trash_type::text AS trash_type,
			t.points_awarded, g.guidance_id, COALESCE(m.notes, g.parent_notes) AS notes
		FROM page AS p
		LEFT JOIN mood_logs AS m ON p.event_type = 'mood' AND m.id = p.id
		LEFT JOIN trash_scans AS t ON p.event_type = 'trash' AND t.id = p.id
		LEFT JOIN guidance_applications AS g ON p.event_type = 'guidance' AND g.id = p.id
		ORDER BY p.occurred_at DESC, p.id DESC`, pageArgs...).Scan(&rows).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}

	if total == 0 {
		return []WaliHistoryRow{}, 0, nil
	}

	return rows, int(total), nil
}
