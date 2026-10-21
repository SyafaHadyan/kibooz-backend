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
	TotalPoints  int
	SeasonPoints int
	Rank         int
	Remaining    int
}

// HistoryRow is one saved place of a semester that ended
type HistoryRow struct {
	SeasonLabel  string
	RankPosition int
	StudentName  string
	Points       int
}

type TrashDBItf interface {
	FindWaliIDByUserID(ctx context.Context, userID uuid.UUID) (*uuid.UUID, error)
	FindGuruIDByUserID(ctx context.Context, userID uuid.UUID) (*uuid.UUID, error)
	FindOwnedStudent(ctx context.Context, waliID uuid.UUID, studentID uuid.UUID) (*entity.Student, error)
	CountScans(ctx context.Context, studentID uuid.UUID, from time.Time, to time.Time) (int, error)
	// Claim checks the daily limit, stores the scan, adds the points and renumbers the class in one transaction
	Claim(ctx context.Context, scan *entity.TrashScan, classID uuid.UUID, limit int, from time.Time, to time.Time) (ClaimResult, error)
	ListClassStudents(ctx context.Context, classID uuid.UUID) ([]entity.Student, error)
	// ListClassStudentsBetween ranks the children of a class by the points of the scans in the window, with SeasonPoints holding those points
	ListClassStudentsBetween(ctx context.Context, classID uuid.UUID, from time.Time, to time.Time) ([]entity.Student, error)
	// ListHistorySeasons returns the labels of the semesters that ended and left standings in the class, newest first
	ListHistorySeasons(ctx context.Context, classID uuid.UUID, limit int, offset int) ([]string, int, error)
	// ListHistoryEntries returns the saved standings of the given semesters, newest semester first and best place first
	ListHistoryEntries(ctx context.Context, classID uuid.UUID, labels []string) ([]HistoryRow, error)
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
	return r.firstID(ctx, "SELECT id FROM walis WHERE user_id = ? AND deleted_at IS NULL LIMIT 1", userID)
}

func (r *TrashDB) FindGuruIDByUserID(ctx context.Context, userID uuid.UUID) (*uuid.UUID, error) {
	return r.firstID(ctx, "SELECT id FROM gurus WHERE user_id = ? AND deleted_at IS NULL LIMIT 1", userID)
}

func (r *TrashDB) FirstWaliClass(ctx context.Context, waliID uuid.UUID) (*uuid.UUID, error) {
	return r.firstID(ctx, "SELECT class_id FROM students WHERE wali_id = ? AND deleted_at IS NULL ORDER BY created_at ASC, id ASC LIMIT 1", waliID)
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

		// the class is part of the match, so a child that moved while this request waited for the lock is not credited in the new class
		update := tx.Model(&entity.Student{}).
			Where("id = ? AND class_id = ?", scan.StudentID, classID).
			UpdateColumns(map[string]any{
				"current_points": gorm.Expr("current_points + ?", scan.PointsAwarded),
				"season_points":  gorm.Expr("season_points + ?", scan.PointsAwarded),
			})
		if update.Error != nil {
			return update.Error
		}

		// the child was deleted after the ownership check, the transaction drops the scan again
		if update.RowsAffected == 0 {
			return apperror.ErrStudentNotFound
		}

		err = ranking.Recompute(tx, classID)
		if err != nil {
			return err
		}

		var student entity.Student

		err = tx.Select("current_points", "season_points", "rank_position").Where("id = ?", scan.StudentID).Take(&student).Error
		if err != nil {
			return err
		}

		result = ClaimResult{
			TotalPoints:  student.CurrentPoints,
			SeasonPoints: student.SeasonPoints,
			Rank:         student.RankPosition,
			Remaining:    limit - used - 1,
		}

		return nil
	})

	return result, err
}

func (r *TrashDB) ListClassStudents(ctx context.Context, classID uuid.UUID) ([]entity.Student, error) {
	var students []entity.Student

	err := r.db.WithContext(ctx).
		Select("id", "full_name", "avatar_url", "season_points").
		Where("class_id = ?", classID).
		Order("season_points DESC, full_name ASC, id ASC").
		Find(&students).Error

	return students, err
}

// ListClassStudentsBetween keeps every active child of the class in the list, a child without a scan in the window has zero points,
// and orders them the way the semester board does
func (r *TrashDB) ListClassStudentsBetween(ctx context.Context, classID uuid.UUID, from time.Time, to time.Time) ([]entity.Student, error) {
	var students []entity.Student

	err := r.db.WithContext(ctx).
		Table("students AS s").
		Select("s.id, s.full_name, s.avatar_url, COALESCE(SUM(t.points_awarded), 0) AS season_points").
		Joins("LEFT JOIN enrollments AS e ON e.student_id = s.id AND e.status = 'active'").
		Joins("LEFT JOIN trash_scans AS t ON t.student_id = s.id AND t.scanned_at >= ? AND t.scanned_at < ? "+
			"AND t.scanned_at >= COALESCE(e.activated_at, '-infinity'::timestamptz)", from, to).
		Where("s.class_id = ? AND s.deleted_at IS NULL", classID).
		Group("s.id").
		Order("COALESCE(SUM(t.points_awarded), 0) DESC, s.full_name ASC, s.id ASC").
		Scan(&students).Error

	return students, err
}

// A semester is listed for as long as the class has standings saved under it, even when every child in them was deleted
// since, so the pages and the total do not shift when an account goes
const historySeasons = `FROM leaderboard_history AS h WHERE h.class_id = ?`

// The standings keep the name of a child as it was when the semester ended, so a child whose account was deleted since is
// left out of them and no name outlives the account
const historyJoin = `FROM leaderboard_history AS h
	JOIN students AS s ON s.id = h.student_id AND s.deleted_at IS NULL
	WHERE h.class_id = ?`

func (r *TrashDB) ListHistorySeasons(ctx context.Context, classID uuid.UUID, limit int, offset int) ([]string, int, error) {
	var total int64

	err := r.db.WithContext(ctx).Raw("SELECT COUNT(DISTINCT h.season_label) "+historySeasons, classID).Scan(&total).Error
	if err != nil {
		return nil, 0, err
	}

	var labels []string

	err = r.db.WithContext(ctx).
		Raw("SELECT DISTINCT h.season_label "+historySeasons+" ORDER BY h.season_label DESC LIMIT ? OFFSET ?", classID, limit, offset).
		Scan(&labels).Error
	if err != nil {
		return nil, 0, err
	}

	return labels, int(total), nil
}

func (r *TrashDB) ListHistoryEntries(ctx context.Context, classID uuid.UUID, labels []string) ([]HistoryRow, error) {
	if len(labels) == 0 {
		return nil, nil
	}

	var rows []HistoryRow

	err := r.db.WithContext(ctx).
		Raw(
			"SELECT h.season_label, h.rank_position, h.student_name, h.points "+historyJoin+
				" AND h.season_label IN ? ORDER BY h.season_label DESC, h.rank_position ASC, h.student_name ASC, h.id ASC",
			classID, labels,
		).
		Scan(&rows).Error

	return rows, err
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
