package repository

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
)

// GuruHistoryRow holds one timeline entry returned from the class history query
type GuruHistoryRow struct {
	ID              uuid.UUID
	EventType       string
	StudentID       *uuid.UUID
	StudentName     *string
	ActorName       *string
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
		branches = append(branches, `SELECT m.id, 'mood' AS event_type, s.id AS student_id, s.full_name AS student_name,
			gu.full_name AS actor_name, m.mood_type::text AS mood_type, m.confidence_score,
			NULL::text AS guidance_id, NULL::text AS title, m.notes AS description, m.recorded_at AS occurred_at
			FROM mood_logs AS m
			JOIN students AS s ON s.id = m.student_id
			LEFT JOIN enrollments AS e ON e.student_id = s.id AND e.class_id = s.class_id AND e.status = 'active'
			LEFT JOIN gurus AS gr ON gr.id = m.recorded_by_guru_id
			LEFT JOIN users AS gu ON gu.id = gr.user_id
			WHERE s.class_id = ? AND s.deleted_at IS NULL
				AND (e.activated_at IS NULL OR m.recorded_at >= e.activated_at
					OR NOT EXISTS (SELECT 1 FROM enrollments AS p WHERE p.student_id = s.id AND p.status = 'ended'))`)
		args = append(args, classID)
	}

	if eventType == "" || eventType == "all" || eventType == "guidance" {
		branches = append(branches, `SELECT g.id, 'guidance' AS event_type, s.id AS student_id, s.full_name AS student_name,
			wu.full_name AS actor_name, NULL::text AS mood_type, NULL::real AS confidence_score,
			g.guidance_id, NULL::text AS title, g.parent_notes AS description, g.applied_at AS occurred_at
			FROM guidance_applications AS g
			JOIN students AS s ON s.id = g.student_id
			JOIN enrollments AS e ON e.student_id = s.id AND e.class_id = s.class_id AND e.status = 'active'
			LEFT JOIN walis AS w ON w.id = g.wali_id
			LEFT JOIN users AS wu ON wu.id = w.user_id
			WHERE s.class_id = ? AND s.deleted_at IS NULL AND g.applied_at >= e.activated_at`)
		args = append(args, classID)
	}

	if eventType == "" || eventType == "all" || eventType == "video" {
		branches = append(branches, `SELECT v.id, 'video' AS event_type, NULL::uuid AS student_id, NULL::text AS student_name,
			gu.full_name AS actor_name, NULL::text AS mood_type, NULL::real AS confidence_score,
			NULL::text AS guidance_id, v.title, v.description, v.created_at AS occurred_at
			FROM learning_videos AS v
			LEFT JOIN gurus AS gr ON gr.id = v.added_by_guru_id
			LEFT JOIN users AS gu ON gu.id = gr.user_id
			WHERE v.class_id = ?`)
		args = append(args, classID)
	}

	unionSQL := strings.Join(branches, " UNION ALL ")

	var total int64

	err := r.db.WithContext(ctx).Raw("SELECT COUNT(*) FROM ("+unionSQL+") AS events", args...).Scan(&total).Error
	if err != nil {
		return nil, 0, err
	}

	if total == 0 {
		return []GuruHistoryRow{}, 0, nil
	}

	var rows []GuruHistoryRow

	pageArgs := make([]any, 0, len(args)+2)
	pageArgs = append(pageArgs, args...)
	pageArgs = append(pageArgs, limit, offset)

	err = r.db.WithContext(ctx).
		Raw("SELECT * FROM ("+unionSQL+") AS events ORDER BY occurred_at DESC, id DESC LIMIT ? OFFSET ?", pageArgs...).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	return rows, int(total), nil
}
