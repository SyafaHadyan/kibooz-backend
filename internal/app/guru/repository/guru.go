// Package repository handles the guru related database operations
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
)

// GuruRow is a guru joined with the account columns shown on the dashboard
type GuruRow struct {
	entity.Guru `gorm:"embedded"`
	FullName    string
	AvatarURL   *string
}

// MoodRecord is the minimal mood log projection used by aggregations
type MoodRecord struct {
	StudentID  uuid.UUID
	MoodType   constants.Mood
	RecordedAt time.Time
}

type GuruDBItf interface {
	FindGuruByUserID(ctx context.Context, userID uuid.UUID) (*GuruRow, error)
	// FindTaughtClass returns the requested class when the guru teaches it, or the oldest taught class when classID is nil
	FindTaughtClass(ctx context.Context, guruID uuid.UUID, classID *uuid.UUID) (*entity.Class, error)
	CountStudents(ctx context.Context, classID uuid.UUID) (int, error)
	ListMoodRecords(ctx context.Context, classID uuid.UUID, from time.Time, to time.Time) ([]MoodRecord, error)
	FindStudent(ctx context.Context, studentID uuid.UUID) (*entity.Student, error)
	TeachesClass(ctx context.Context, guruID uuid.UUID, classID uuid.UUID) (bool, error)
	CreateMoodLog(ctx context.Context, moodLog *entity.MoodLog) error
}

type GuruDB struct {
	db *gorm.DB
}

func NewGuruDB(db *gorm.DB) GuruDBItf {
	return &GuruDB{db: db}
}

func (r *GuruDB) FindGuruByUserID(ctx context.Context, userID uuid.UUID) (*GuruRow, error) {
	var rows []GuruRow

	err := r.db.WithContext(ctx).
		Table("gurus AS g").
		Select("g.*, u.full_name AS full_name, u.avatar_url AS avatar_url").
		Joins("JOIN users AS u ON u.id = g.user_id").
		Where("g.user_id = ? AND g.deleted_at IS NULL AND u.deleted_at IS NULL", userID).
		Limit(1).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	if len(rows) == 0 {
		return nil, nil
	}

	return &rows[0], nil
}

func (r *GuruDB) FindTaughtClass(ctx context.Context, guruID uuid.UUID, classID *uuid.UUID) (*entity.Class, error) {
	query := r.db.WithContext(ctx).
		Table("classes AS c").
		Select("c.*").
		Joins("JOIN class_teachers AS ct ON ct.class_id = c.id").
		Where("ct.guru_id = ?", guruID)

	if classID != nil {
		query = query.Where("c.id = ?", *classID)
	}

	var classes []entity.Class

	err := query.Order("c.created_at ASC, c.id ASC").Limit(1).Scan(&classes).Error
	if err != nil {
		return nil, err
	}

	if len(classes) == 0 {
		return nil, nil
	}

	return &classes[0], nil
}

func (r *GuruDB) CountStudents(ctx context.Context, classID uuid.UUID) (int, error) {
	var total int64

	err := r.db.WithContext(ctx).Model(&entity.Student{}).Where("class_id = ?", classID).Count(&total).Error

	return int(total), err
}

func (r *GuruDB) ListMoodRecords(ctx context.Context, classID uuid.UUID, from time.Time, to time.Time) ([]MoodRecord, error) {
	var records []MoodRecord

	err := r.db.WithContext(ctx).
		Table("mood_logs AS m").
		Select("m.student_id AS student_id, m.mood_type AS mood_type, m.recorded_at AS recorded_at").
		Joins("JOIN students AS s ON s.id = m.student_id").
		Where("s.class_id = ? AND s.deleted_at IS NULL AND m.recorded_at >= ? AND m.recorded_at < ?", classID, from, to).
		Order("m.recorded_at ASC").
		Scan(&records).Error

	return records, err
}

func (r *GuruDB) FindStudent(ctx context.Context, studentID uuid.UUID) (*entity.Student, error) {
	var student entity.Student

	err := r.db.WithContext(ctx).Where("id = ?", studentID).Take(&student).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &student, nil
}

func (r *GuruDB) TeachesClass(ctx context.Context, guruID uuid.UUID, classID uuid.UUID) (bool, error) {
	var total int64

	err := r.db.WithContext(ctx).
		Model(&entity.ClassTeacher{}).
		Where("guru_id = ? AND class_id = ?", guruID, classID).
		Count(&total).Error

	return total > 0, err
}

func (r *GuruDB) CreateMoodLog(ctx context.Context, moodLog *entity.MoodLog) error {
	return r.db.WithContext(ctx).Create(moodLog).Error
}
