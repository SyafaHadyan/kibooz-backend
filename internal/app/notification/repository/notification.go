// Package repository handles persistence for in-app notifications.
package repository

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/notifier"
)

type NotificationDBItf interface {
	List(ctx context.Context, userID uuid.UUID, unreadOnly bool, limit int, offset int) ([]entity.Notification, int, int, error)
	MarkRead(ctx context.Context, userID uuid.UUID, notificationID uuid.UUID, readAt time.Time) (bool, error)
	MarkAllRead(ctx context.Context, userID uuid.UUID, readAt time.Time) error
	CreateForStudentParent(ctx context.Context, row *entity.Notification, studentID uuid.UUID) error
	// CreateForStudentParents stores one notification for the parent of each student of the notices with one statement
	CreateForStudentParents(
		ctx context.Context, kind string, refType string, createdAt time.Time, notices []notifier.StudentNotice,
	) error
	CreateForMoveRequestParent(ctx context.Context, row *entity.Notification, requestID uuid.UUID) error
	CreateForClassTeachers(ctx context.Context, row *entity.Notification, classID uuid.UUID) error
	CreateForClassParents(ctx context.Context, row *entity.Notification, classID uuid.UUID) error
}

type NotificationDB struct {
	db *gorm.DB
}

func NewNotificationDB(db *gorm.DB) NotificationDBItf {
	return &NotificationDB{db: db}
}

func (r *NotificationDB) List(
	ctx context.Context, userID uuid.UUID, unreadOnly bool, limit int, offset int,
) ([]entity.Notification, int, int, error) {
	var unreadCount int64

	err := r.db.WithContext(ctx).
		Model(&entity.Notification{}).
		Where("user_id = ? AND read_at IS NULL", userID).
		Count(&unreadCount).Error
	if err != nil {
		return nil, 0, 0, err
	}

	query := r.db.WithContext(ctx).Model(&entity.Notification{}).Where("user_id = ?", userID)

	// the unread filter makes the total the unread count, so it is not counted a second time
	total := unreadCount

	if unreadOnly {
		query = query.Where("read_at IS NULL")
	} else {
		err = query.Count(&total).Error
		if err != nil {
			return nil, 0, 0, err
		}
	}

	var rows []entity.Notification

	err = query.Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&rows).Error
	if err != nil {
		return nil, 0, 0, err
	}

	return rows, int(total), int(unreadCount), nil
}

func (r *NotificationDB) MarkRead(ctx context.Context, userID uuid.UUID, notificationID uuid.UUID, readAt time.Time) (bool, error) {
	var exists int64

	err := r.db.WithContext(ctx).
		Model(&entity.Notification{}).
		Where("id = ? AND user_id = ?", notificationID, userID).
		Count(&exists).Error
	if err != nil {
		return false, err
	}

	if exists == 0 {
		return false, nil
	}

	err = r.db.WithContext(ctx).
		Model(&entity.Notification{}).
		Where("id = ? AND user_id = ? AND read_at IS NULL", notificationID, userID).
		Update("read_at", readAt).Error

	return err == nil, err
}

func (r *NotificationDB) MarkAllRead(ctx context.Context, userID uuid.UUID, readAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&entity.Notification{}).
		Where("user_id = ? AND read_at IS NULL", userID).
		Update("read_at", readAt).Error
}

func (r *NotificationDB) CreateForStudentParent(ctx context.Context, row *entity.Notification, studentID uuid.UUID) error {
	return r.insertFromQuery(ctx, row,
		`SELECT w.user_id
		FROM students AS s
		JOIN walis AS w ON w.id = s.wali_id
		JOIN users AS u ON u.id = w.user_id
		WHERE s.id = ? AND s.deleted_at IS NULL AND w.deleted_at IS NULL AND u.deleted_at IS NULL`,
		studentID,
	)
}

func (r *NotificationDB) CreateForStudentParents(
	ctx context.Context, kind string, refType string, createdAt time.Time, notices []notifier.StudentNotice,
) error {
	if len(notices) == 0 {
		return nil
	}

	var values strings.Builder

	args := make([]any, 0, 3+4*len(notices))
	args = append(args, kind, refType, createdAt)

	for i, notice := range notices {
		if i > 0 {
			values.WriteString(", ")
		}

		values.WriteString("(CAST(? AS UUID), CAST(? AS TEXT), CAST(? AS TEXT), CAST(? AS UUID))")

		// a nil reference is stored as NULL, like the single insert does
		var ref any
		if notice.RefID != uuid.Nil {
			ref = notice.RefID
		}

		args = append(args, notice.StudentID, notice.Title, notice.Body, ref)
	}

	return r.db.WithContext(ctx).Exec(
		`INSERT INTO notifications (id, user_id, kind, title, body, reference_type, reference_id, created_at)
		SELECT gen_random_uuid(), w.user_id, CAST(? AS VARCHAR), n.title, n.body, NULLIF(CAST(? AS VARCHAR), ''), n.ref_id, CAST(? AS TIMESTAMPTZ)
		FROM (VALUES `+values.String()+`) AS n(student_id, title, body, ref_id)
		JOIN students AS s ON s.id = n.student_id
		JOIN walis AS w ON w.id = s.wali_id
		JOIN users AS u ON u.id = w.user_id
		WHERE s.deleted_at IS NULL AND w.deleted_at IS NULL AND u.deleted_at IS NULL`,
		args...,
	).Error
}

func (r *NotificationDB) CreateForMoveRequestParent(ctx context.Context, row *entity.Notification, requestID uuid.UUID) error {
	return r.insertFromQuery(ctx, row,
		`SELECT w.user_id
		FROM enrollments AS e
		JOIN students AS s ON s.id = e.student_id
		JOIN walis AS w ON w.id = s.wali_id
		JOIN users AS u ON u.id = w.user_id
		WHERE e.id = ? AND s.deleted_at IS NULL AND w.deleted_at IS NULL AND u.deleted_at IS NULL`,
		requestID,
	)
}

func (r *NotificationDB) CreateForClassTeachers(ctx context.Context, row *entity.Notification, classID uuid.UUID) error {
	return r.insertFromQuery(ctx, row,
		`SELECT DISTINCT g.user_id
		FROM class_teachers AS ct
		JOIN gurus AS g ON g.id = ct.guru_id
		JOIN users AS u ON u.id = g.user_id
		WHERE ct.class_id = ? AND g.deleted_at IS NULL AND u.deleted_at IS NULL`,
		classID,
	)
}

func (r *NotificationDB) CreateForClassParents(ctx context.Context, row *entity.Notification, classID uuid.UUID) error {
	return r.insertFromQuery(ctx, row,
		`SELECT DISTINCT w.user_id
		FROM students AS s
		JOIN walis AS w ON w.id = s.wali_id
		JOIN users AS u ON u.id = w.user_id
		WHERE s.class_id = ? AND s.deleted_at IS NULL AND w.deleted_at IS NULL AND u.deleted_at IS NULL`,
		classID,
	)
}

func (r *NotificationDB) insertFromQuery(
	ctx context.Context, row *entity.Notification, recipientsSQL string, arg uuid.UUID,
) error {
	return r.db.WithContext(ctx).Exec(
		`INSERT INTO notifications (id, user_id, kind, title, body, reference_type, reference_id, created_at)
		SELECT gen_random_uuid(), recipient.user_id, ?, ?, ?, CAST(? AS VARCHAR), CAST(? AS UUID), CAST(? AS TIMESTAMPTZ)
		FROM (`+recipientsSQL+`) AS recipient`,
		row.Kind, row.Title, row.Body, row.ReferenceType, row.ReferenceID, row.CreatedAt, arg,
	).Error
}
