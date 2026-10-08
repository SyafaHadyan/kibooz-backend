// Package repository handles the wali related database operations
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

// StudentRow is a student joined with the class columns the dashboard needs
type StudentRow struct {
	entity.Student `gorm:"embedded"`
	ClassName      string
	GradeLevel     string
	SchoolName     string
	AcademicYear   string
}

// GuardianRow is the account of a parent joined with the columns of the parent profile
type GuardianRow struct {
	FullName       string
	Email          string
	PhoneNumber    *string
	WhatsappNumber *string
	Address        *string
}

// TrashTotals is what a child collected in one trash category
type TrashTotals struct {
	Scans  int
	Points int
}

type WaliDBItf interface {
	FindWaliByUserID(ctx context.Context, userID uuid.UUID) (*entity.Wali, error)
	// FindStudent returns the requested child of the wali, or the oldest child when studentID is nil
	FindStudent(ctx context.Context, waliID uuid.UUID, studentID *uuid.UUID) (*StudentRow, error)
	LatestMood(ctx context.Context, studentID uuid.UUID, from time.Time, to time.Time) (*entity.MoodLog, error)
	CountTrashByType(ctx context.Context, studentID uuid.UUID) (map[constants.TrashType]int, error)
	// SumTrashByType returns the scans and points of a child per trash category
	SumTrashByType(ctx context.Context, studentID uuid.UUID) (map[constants.TrashType]TrashTotals, error)
	CountScans(ctx context.Context, studentID uuid.UUID, from time.Time, to time.Time) (int, error)
	// FindGuardian returns nil when the account or the parent profile does not exist
	FindGuardian(ctx context.Context, userID uuid.UUID) (*GuardianRow, error)
	CreateGuidanceApplication(ctx context.Context, application *entity.GuidanceApplication) error
}

type WaliDB struct {
	db *gorm.DB
}

func NewWaliDB(db *gorm.DB) WaliDBItf {
	return &WaliDB{db: db}
}

func (r *WaliDB) FindWaliByUserID(ctx context.Context, userID uuid.UUID) (*entity.Wali, error) {
	var wali entity.Wali

	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Take(&wali).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &wali, nil
}

func (r *WaliDB) FindStudent(ctx context.Context, waliID uuid.UUID, studentID *uuid.UUID) (*StudentRow, error) {
	query := r.db.WithContext(ctx).
		Table("students AS s").
		Select("s.*, c.name AS class_name, c.grade_level AS grade_level, c.school_name AS school_name, c.academic_year AS academic_year").
		Joins("JOIN classes AS c ON c.id = s.class_id").
		Where("s.wali_id = ? AND s.deleted_at IS NULL", waliID)

	if studentID != nil {
		query = query.Where("s.id = ?", *studentID)
	}

	var rows []StudentRow

	err := query.Order("s.created_at ASC, s.id ASC").Limit(1).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	if len(rows) == 0 {
		return nil, nil
	}

	return &rows[0], nil
}

func (r *WaliDB) LatestMood(ctx context.Context, studentID uuid.UUID, from time.Time, to time.Time) (*entity.MoodLog, error) {
	var logs []entity.MoodLog

	err := r.db.WithContext(ctx).
		Where("student_id = ? AND recorded_at >= ? AND recorded_at < ?", studentID, from, to).
		Order("recorded_at DESC").
		Limit(1).
		Find(&logs).Error
	if err != nil {
		return nil, err
	}

	if len(logs) == 0 {
		return nil, nil
	}

	return &logs[0], nil
}

func (r *WaliDB) CountTrashByType(ctx context.Context, studentID uuid.UUID) (map[constants.TrashType]int, error) {
	type row struct {
		TrashType constants.TrashType
		Total     int
	}

	var rows []row

	err := r.db.WithContext(ctx).
		Model(&entity.TrashScan{}).
		Select("trash_type, COUNT(*) AS total").
		Where("student_id = ?", studentID).
		Group("trash_type").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	counts := make(map[constants.TrashType]int, len(rows))
	for _, item := range rows {
		counts[item.TrashType] = item.Total
	}

	return counts, nil
}

func (r *WaliDB) CreateGuidanceApplication(ctx context.Context, application *entity.GuidanceApplication) error {
	return r.db.WithContext(ctx).Create(application).Error
}

func (r *WaliDB) SumTrashByType(ctx context.Context, studentID uuid.UUID) (map[constants.TrashType]TrashTotals, error) {
	type row struct {
		TrashType constants.TrashType
		Scans     int
		Points    int
	}

	var rows []row

	err := r.db.WithContext(ctx).
		Model(&entity.TrashScan{}).
		Select("trash_type, COUNT(*) AS scans, COALESCE(SUM(points_awarded), 0) AS points").
		Where("student_id = ?", studentID).
		Group("trash_type").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	totals := make(map[constants.TrashType]TrashTotals, len(rows))
	for _, item := range rows {
		totals[item.TrashType] = TrashTotals{Scans: item.Scans, Points: item.Points}
	}

	return totals, nil
}

func (r *WaliDB) CountScans(ctx context.Context, studentID uuid.UUID, from time.Time, to time.Time) (int, error) {
	var total int64

	err := r.db.WithContext(ctx).Model(&entity.TrashScan{}).
		Where("student_id = ? AND scanned_at >= ? AND scanned_at < ?", studentID, from, to).Count(&total).Error

	return int(total), err
}

func (r *WaliDB) FindGuardian(ctx context.Context, userID uuid.UUID) (*GuardianRow, error) {
	var rows []GuardianRow

	err := r.db.WithContext(ctx).
		Table("walis AS w").
		Select("u.full_name AS full_name, u.email AS email, u.phone_number AS phone_number, "+
			"w.whatsapp_number AS whatsapp_number, w.address AS address").
		Joins("JOIN users AS u ON u.id = w.user_id").
		Where("w.user_id = ? AND w.deleted_at IS NULL AND u.deleted_at IS NULL", userID).
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
