package repository

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/kiboozapp/kibooz-backend/internal/constants"
)

// GuruHistoryRow holds one timeline entry returned from the class history query
type GuruHistoryRow struct {
	ID              uuid.UUID
	EventType       string
	StudentID       *uuid.UUID
	StudentName     *string
	ActorName       *string
	ActorDeleted    bool
	MoodType        *constants.Mood
	ConfidenceScore *float32
	GuidanceID      *string
	Title           *string
	Description     *string
	OccurredAt      time.Time
}

func (r *GuruDB) ListClassHistory(
	ctx context.Context, classID uuid.UUID, eventType string, limit int, offset int,
) ([]GuruHistoryRow, int, error) {
	branches := make([]string, 0, 3)
	args := make([]any, 0, 3)

	if eventType == "" || eventType == "all" || eventType == "mood" {
		branches = append(branches, `SELECT m.id, 'mood' AS event_type, m.recorded_at AS occurred_at
			FROM mood_logs AS m
			JOIN students AS s ON s.id = m.student_id
			LEFT JOIN enrollments AS e ON e.student_id = s.id AND e.class_id = s.class_id AND e.status = 'active'
			WHERE s.class_id = ? AND s.deleted_at IS NULL
				AND (e.activated_at IS NULL OR m.recorded_at >= e.activated_at
					OR NOT EXISTS (SELECT 1 FROM enrollments AS p WHERE p.student_id = s.id AND p.status = 'ended'))`)
		args = append(args, classID)
	}

	if eventType == "" || eventType == "all" || eventType == "guidance" {
		branches = append(branches, `SELECT g.id, 'guidance' AS event_type, g.applied_at AS occurred_at
			FROM guidance_applications AS g
			JOIN students AS s ON s.id = g.student_id
			JOIN enrollments AS e ON e.student_id = s.id AND e.class_id = s.class_id AND e.status = 'active'
			WHERE s.class_id = ? AND s.deleted_at IS NULL AND g.applied_at >= e.activated_at`)
		args = append(args, classID)
	}

	if eventType == "" || eventType == "all" || eventType == "video" {
		branches = append(branches, `SELECT v.id, 'video' AS event_type, v.created_at AS occurred_at
			FROM learning_videos AS v
			WHERE v.class_id = ?`)
		args = append(args, classID)
	}

	// the union only holds what is needed to filter and order, and the names and texts are read for the rows of one page
	unionSQL := strings.Join(branches, " UNION ALL ")

	var (
		total int64
		rows  []GuruHistoryRow
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
		SELECT p.id, p.event_type, p.occurred_at, s.id AS student_id, s.full_name AS student_name,
			COALESCE(gu.full_name, wu.full_name) AS actor_name,
			(gu.deleted_at IS NOT NULL OR wu.deleted_at IS NOT NULL) AS actor_deleted,
			m.mood_type::text AS mood_type, m.confidence_score, g.guidance_id, v.title,
			COALESCE(m.notes, g.parent_notes, v.description) AS description
		FROM page AS p
		LEFT JOIN mood_logs AS m ON p.event_type = 'mood' AND m.id = p.id
		LEFT JOIN guidance_applications AS g ON p.event_type = 'guidance' AND g.id = p.id
		LEFT JOIN learning_videos AS v ON p.event_type = 'video' AND v.id = p.id
		LEFT JOIN students AS s ON s.id = COALESCE(m.student_id, g.student_id)
		LEFT JOIN gurus AS gr ON gr.id = COALESCE(m.recorded_by_guru_id, v.added_by_guru_id)
		LEFT JOIN users AS gu ON gu.id = gr.user_id
		LEFT JOIN walis AS w ON w.id = g.wali_id
		LEFT JOIN users AS wu ON wu.id = w.user_id
		ORDER BY p.occurred_at DESC, p.id DESC`, pageArgs...).Scan(&rows).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}

	if total == 0 {
		return []GuruHistoryRow{}, 0, nil
	}

	return rows, int(total), nil
}
