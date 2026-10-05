// Package repository handles the gamification database operations
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/ranking"
)

type ClaimResult struct {
	TotalPoints int
	Rank        int
	Remaining   int
}

type TrashDBItf interface {
	FindWaliIDByUserID(ctx context.Context, userID uuid.UUID) (*uuid.UUID, error)
	FindGuruIDByUserID(ctx context.Context, userID uuid.UUID) (*uuid.UUID, error)
	FindOwnedStudent(ctx context.Context, waliID uuid.UUID, studentID uuid.UUID) (*entity.Student, error)
	CountScans(ctx context.Context, studentID uuid.UUID, from time.Time, to time.Time) (int, error)
	// Claim checks the daily limit, stores the scan, adds the points and renumbers the class in one transaction
	Claim(ctx context.Context, scan *entity.TrashScan, classID uuid.UUID, limit int, from time.Time, to time.Time) (ClaimResult, error)
	ListClassStudents(ctx context.Context, classID uuid.UUID) ([]entity.Student, error)
	FirstWaliClass(ctx context.Context, waliID uuid.UUID) (*uuid.UUID, error)
	WaliHasStudentInClass(ctx context.Context, waliID uuid.UUID, classID uuid.UUID) (bool, error)
	FirstGuruClass(ctx context.Context, guruID uuid.UUID) (*uuid.UUID, error)
	GuruTeachesClass(ctx context.Context, guruID uuid.UUID, classID uuid.UUID) (bool, error)
}

type TrashDB struct {
	db *gorm.DB
}

func NewTrashDB(db *gorm.DB) TrashDBItf {
	return &TrashDB{db: db}
}

func (r *TrashDB) FindWaliIDByUserID(ctx context.Context, userID uuid.UUID) (*uuid.UUID, error) {
	return r.firstID(ctx, "SELECT id FROM walis WHERE user_id = ? LIMIT 1", userID)
}

func (r *TrashDB) FindGuruIDByUserID(ctx context.Context, userID uuid.UUID) (*uuid.UUID, error) {
	return r.firstID(ctx, "SELECT id FROM gurus WHERE user_id = ? LIMIT 1", userID)
}

func (r *TrashDB) FirstWaliClass(ctx context.Context, waliID uuid.UUID) (*uuid.UUID, error) {
	return r.firstID(ctx, "SELECT class_id FROM students WHERE wali_id = ? ORDER BY created_at ASC, id ASC LIMIT 1", waliID)
}

func (r *TrashDB) FirstGuruClass(ctx context.Context, guruID uuid.UUID) (*uuid.UUID, error) {
	return r.firstID(ctx, `SELECT ct.class_id FROM class_teachers AS ct
		JOIN classes AS c ON c.id = ct.class_id
		WHERE ct.guru_id = ? ORDER BY c.created_at ASC, c.id ASC LIMIT 1`, guruID)
}

func (r *TrashDB) WaliHasStudentInClass(ctx context.Context, waliID uuid.UUID, classID uuid.UUID) (bool, error) {
	var total int64

	err := r.db.WithContext(ctx).Model(&entity.Student{}).
		Where("wali_id = ? AND class_id = ?", waliID, classID).Count(&total).Error

	return total > 0, err
}

func (r *TrashDB) GuruTeachesClass(ctx context.Context, guruID uuid.UUID, classID uuid.UUID) (bool, error) {
	var total int64

	err := r.db.WithContext(ctx).Model(&entity.ClassTeacher{}).
		Where("guru_id = ? AND class_id = ?", guruID, classID).Count(&total).Error

	return total > 0, err
}

func (r *TrashDB) FindOwnedStudent(ctx context.Context, waliID uuid.UUID, studentID uuid.UUID) (*entity.Student, error) {
	var student entity.Student

	err := r.db.WithContext(ctx).Where("id = ? AND wali_id = ?", studentID, waliID).Take(&student).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &student, nil
}

func (r *TrashDB) CountScans(ctx context.Context, studentID uuid.UUID, from time.Time, to time.Time) (int, error) {
	return countScans(r.db.WithContext(ctx), studentID, from, to)
}

func (r *TrashDB) Claim(
	ctx context.Context, scan *entity.TrashScan, classID uuid.UUID, limit int, from time.Time, to time.Time,
) (ClaimResult, error) {
	var result ClaimResult

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := ranking.Lock(tx, classID)
		if err != nil {
			return err
		}

		used, err := countScans(tx, scan.StudentID, from, to)
		if err != nil {
			return err
		}

		if used >= limit {
			return apperror.ErrDailyLimitReached
		}

		err = tx.Create(scan).Error
		if err != nil {
			return err
		}

		err = tx.Model(&entity.Student{}).
			Where("id = ?", scan.StudentID).
			UpdateColumn("current_points", gorm.Expr("current_points + ?", scan.PointsAwarded)).Error
		if err != nil {
			return err
		}

		err = ranking.Recompute(tx, classID)
		if err != nil {
			return err
		}

		var student entity.Student

		err = tx.Select("current_points", "rank_position").Where("id = ?", scan.StudentID).Take(&student).Error
		if err != nil {
			return err
		}

		result = ClaimResult{
			TotalPoints: student.CurrentPoints,
			Rank:        student.RankPosition,
			Remaining:   limit - used - 1,
		}

		return nil
	})

	return result, err
}

func (r *TrashDB) ListClassStudents(ctx context.Context, classID uuid.UUID) ([]entity.Student, error) {
	var students []entity.Student

	err := r.db.WithContext(ctx).
		Select("id", "full_name", "avatar_url", "current_points").
		Where("class_id = ?", classID).
		Order("current_points DESC, full_name ASC, id ASC").
		Find(&students).Error

	return students, err
}

func (r *TrashDB) firstID(ctx context.Context, query string, args ...any) (*uuid.UUID, error) {
	var ids []uuid.UUID

	err := r.db.WithContext(ctx).Raw(query, args...).Scan(&ids).Error
	if err != nil {
		return nil, err
	}

	if len(ids) == 0 {
		return nil, nil
	}

	return &ids[0], nil
}

func countScans(db *gorm.DB, studentID uuid.UUID, from time.Time, to time.Time) (int, error) {
	var total int64

	err := db.Model(&entity.TrashScan{}).
		Where("student_id = ? AND scanned_at >= ? AND scanned_at < ?", studentID, from, to).
		Count(&total).Error

	return int(total), err
}
