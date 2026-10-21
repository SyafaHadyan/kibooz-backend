// Package enrollment moves a child from one class to another. A parent asks with the join code of the new class, a teacher of
// the new class accepts or rejects, and an accepted move ends the old enrollment and starts the new one in one transaction, see
// docs/adr/0014-class-enrollments.md. The class in students always names the class of the one active enrollment.
package enrollment

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/classteam"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/ranking"
)

const (
	uniqueViolation = "23505"
	pendingKey      = "enrollments_pending_child_key"

	// maxListed bounds the requests that the parent of a child sees, the newest first
	maxListed = 20
)

// ParentRow is a request as the parent of the child sees it
type ParentRow struct {
	ID          uuid.UUID
	StudentID   uuid.UUID
	ClassID     uuid.UUID
	ClassName   string
	Status      constants.EnrollmentStatus
	RequestedAt time.Time
	DecidedAt   *time.Time
}

// ClassRow is a request that waits, as a teacher of the class sees it
type ClassRow struct {
	ID          uuid.UUID
	StudentID   uuid.UUID
	StudentName string
	RequestedAt time.Time
}

// Request opens a request to move the child of a parent to the class that owns joinCode. It refuses a child that is not the
// parent's, a code that nobody has, a class that does not take in children right now, the class the child is already in,
// a full class and a child that already has a request that waits.
func Request(tx *gorm.DB, waliID uuid.UUID, studentID uuid.UUID, joinCode string) (ParentRow, error) {
	var child entity.Student

	found := tx.Select("id", "class_id").Where("id = ? AND wali_id = ?", studentID, waliID).Limit(1).Find(&child)
	if found.Error != nil {
		return ParentRow{}, found.Error
	}

	if found.RowsAffected == 0 {
		return ParentRow{}, apperror.ErrStudentNotFound
	}

	// the code comes next, so a wrong guess is answered the same way whatever the child already has
	var class entity.Class

	err := tx.Where("join_code = ? AND archived_at IS NULL", joinCode).Take(&class).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ParentRow{}, apperror.ErrClassCodeNotFound
	}

	if err != nil {
		return ParentRow{}, err
	}

	err = classteam.LockOpen(tx, class.ID, joinCode)
	if err == nil {
		err = classteam.CheckTeacher(tx, class.ID)
	}

	if err != nil {
		return ParentRow{}, err
	}

	// the teachers lock comes first, as it does when a child is added, and then the lock on the child keeps it from being deleted
	// while the request is written, because a deleted child has no request that waits
	locked := tx.Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
		Select("id", "class_id").Where("id = ? AND wali_id = ?", studentID, waliID).Limit(1).Find(&child)
	if locked.Error != nil {
		return ParentRow{}, locked.Error
	}

	if locked.RowsAffected == 0 {
		return ParentRow{}, apperror.ErrStudentNotFound
	}

	if class.ID == child.ClassID {
		return ParentRow{}, apperror.ErrAlreadyInClass
	}

	err = classteam.CheckRoom(tx, class.ID, constants.MaxChildrenPerClass)
	if err != nil {
		return ParentRow{}, err
	}

	request := entity.Enrollment{StudentID: studentID, ClassID: class.ID, Status: constants.EnrollmentPending}
	request.ID = uuid.New()

	err = tx.Create(&request).Error
	if err != nil {
		return ParentRow{}, mapPending(err)
	}

	return ParentRow{
		ID: request.ID, StudentID: studentID, ClassID: class.ID, ClassName: class.Name,
		Status: request.Status, RequestedAt: request.RequestedAt,
	}, nil
}

// mapPending turns the unique violation of the one request that waits into its API error
func mapPending(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == pendingKey {
		return apperror.ErrMovePending
	}

	return err
}

// Cancel withdraws a request of the parent that no teacher has decided yet
func Cancel(tx *gorm.DB, waliID uuid.UUID, studentID uuid.UUID, requestID uuid.UUID) error {
	res := tx.Exec(`DELETE FROM enrollments AS e USING students AS s
		WHERE e.id = ? AND e.student_id = ? AND e.status = 'pending' AND s.id = e.student_id AND s.wali_id = ? AND s.deleted_at IS NULL`,
		requestID, studentID, waliID)
	if res.Error != nil {
		return res.Error
	}

	if res.RowsAffected == 0 {
		return apperror.ErrMoveNotFound
	}

	return nil
}

// ListForChild returns the requests of a child that wait or were rejected, the newest first
func ListForChild(tx *gorm.DB, waliID uuid.UUID, studentID uuid.UUID) ([]ParentRow, error) {
	var owned int64

	err := tx.Model(&entity.Student{}).Where("id = ? AND wali_id = ?", studentID, waliID).Count(&owned).Error
	if err != nil {
		return nil, err
	}

	if owned == 0 {
		return nil, apperror.ErrStudentNotFound
	}

	var rows []ParentRow

	err = tx.Table("enrollments AS e").
		Select("e.id, e.student_id, e.class_id, c.name AS class_name, e.status, e.requested_at, e.decided_at").
		Joins("JOIN classes AS c ON c.id = e.class_id").
		Where("e.student_id = ? AND e.status IN ?", studentID, []constants.EnrollmentStatus{constants.EnrollmentPending, constants.EnrollmentRejected}).
		Order("e.requested_at DESC, e.id DESC").
		Limit(maxListed).
		Scan(&rows).Error

	return rows, err
}

// ListForClass returns the requests that wait for a teacher of the class, the oldest first
func ListForClass(tx *gorm.DB, classID uuid.UUID) ([]ClassRow, error) {
	var rows []ClassRow

	err := tx.Table("enrollments AS e").
		Select("e.id, e.student_id, s.full_name AS student_name, e.requested_at").
		Joins("JOIN students AS s ON s.id = e.student_id AND s.deleted_at IS NULL").
		Where("e.class_id = ? AND e.status = ?", classID, constants.EnrollmentPending).
		Order("e.requested_at ASC, e.id ASC").
		Scan(&rows).Error

	return rows, err
}

// Reject turns down a request that waits for the class
func Reject(tx *gorm.DB, classID uuid.UUID, requestID uuid.UUID, guruID uuid.UUID) error {
	res := tx.Exec(
		"UPDATE enrollments SET status = 'rejected', decided_at = ?, decided_by_guru_id = ? WHERE id = ? AND class_id = ? AND status = 'pending'",
		time.Now(), guruID, requestID, classID,
	)
	if res.Error != nil {
		return res.Error
	}

	if res.RowsAffected == 0 {
		return apperror.ErrMoveNotFound
	}

	return nil
}

// Accept moves the child of a request that waits for the class into it, and returns the class that the child left. The class of
// the child, its points of all semesters and of the semester, and its rank all start from zero in the new class. What the child had
// in the old class is saved on the enrollment that ends. The ranking locks of both classes are taken in the order of their ids, so
// two moves in opposite directions cannot wait for each other.
func Accept(tx *gorm.DB, classID uuid.UUID, requestID uuid.UUID, guruID uuid.UUID) (uuid.UUID, error) {
	// The row of the teacher comes first, which is the order that a teacher who leaves takes the locks in, and the request is decided
	// in the name of this row. A teacher that was deleted just before cannot decide.
	var guruRows []uuid.UUID

	err := tx.Raw("SELECT id FROM gurus WHERE id = ? AND deleted_at IS NULL FOR KEY SHARE", guruID).Scan(&guruRows).Error
	if err != nil {
		return uuid.Nil, err
	}

	if len(guruRows) == 0 {
		return uuid.Nil, apperror.ErrClassNotFound
	}

	// the teachers lock of the class serializes the move with a teacher who leaves, with a change of joining and with an archive
	archived, err := classteam.LockLive(tx, classID)
	if err != nil {
		return uuid.Nil, err
	}

	if archived {
		return uuid.Nil, apperror.ErrMoveNotFound
	}

	// a teacher that was removed from the class while this waited for the lock does not decide for it any more
	var teaching int64

	err = tx.Table("class_teachers").Where("class_id = ? AND guru_id = ?", classID, guruID).Count(&teaching).Error
	if err != nil {
		return uuid.Nil, err
	}

	if teaching == 0 {
		return uuid.Nil, apperror.ErrClassNotFound
	}

	var waiting entity.Enrollment

	err = tx.Where("id = ? AND class_id = ? AND status = ?", requestID, classID, constants.EnrollmentPending).Take(&waiting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return uuid.Nil, apperror.ErrMoveNotFound
	}

	if err != nil {
		return uuid.Nil, err
	}

	var oldClasses []uuid.UUID

	err = tx.Model(&entity.Student{}).Where("id = ?", waiting.StudentID).Pluck("class_id", &oldClasses).Error
	if err != nil {
		return uuid.Nil, err
	}

	if len(oldClasses) == 0 {
		return uuid.Nil, apperror.ErrMoveNotFound
	}

	oldClassID := oldClasses[0]

	err = lockBoth(tx, oldClassID, classID)
	if err != nil {
		return uuid.Nil, err
	}

	// everything is read again under the locks, a request that was cancelled or a child that was deleted while this waited is gone
	var child entity.Student

	found := tx.Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
		Where("id = ? AND class_id = ?", waiting.StudentID, oldClassID).Limit(1).Find(&child)
	if found.Error != nil {
		return uuid.Nil, found.Error
	}

	var waitingIDs []uuid.UUID

	err = tx.Raw("SELECT id FROM enrollments WHERE id = ? AND status = 'pending' FOR UPDATE", requestID).Scan(&waitingIDs).Error
	if err != nil {
		return uuid.Nil, err
	}

	if found.RowsAffected == 0 || len(waitingIDs) == 0 {
		return uuid.Nil, apperror.ErrMoveNotFound
	}

	if oldClassID == classID {
		return uuid.Nil, apperror.ErrAlreadyInClass
	}

	err = classteam.CheckRoom(tx, classID, constants.MaxChildrenPerClass)
	if err != nil {
		return uuid.Nil, err
	}

	err = swap(tx, &child, classID, requestID, guruID, time.Now())
	if err != nil {
		return uuid.Nil, err
	}

	err = ranking.Recompute(tx, oldClassID)
	if err == nil {
		err = ranking.Recompute(tx, classID)
	}

	return oldClassID, err
}

// swap ends the active enrollment of the child with what it had, starts the one that waited, and puts the child in the new class
// with nothing. The old one ends first because a child has at most one active enrollment.
func swap(tx *gorm.DB, child *entity.Student, classID uuid.UUID, requestID uuid.UUID, guruID uuid.UUID, now time.Time) error {
	err := tx.Exec(`UPDATE enrollments AS e
		SET status = 'ended', ended_at = ?, final_points = ?, final_season_points = ?, final_rank = ?,
			final_scans = (SELECT count(*) FROM trash_scans WHERE student_id = e.student_id AND scanned_at >= e.activated_at)
		WHERE e.student_id = ? AND e.status = 'active'`,
		now, child.CurrentPoints, child.SeasonPoints, child.RankPosition, child.ID).Error
	if err != nil {
		return err
	}

	err = tx.Model(&entity.Student{}).Where("id = ?", child.ID).UpdateColumns(map[string]any{
		"class_id": classID, "current_points": 0, "season_points": 0, "rank_position": 0,
	}).Error
	if err != nil {
		return err
	}

	return tx.Exec(
		"UPDATE enrollments SET status = 'active', decided_at = ?, decided_by_guru_id = ?, activated_at = ? WHERE id = ?",
		now, guruID, now, requestID,
	).Error
}

// lockBoth takes the ranking lock of two classes in the order of their ids
func lockBoth(tx *gorm.DB, a uuid.UUID, b uuid.UUID) error {
	first, second := a, b
	if b.String() < a.String() {
		first, second = b, a
	}

	err := ranking.Lock(tx, first)
	if err != nil || first == second {
		return err
	}

	return ranking.Lock(tx, second)
}

// PreviousRow is a class that a child was in, with what the child had in it when it left
type PreviousRow struct {
	ID           uuid.UUID
	ClassID      uuid.UUID
	ClassName    string
	GradeLevel   string
	SchoolName   string
	AcademicYear string
	ActivatedAt  time.Time
	EndedAt      time.Time
	FinalPoints  int
	FinalSeason  int
	FinalRank    int
	FinalScans   int
}

// ListEnded returns the classes that the child of a parent left, the latest first. A child that was never moved has none.
func ListEnded(tx *gorm.DB, waliID uuid.UUID, studentID uuid.UUID) ([]PreviousRow, error) {
	var owned int64

	err := tx.Model(&entity.Student{}).Where("id = ? AND wali_id = ?", studentID, waliID).Count(&owned).Error
	if err != nil {
		return nil, err
	}

	if owned == 0 {
		return nil, apperror.ErrStudentNotFound
	}

	return listEnded(tx, studentID)
}

func listEnded(tx *gorm.DB, studentID uuid.UUID) ([]PreviousRow, error) {
	var rows []PreviousRow

	err := tx.Table("enrollments AS e").
		Select(`e.id, e.class_id, c.name AS class_name, c.grade_level, c.school_name, c.academic_year, e.activated_at, e.ended_at,
			e.final_points, e.final_season_points AS final_season, e.final_rank, e.final_scans`).
		Joins("JOIN classes AS c ON c.id = e.class_id").
		Where("e.student_id = ? AND e.status = ?", studentID, constants.EnrollmentEnded).
		Order("e.ended_at DESC, e.id DESC").
		Scan(&rows).Error

	return rows, err
}
