// Package repository handles the wali related database operations
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/childremoval"
	"github.com/SyafaHadyan/kibooz-backend/internal/classteam"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/enrollment"
	"github.com/SyafaHadyan/kibooz-backend/internal/filecleanup"
	"github.com/SyafaHadyan/kibooz-backend/internal/ranking"
)

const uniqueViolation = "23505"

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

// ProfileUpdate holds the profile fields to change, a Set flag tells a missing field from one that is cleared
type ProfileUpdate struct {
	SetName     bool
	FullName    string
	SetPhone    bool
	Phone       *string
	SetWhatsapp bool
	Whatsapp    *string
	SetAddress  bool
	Address     *string
}

// StudentUpdate holds the fields of a child to change, a Set flag tells a missing field from one that is given
type StudentUpdate struct {
	SetName  bool
	FullName string
	SetNISN  bool
	NISN     string
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
	// SumTrashByTypeBetween is SumTrashByType for the scans in a window
	SumTrashByTypeBetween(ctx context.Context, studentID uuid.UUID, from time.Time, to time.Time) (map[constants.TrashType]TrashTotals, error)
	CountScans(ctx context.Context, studentID uuid.UUID, from time.Time, to time.Time) (int, error)
	// FindGuardian returns nil when the account or the parent profile does not exist
	FindGuardian(ctx context.Context, userID uuid.UUID) (*GuardianRow, error)
	// UpdateProfile stores the changed fields of the account and of the parent profile in one transaction
	UpdateProfile(ctx context.Context, userID uuid.UUID, waliID uuid.UUID, update ProfileUpdate) error
	CreateGuidanceApplication(ctx context.Context, application *entity.GuidanceApplication) error
	// UpdateStudent changes the name or the NISN of a child of the wali. A new name is also written into the standings of
	// the semesters that ended, and the ranking of the class is renumbered. It returns the class of the child, and false
	// when the wali has no such active child, and apperror.ErrNISNTaken when another active child has the NISN.
	UpdateStudent(ctx context.Context, waliID uuid.UUID, studentID uuid.UUID, update StudentUpdate) (classID uuid.UUID, found bool, err error)
	// RemoveStudent hides the active child of the wali, renumbers the ranking of the class and queues the avatar of the
	// child and the photos of its scans for deletion in the same transaction. It returns the class of the child, and
	// false when the wali has no such active child.
	RemoveStudent(ctx context.Context, waliID uuid.UUID, studentID uuid.UUID) (classID uuid.UUID, removed bool, err error)
	// ListChildren returns the children of the wali, the oldest first
	ListChildren(ctx context.Context, waliID uuid.UUID) ([]StudentRow, error)
	// AddChild stores student as one more child of the wali inside the class that owns joinCode. It refuses a code that
	// nobody has, a class without an active teacher, and a parent who already has maxChildren children.
	AddChild(ctx context.Context, waliID uuid.UUID, joinCode string, student *entity.Student, maxChildren int) (classID uuid.UUID, err error)
	// RequestMove asks for a child of the wali to move to the class that owns joinCode, see package enrollment for what it refuses
	RequestMove(ctx context.Context, waliID uuid.UUID, studentID uuid.UUID, joinCode string) (enrollment.ParentRow, error)
	// CancelMove withdraws a request that no teacher has decided yet, and answers apperror.ErrMoveNotFound when there is none
	CancelMove(ctx context.Context, waliID uuid.UUID, studentID uuid.UUID, requestID uuid.UUID) error
	// ListMoves returns the requests of a child that wait or were rejected, the newest first
	ListMoves(ctx context.Context, waliID uuid.UUID, studentID uuid.UUID) ([]enrollment.ParentRow, error)
}

type WaliDB struct {
	db *gorm.DB
	// fileKey picks the files of this storage out of the addresses that a removed child held
	fileKey filecleanup.KeyFunc
}

func NewWaliDB(db *gorm.DB, fileKey filecleanup.KeyFunc) WaliDBItf {
	return &WaliDB{db: db, fileKey: fileKey}
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
		Order("recorded_at DESC, id DESC").
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

// sinceJoined keeps the scans that a child made in the class it is in now. A child that moved starts from zero in the new class, and
// what it scanned before is saved on the enrollment that ended.
const sinceJoined = "scanned_at >= COALESCE((SELECT activated_at FROM enrollments WHERE student_id = ? AND status = 'active'), '-infinity'::timestamptz)"

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
		Where(sinceJoined, studentID).
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
	return r.sumTrash(ctx, studentID, nil)
}

func (r *WaliDB) SumTrashByTypeBetween(
	ctx context.Context, studentID uuid.UUID, from time.Time, to time.Time,
) (map[constants.TrashType]TrashTotals, error) {
	return r.sumTrash(ctx, studentID, &[2]time.Time{from, to})
}

// sumTrash adds up the scans of a child per category, over all time or inside a window of [from, to)
func (r *WaliDB) sumTrash(ctx context.Context, studentID uuid.UUID, window *[2]time.Time) (map[constants.TrashType]TrashTotals, error) {
	type row struct {
		TrashType constants.TrashType
		Scans     int
		Points    int
	}

	var rows []row

	query := r.db.WithContext(ctx).
		Model(&entity.TrashScan{}).
		Select("trash_type, COUNT(*) AS scans, COALESCE(SUM(points_awarded), 0) AS points").
		Where("student_id = ?", studentID).
		Where(sinceJoined, studentID)

	if window != nil {
		query = query.Where("scanned_at >= ? AND scanned_at < ?", window[0], window[1])
	}

	err := query.
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

func (r *WaliDB) UpdateProfile(ctx context.Context, userID uuid.UUID, waliID uuid.UUID, update ProfileUpdate) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		account := map[string]any{}
		if update.SetName {
			account["full_name"] = update.FullName
		}

		if update.SetPhone {
			account["phone_number"] = update.Phone
		}

		if len(account) > 0 {
			err := tx.Model(&entity.User{}).Where("id = ?", userID).Updates(account).Error
			if err != nil {
				return err
			}
		}

		profile := map[string]any{}
		if update.SetWhatsapp {
			profile["whatsapp_number"] = update.Whatsapp
		}

		if update.SetAddress {
			profile["address"] = update.Address
		}

		if len(profile) == 0 {
			return nil
		}

		return tx.Model(&entity.Wali{}).Where("id = ?", waliID).Updates(profile).Error
	})
}

func (r *WaliDB) UpdateStudent(
	ctx context.Context, waliID uuid.UUID, studentID uuid.UUID, update StudentUpdate,
) (uuid.UUID, bool, error) {
	var (
		classID uuid.UUID
		found   bool
	)

	err := ranking.Retry(func() error {
		return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// a retried transaction starts from nothing
			classID, found = uuid.Nil, false

			var classes []uuid.UUID

			err := tx.Model(&entity.Student{}).Where("id = ? AND wali_id = ?", studentID, waliID).Pluck("class_id", &classes).Error
			if err != nil || len(classes) == 0 {
				return err
			}

			// the class lock comes before the change, because a new name moves the child in the ranking and the semester
			// rollover copies the names under the same lock
			err = ranking.Lock(tx, classes[0])
			if err != nil {
				return err
			}

			// a move may have taken the child to another class while this waited for the lock, then it starts again
			var current []uuid.UUID

			err = tx.Model(&entity.Student{}).Where("id = ? AND wali_id = ?", studentID, waliID).Pluck("class_id", &current).Error
			if err != nil || len(current) == 0 {
				return err
			}

			if current[0] != classes[0] {
				return ranking.ErrMoved
			}

			fields := map[string]any{}
			if update.SetName {
				fields["full_name"] = update.FullName
			}

			if update.SetNISN {
				fields["nisn"] = update.NISN
			}

			changed := tx.Model(&entity.Student{}).Where("id = ? AND wali_id = ?", studentID, waliID).Updates(fields)
			if changed.Error != nil || changed.RowsAffected == 0 {
				return changed.Error
			}

			if update.SetName {
				err = tx.Exec("UPDATE leaderboard_history SET student_name = ? WHERE student_id = ?", update.FullName, studentID).Error
				if err != nil {
					return err
				}

				err = ranking.Recompute(tx, classes[0])
				if err != nil {
					return err
				}
			}

			classID, found = classes[0], true

			return nil
		})
	})

	return classID, found, mapNISNTaken(err)
}

func (r *WaliDB) RemoveStudent(ctx context.Context, waliID uuid.UUID, studentID uuid.UUID) (uuid.UUID, bool, error) {
	var (
		classID uuid.UUID
		removed bool
	)

	err := ranking.Retry(func() error {
		return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// a retried transaction starts from nothing
			classID, removed = uuid.Nil, false

			var err error

			classID, removed, err = childremoval.Remove(tx, studentID, func(rows *gorm.DB) *gorm.DB {
				return rows.Where("wali_id = ?", waliID)
			}, r.fileKey)

			return err
		})
	})

	return classID, removed, err
}

func (r *WaliDB) ListChildren(ctx context.Context, waliID uuid.UUID) ([]StudentRow, error) {
	var rows []StudentRow

	err := r.db.WithContext(ctx).
		Table("students AS s").
		Select("s.*, c.name AS class_name, c.grade_level AS grade_level, c.school_name AS school_name, c.academic_year AS academic_year").
		Joins("JOIN classes AS c ON c.id = s.class_id").
		Where("s.wali_id = ? AND s.deleted_at IS NULL", waliID).
		Order("s.created_at ASC, s.id ASC").
		Scan(&rows).Error

	return rows, err
}

func (r *WaliDB) AddChild(
	ctx context.Context, waliID uuid.UUID, joinCode string, student *entity.Student, maxChildren int,
) (uuid.UUID, error) {
	var classID uuid.UUID

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var class entity.Class

		// the code comes first, so a wrong guess is answered the same way whatever the parent already has
		err := tx.Where("join_code = ? AND archived_at IS NULL", joinCode).Take(&class).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperror.ErrClassCodeNotFound
		}

		if err != nil {
			return err
		}

		classID = class.ID

		// the teachers of the class are counted under the lock that a teacher takes when leaving, so a child cannot join
		// while the class is being archived
		err = classteam.LockOpen(tx, class.ID, joinCode)
		if err != nil {
			return err
		}

		// a class whose teachers were all deleted keeps its code but must not take in new children
		var teachers int64

		err = tx.Table("class_teachers AS ct").
			Joins("JOIN gurus AS g ON g.id = ct.guru_id AND g.deleted_at IS NULL").
			Where("ct.class_id = ?", class.ID).
			Count(&teachers).Error
		if err != nil {
			return err
		}

		if teachers == 0 {
			return apperror.ErrClassNoTeacher
		}

		err = classteam.CheckRoom(tx, class.ID, constants.MaxChildrenPerClass)
		if err != nil {
			return err
		}

		// The lock on the parent makes two requests of the same parent count the children one after the other. A parent that
		// was deleted while this request waited matches nothing, and the child must not be added to a parent that is gone.
		var parent entity.Wali

		locked := tx.Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
			Select("id").Where("id = ?", waliID).Limit(1).Find(&parent)
		if locked.Error != nil {
			return locked.Error
		}

		if locked.RowsAffected == 0 {
			return apperror.ErrProfileNotFound
		}

		var children int64

		err = tx.Model(&entity.Student{}).Where("wali_id = ?", waliID).Count(&children).Error
		if err != nil {
			return err
		}

		if int(children) >= maxChildren {
			return apperror.ErrChildLimitReached
		}

		student.WaliID = waliID
		student.ClassID = class.ID

		err = tx.Create(student).Error
		if err != nil {
			return err
		}

		return ranking.Recompute(tx, class.ID)
	})

	return classID, mapNISNTaken(err)
}

// mapNISNTaken turns the unique violation of the NISN into its API error and leaves any other error as it is
func mapNISNTaken(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == "students_nisn_key" {
		return apperror.ErrNISNTaken
	}

	return err
}

func (r *WaliDB) RequestMove(
	ctx context.Context, waliID uuid.UUID, studentID uuid.UUID, joinCode string,
) (enrollment.ParentRow, error) {
	var row enrollment.ParentRow

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error

		row, err = enrollment.Request(tx, waliID, studentID, joinCode)

		return err
	})

	return row, err
}

func (r *WaliDB) CancelMove(ctx context.Context, waliID uuid.UUID, studentID uuid.UUID, requestID uuid.UUID) error {
	return enrollment.Cancel(r.db.WithContext(ctx), waliID, studentID, requestID)
}

func (r *WaliDB) ListMoves(ctx context.Context, waliID uuid.UUID, studentID uuid.UUID) ([]enrollment.ParentRow, error) {
	return enrollment.ListForChild(r.db.WithContext(ctx), waliID, studentID)
}
