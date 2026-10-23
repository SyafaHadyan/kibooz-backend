// Package repository handles the guru related database operations
package repository

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/childremoval"
	"github.com/SyafaHadyan/kibooz-backend/internal/classcode"
	"github.com/SyafaHadyan/kibooz-backend/internal/classteam"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/enrollment"
	"github.com/SyafaHadyan/kibooz-backend/internal/filecleanup"
	"github.com/SyafaHadyan/kibooz-backend/internal/ranking"
)

// GuruRow is a guru joined with the account columns shown on the dashboard and the profile
type GuruRow struct {
	entity.Guru `gorm:"embedded"`
	FullName    string
	AvatarURL   *string
	Email       string
	PhoneNumber *string
	JoinedAt    time.Time
}

// MoodDay is the local day that some mood logs fall on, and how many logs one child may have inside it
type MoodDay struct {
	From  time.Time
	To    time.Time
	Limit int
}

// TeacherRow is a teacher of a class together with the account columns and the time the teacher joined the class
type TeacherRow struct {
	entity.Guru `gorm:"embedded"`
	FullName    string
	AvatarURL   *string
	IsOwner     bool
	JoinedAt    time.Time
}

// teamRow is an active teacher of a class and whether the teacher owns it
type teamRow struct {
	GuruID  uuid.UUID
	IsOwner bool
}

// ClassRow is a class with the number of children in it
type ClassRow struct {
	entity.Class  `gorm:"embedded"`
	TotalStudents int
}

// ProfileUpdate holds the profile fields to change, a Set flag tells a missing field from one that is cleared
type ProfileUpdate struct {
	SetName    bool
	FullName   string
	SetPhone   bool
	Phone      *string
	SetAddress bool
	Address    *string
}

// MoodRecord is the minimal mood log projection used by aggregations
type MoodRecord struct {
	ID         uuid.UUID
	StudentID  uuid.UUID
	MoodType   constants.Mood
	RecordedAt time.Time
}

type GuruDBItf interface {
	// ListMoveRequests returns the requests to move a child into the class that wait for a teacher, the oldest first
	ListMoveRequests(ctx context.Context, classID uuid.UUID) ([]enrollment.ClassRow, error)
	// AcceptMove moves the child of a request that waits into the class and returns the class that the child left
	AcceptMove(ctx context.Context, classID uuid.UUID, requestID uuid.UUID, guruID uuid.UUID) (uuid.UUID, error)
	// ArchiveClass archives the class for its owner, which makes it read only. It answers apperror.ErrNotClassOwner for a teacher who is
	// not the owner, apperror.ErrClassNotFound for one who no longer teaches the class and apperror.ErrClassArchived for a class that is
	// archived already. Requests to move into the class that wait are rejected by the database.
	ArchiveClass(ctx context.Context, classID uuid.UUID, actorGuruID uuid.UUID) error
	// RestoreClass makes an archived class writable again for any of its teachers and gives it a new join code, which it returns. It
	// answers apperror.ErrClassNotArchived, apperror.ErrClassNotFound and apperror.ErrClassLimitReached when the teacher already teaches the
	// largest number of classes that are not archived.
	RestoreClass(ctx context.Context, classID uuid.UUID, actorGuruID uuid.UUID) (string, error)
	// StudentHistory returns what a child that is in the class did in the classes it left, and apperror.ErrStudentNotFound when the
	// child is not in the class
	StudentHistory(ctx context.Context, classID uuid.UUID, studentID uuid.UUID) ([]enrollment.HistoryRow, error)
	// RejectMove turns down a request that waits for the class
	RejectMove(ctx context.Context, classID uuid.UUID, requestID uuid.UUID, guruID uuid.UUID) error
	FindGuruByUserID(ctx context.Context, userID uuid.UUID) (*GuruRow, error)
	// FindTaughtClass returns the requested class when the guru teaches it, or the oldest taught class when classID is nil
	FindTaughtClass(ctx context.Context, guruID uuid.UUID, classID *uuid.UUID) (*entity.Class, error)
	CountStudents(ctx context.Context, classID uuid.UUID) (int, error)
	// ListMoodRecords returns the newest log of each child for each day in loc, between from and to. A log is newer by its
	// time and then by its id, which is the order the parent dashboard uses as well.
	ListMoodRecords(ctx context.Context, classID uuid.UUID, from time.Time, to time.Time, loc *time.Location) ([]MoodRecord, error)
	FindStudent(ctx context.Context, studentID uuid.UUID) (*entity.Student, error)
	TeachesClass(ctx context.Context, guruID uuid.UUID, classID uuid.UUID) (bool, error)
	// CreateMoodLogs stores every mood log of one class in one transaction, so a failure writes none of them. It takes the
	// ranking lock of the class first, counts the logs that each child already has inside day and writes nothing when one
	// of them has reached day.Limit, in which case it returns the ids of those children.
	CreateMoodLogs(ctx context.Context, classID uuid.UUID, moodLogs []entity.MoodLog, day MoodDay) (full []uuid.UUID, err error)
	UpdateProfile(ctx context.Context, userID uuid.UUID, guruID uuid.UUID, update ProfileUpdate) error
	ListTaughtClasses(ctx context.Context, guruID uuid.UUID, limit int, offset int) ([]ClassRow, int, error)
	// FindClassRow returns nil when the guru does not teach the class
	FindClassRow(ctx context.Context, guruID uuid.UUID, classID uuid.UUID) (*ClassRow, error)
	// CountClassContent returns how many learning videos and forum threads the class holds
	CountClassContent(ctx context.Context, classID uuid.UUID) (videos int, threads int, err error)
	// CreateClass stores the class under a new join code and makes the guru its teacher
	CreateClass(ctx context.Context, guruID uuid.UUID, class *entity.Class) error
	// RotateJoinCode gives the class a new join code on behalf of actorGuruID and returns it, the old code stops working at once.
	// The actor has to still teach the class once the lock is held, and otherwise it returns apperror.ErrClassNotFound, so a
	// teacher who was removed while the request waited does not get the new code.
	RotateJoinCode(ctx context.Context, classID uuid.UUID, actorGuruID uuid.UUID) (string, error)
	// SetJoining opens or closes the class for new children, and waits for a child who is joining right now
	SetJoining(ctx context.Context, classID uuid.UUID, open bool) error
	// ListTeachers returns the active teachers of the class, the one who joined first at the top
	ListTeachers(ctx context.Context, classID uuid.UUID) ([]TeacherRow, error)
	// AddTeacher makes the active teacher account with the email a teacher of the class, on behalf of actorGuruID. It returns
	// nil when no such account exists and apperror.ErrTeacherInClass when the account already teaches the class. The actor
	// has to still teach the class once the lock is held, and otherwise it returns apperror.ErrClassNotFound, so a
	// teacher who was removed while the request waited cannot add themselves again.
	AddTeacher(ctx context.Context, classID uuid.UUID, actorGuruID uuid.UUID, email string) (*TeacherRow, error)
	// RemoveTeacher takes target off the class on behalf of actor. The owner can remove anyone and any teacher can remove
	// themselves, and the owner who goes hands the class to the teacher who joined next. It returns
	// apperror.ErrTeacherNotInClass, apperror.ErrNotClassOwner or apperror.ErrLastTeacher when the change is refused.
	RemoveTeacher(ctx context.Context, classID uuid.UUID, actor uuid.UUID, target uuid.UUID) error
	// TransferOwnership makes target the owner of the class on behalf of actor, who has to be the current owner. It
	// returns the new owner, apperror.ErrNotClassOwner or apperror.ErrTeacherNotInClass.
	TransferOwnership(ctx context.Context, classID uuid.UUID, actor uuid.UUID, target uuid.UUID) (*TeacherRow, error)
	// RemoveStudent hides the active child of the class, renumbers the ranking of the class and queues the avatar of the
	// child and the photos of its trash scans for deletion in the same transaction. It returns false when the class has
	// no such active child.
	RemoveStudent(ctx context.Context, classID uuid.UUID, studentID uuid.UUID) (bool, error)
}

type GuruDB struct {
	db *gorm.DB
	// fileKey picks the files of this storage out of the addresses that a removed child held
	fileKey filecleanup.KeyFunc
}

func NewGuruDB(db *gorm.DB, fileKey filecleanup.KeyFunc) GuruDBItf {
	return &GuruDB{db: db, fileKey: fileKey}
}

func (r *GuruDB) FindGuruByUserID(ctx context.Context, userID uuid.UUID) (*GuruRow, error) {
	var rows []GuruRow

	err := r.db.WithContext(ctx).
		Table("gurus AS g").
		Select("g.*, u.full_name AS full_name, u.avatar_url AS avatar_url, u.email AS email, "+
			"u.phone_number AS phone_number, u.created_at AS joined_at").
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

	// without an id the oldest class that is not archived comes first, and an archived one only when nothing else is left
	err := query.Order("(c.archived_at IS NOT NULL) ASC, c.created_at ASC, c.id ASC").Limit(1).Scan(&classes).Error
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

func (r *GuruDB) ListMoodRecords(
	ctx context.Context, classID uuid.UUID, from time.Time, to time.Time, loc *time.Location,
) ([]MoodRecord, error) {
	var records []MoodRecord

	// Only the newest log of a child for a day is read, so a child with many logs in a day costs one row. A uuid compares
	// byte by byte in Postgres, like bytes.Compare in the use case. A child that moved only shows the moods that are dated since it joined
	// this class, and a child that never moved has no earlier class, so all its moods show, as the write side lets them be dated before.
	err := r.db.WithContext(ctx).Raw(`
SELECT DISTINCT ON (t.student_id, t.local_day) t.id, t.student_id, t.mood_type, t.recorded_at
FROM (
  SELECT m.id, m.student_id, m.mood_type, m.recorded_at, (m.recorded_at AT TIME ZONE ?)::date AS local_day
  FROM mood_logs AS m
  JOIN students AS s ON s.id = m.student_id
  JOIN enrollments AS e ON e.student_id = s.id AND e.class_id = s.class_id AND e.status = 'active'
  WHERE s.class_id = ? AND s.deleted_at IS NULL AND m.recorded_at >= ? AND m.recorded_at < ? AND (m.recorded_at >= e.activated_at OR NOT EXISTS (SELECT 1 FROM enrollments AS p WHERE p.student_id = s.id AND p.status = 'ended'))
) AS t
ORDER BY t.student_id, t.local_day, t.recorded_at DESC, t.id DESC`,
		loc.String(), classID, from, to,
	).Scan(&records).Error

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

func (r *GuruDB) CreateMoodLogs(
	ctx context.Context, classID uuid.UUID, moodLogs []entity.MoodLog, day MoodDay,
) ([]uuid.UUID, error) {
	if len(moodLogs) == 0 {
		return nil, nil
	}

	studentIDs := make([]uuid.UUID, 0, len(moodLogs))
	for i := range moodLogs {
		studentIDs = append(studentIDs, moodLogs[i].StudentID)
	}

	var full []uuid.UUID

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// the lock makes the count and the insert one step, so two requests cannot both pass the limit
		err := ranking.Lock(tx, classID)
		if err != nil {
			return err
		}

		err = classteam.CheckWritable(tx, classID)
		if err != nil {
			return err
		}

		// A mood is told apart from the moods of an earlier class by its date, so a child that moved cannot get a mood that is dated
		// before it joined its class. A child that never moved has no earlier class, and its moods may be dated as before.
		var joined []struct {
			StudentID   uuid.UUID
			ActivatedAt time.Time
		}

		err = tx.Raw(`SELECT e.student_id, e.activated_at FROM enrollments AS e
			WHERE e.student_id IN ? AND e.status = 'active'
				AND EXISTS (SELECT 1 FROM enrollments AS p WHERE p.student_id = e.student_id AND p.status = 'ended')`, studentIDs).Scan(&joined).Error
		if err != nil {
			return err
		}

		for i := range moodLogs {
			for _, row := range joined {
				if row.StudentID == moodLogs[i].StudentID && moodLogs[i].RecordedAt.Before(row.ActivatedAt) {
					return apperror.Validation(map[string]string{"recordedAt": "is before the child joined its class"})
				}
			}
		}

		var counts []struct {
			StudentID uuid.UUID
			Total     int
		}

		err = tx.Raw(
			"SELECT student_id, COUNT(*) AS total FROM mood_logs WHERE student_id IN ? AND recorded_at >= ? AND recorded_at < ? GROUP BY student_id",
			studentIDs, day.From, day.To,
		).Scan(&counts).Error
		if err != nil {
			return err
		}

		for _, row := range counts {
			if row.Total >= day.Limit {
				full = append(full, row.StudentID)
			}
		}

		if len(full) > 0 {
			return nil
		}

		return tx.Create(&moodLogs).Error
	})

	return full, err
}

func (r *GuruDB) UpdateProfile(ctx context.Context, userID uuid.UUID, guruID uuid.UUID, update ProfileUpdate) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if update.SetName {
			err := tx.Model(&entity.User{}).Where("id = ?", userID).Update("full_name", update.FullName).Error
			if err != nil {
				return err
			}
		}

		if update.SetPhone {
			err := tx.Model(&entity.User{}).Where("id = ?", userID).Update("phone_number", update.Phone).Error
			if err != nil {
				return err
			}
		}

		if update.SetAddress {
			return tx.Model(&entity.Guru{}).Where("id = ?", guruID).Update("address", update.Address).Error
		}

		return nil
	})
}

func (r *GuruDB) ListTaughtClasses(ctx context.Context, guruID uuid.UUID, limit int, offset int) ([]ClassRow, int, error) {
	var total int64

	err := r.db.WithContext(ctx).Model(&entity.ClassTeacher{}).Where("guru_id = ?", guruID).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	var rows []ClassRow

	err = r.classQuery(ctx).
		Where("ct.guru_id = ?", guruID).
		Order("c.created_at ASC, c.id ASC").
		Limit(limit).Offset(offset).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	return rows, int(total), nil
}

func (r *GuruDB) FindClassRow(ctx context.Context, guruID uuid.UUID, classID uuid.UUID) (*ClassRow, error) {
	var rows []ClassRow

	err := r.classQuery(ctx).Where("ct.guru_id = ? AND c.id = ?", guruID, classID).Limit(1).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	if len(rows) == 0 {
		return nil, nil
	}

	return &rows[0], nil
}

func (r *GuruDB) CountClassContent(ctx context.Context, classID uuid.UUID) (int, int, error) {
	var videos, threads int64

	err := r.db.WithContext(ctx).Model(&entity.LearningVideo{}).Where("class_id = ?", classID).Count(&videos).Error
	if err != nil {
		return 0, 0, err
	}

	err = r.db.WithContext(ctx).Model(&entity.ForumPost{}).Where("class_id = ? AND parent_id IS NULL", classID).Count(&threads).Error
	if err != nil {
		return 0, 0, err
	}

	return int(videos), int(threads), nil
}

func (r *GuruDB) CreateClass(ctx context.Context, guruID uuid.UUID, class *entity.Class) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// the row of the teacher is locked before the classes are counted, so two requests of one teacher count one after
		// the other. A class takes no lock that waits for this row, so the order is the one of AddTeacher.
		err := lockGuru(tx, guruID)
		if err != nil {
			return err
		}

		err = classteam.CheckClassRoom(tx, guruID, constants.MaxClassesPerTeacher)
		if err != nil {
			return err
		}

		err = classcode.Create(tx, class)
		if err != nil {
			return err
		}

		return tx.Create(&entity.ClassTeacher{ClassID: class.ID, GuruID: guruID, IsOwner: true}).Error
	})
}

// classQuery selects the classes of a teacher together with the number of children that are still active
func (r *GuruDB) RotateJoinCode(ctx context.Context, classID uuid.UUID, actorGuruID uuid.UUID) (string, error) {
	var code string

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// a join that is under way finishes first, and one that waits finds the old code gone
		err := classteam.Lock(tx, classID)
		if err != nil {
			return err
		}

		// A removal commits under this lock, so the teacher is checked again here. One who was removed meanwhile must neither
		// replace the code that the removal made nor read the new one.
		var stillTeaching int64

		// a teacher who deleted the account may keep a row of the class for the handover, so the account has to be active too
		err = tx.Table("class_teachers AS ct").
			Joins("JOIN gurus AS g ON g.id = ct.guru_id AND g.deleted_at IS NULL").
			Where("ct.class_id = ? AND ct.guru_id = ?", classID, actorGuruID).
			Count(&stillTeaching).Error
		if err == nil && stillTeaching == 0 {
			err = apperror.ErrClassNotFound
		}

		if err != nil {
			return err
		}

		code, err = classcode.Replace(tx, classID)

		return err
	})

	return code, err
}

func (r *GuruDB) SetJoining(ctx context.Context, classID uuid.UUID, open bool) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return classteam.SetJoining(tx, classID, open)
	})
}

func (r *GuruDB) classQuery(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).
		Table("classes AS c").
		Select("c.*, (SELECT COUNT(*) FROM students AS s WHERE s.class_id = c.id AND s.deleted_at IS NULL) AS total_students").
		Joins("JOIN class_teachers AS ct ON ct.class_id = c.id")
}

func teacherQuery(tx *gorm.DB) *gorm.DB {
	return tx.Table("class_teachers AS ct").
		Select("g.*, u.full_name AS full_name, u.avatar_url AS avatar_url, ct.is_owner AS is_owner, ct.created_at AS joined_at").
		Joins("JOIN gurus AS g ON g.id = ct.guru_id AND g.deleted_at IS NULL").
		Joins("JOIN users AS u ON u.id = g.user_id AND u.deleted_at IS NULL")
}

func (r *GuruDB) ListTeachers(ctx context.Context, classID uuid.UUID) ([]TeacherRow, error) {
	var rows []TeacherRow

	err := teacherQuery(r.db.WithContext(ctx)).
		Where("ct.class_id = ?", classID).
		Order("ct.created_at ASC, g.id ASC").
		Scan(&rows).Error

	return rows, err
}

// lockGuru locks the row of an active teacher until the end of the transaction. It answers apperror.ErrProfileNotFound
// when no active teacher has the id, for example when the account was deleted after the caller looked it up.
func lockGuru(tx *gorm.DB, guruID uuid.UUID) error {
	var ids []uuid.UUID

	err := tx.Model(&entity.Guru{}).
		Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
		Where("id = ?", guruID).
		Pluck("id", &ids).Error
	if err != nil {
		return err
	}

	if len(ids) == 0 {
		return apperror.ErrProfileNotFound
	}

	return nil
}

func (r *GuruDB) AddTeacher(ctx context.Context, classID uuid.UUID, actorGuruID uuid.UUID, email string) (*TeacherRow, error) {
	var added *TeacherRow

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// a retried transaction starts from nothing
		added = nil

		var guruIDs []uuid.UUID

		// the row of the new teacher is locked first and the class after it, which is the order of classteam.Leave, so a
		// teacher who deletes the account at the same moment cannot wait for this request while this one waits for the class
		err := tx.Table("users AS u").
			Select("g.id").
			Joins("JOIN gurus AS g ON g.user_id = u.id AND g.deleted_at IS NULL").
			Where("u.email = ? AND u.role = ? AND u.deleted_at IS NULL", email, constants.RoleGuru).
			Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate, Table: clause.Table{Name: "g"}}).
			Limit(1).
			Scan(&guruIDs).Error
		if err != nil {
			return err
		}

		if len(guruIDs) == 0 {
			return nil
		}

		// the row stays locked until the end, so a teacher who is added to two classes at once is counted one after the other
		err = classteam.CheckClassRoom(tx, guruIDs[0], constants.MaxClassesPerTeacher)
		if err != nil {
			return err
		}

		// a teacher who leaves archives a class that nobody else teaches under this lock, and a teacher must not be added to
		// a class that was archived while this request waited
		archived, err := classteam.LockLive(tx, classID)
		if err != nil {
			return err
		}

		if archived {
			return apperror.ErrClassNotFound
		}

		// The request checked that the actor teaches the class before it waited for this lock. A removal commits under the
		// same lock, so the actor is checked again here, and one who was removed meanwhile must not bring a teacher in.
		var stillTeaching int64

		// a teacher who deleted the account may keep a row of the class for the handover, so the account has to be active too
		err = tx.Table("class_teachers AS ct").
			Joins("JOIN gurus AS g ON g.id = ct.guru_id AND g.deleted_at IS NULL").
			Where("ct.class_id = ? AND ct.guru_id = ?", classID, actorGuruID).
			Count(&stillTeaching).Error
		if err != nil {
			return err
		}

		if stillTeaching == 0 {
			return apperror.ErrClassNotFound
		}

		link := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&entity.ClassTeacher{ClassID: classID, GuruID: guruIDs[0]})
		if link.Error != nil {
			return link.Error
		}

		if link.RowsAffected == 0 {
			return apperror.ErrTeacherInClass
		}

		var rows []TeacherRow

		err = teacherQuery(tx).Where("ct.class_id = ? AND ct.guru_id = ?", classID, guruIDs[0]).Scan(&rows).Error
		if err != nil || len(rows) == 0 {
			return err
		}

		added = &rows[0]

		return nil
	})
	if err != nil {
		return nil, err
	}

	return added, nil
}

// loadTeam reads the active teachers of the class. The caller holds classteam.Lock, so the answer holds until the end of the
// transaction.
func loadTeam(tx *gorm.DB, classID uuid.UUID) (map[uuid.UUID]teamRow, error) {
	var rows []teamRow

	err := tx.Table("class_teachers AS ct").
		Select("ct.guru_id AS guru_id, ct.is_owner AS is_owner").
		Joins("JOIN gurus AS g ON g.id = ct.guru_id AND g.deleted_at IS NULL").
		Where("ct.class_id = ?", classID).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	team := make(map[uuid.UUID]teamRow, len(rows))
	for _, row := range rows {
		team[row.GuruID] = row
	}

	return team, nil
}

func (r *GuruDB) RemoveTeacher(ctx context.Context, classID uuid.UUID, actor uuid.UUID, target uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := classteam.Lock(tx, classID)
		if err != nil {
			return err
		}

		team, err := loadTeam(tx, classID)
		if err != nil {
			return err
		}

		// the caller stopped teaching the class since the request was checked
		self, teaches := team[actor]
		if !teaches {
			return apperror.ErrForbidden
		}

		goer, found := team[target]
		if !found {
			return apperror.ErrTeacherNotInClass
		}

		if target != actor && !self.IsOwner {
			return apperror.ErrNotClassOwner
		}

		if len(team) == 1 {
			return apperror.ErrLastTeacher
		}

		if goer.IsOwner {
			err = classteam.HandOver(tx, classID, target)
			if err != nil {
				return err
			}
		}

		err = tx.Where("class_id = ? AND guru_id = ?", classID, target).Delete(&entity.ClassTeacher{}).Error
		if err == nil {
			// the teacher who is gone knows the join code, so it is replaced under the same lock that joining takes
			_, err = classcode.Replace(tx, classID)
		}

		return err
	})
}

func (r *GuruDB) RemoveStudent(ctx context.Context, classID uuid.UUID, studentID uuid.UUID) (bool, error) {
	removed := false

	err := ranking.Retry(func() error {
		return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// a retried transaction starts from nothing
			removed = false

			var err error

			_, removed, err = childremoval.Remove(tx, studentID, func(rows *gorm.DB) *gorm.DB {
				return rows.Where("class_id = ?", classID)
			}, r.fileKey)

			return err
		})
	})

	return removed, err
}

func (r *GuruDB) TransferOwnership(ctx context.Context, classID uuid.UUID, actor uuid.UUID, target uuid.UUID) (*TeacherRow, error) {
	var owner *TeacherRow

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// a retried transaction starts from nothing
		owner = nil

		err := classteam.Lock(tx, classID)
		if err != nil {
			return err
		}

		team, err := loadTeam(tx, classID)
		if err != nil {
			return err
		}

		self, teaches := team[actor]
		if !teaches {
			return apperror.ErrForbidden
		}

		if !self.IsOwner {
			return apperror.ErrNotClassOwner
		}

		if _, found := team[target]; !found {
			return apperror.ErrTeacherNotInClass
		}

		if target != actor {
			err = classteam.Transfer(tx, classID, actor, target)
			if err != nil {
				return err
			}
		}

		var rows []TeacherRow

		err = teacherQuery(tx).Where("ct.class_id = ? AND ct.guru_id = ?", classID, target).Scan(&rows).Error
		if err != nil || len(rows) == 0 {
			return err
		}

		owner = &rows[0]

		return nil
	})
	if err != nil {
		return nil, err
	}

	return owner, nil
}

func (r *GuruDB) ListMoveRequests(ctx context.Context, classID uuid.UUID) ([]enrollment.ClassRow, error) {
	return enrollment.ListForClass(r.db.WithContext(ctx), classID)
}

func (r *GuruDB) AcceptMove(ctx context.Context, classID uuid.UUID, requestID uuid.UUID, guruID uuid.UUID) (uuid.UUID, error) {
	var left uuid.UUID

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error

		left, err = enrollment.Accept(tx, classID, requestID, guruID)

		return err
	})

	return left, err
}

func (r *GuruDB) RejectMove(ctx context.Context, classID uuid.UUID, requestID uuid.UUID, guruID uuid.UUID) error {
	return enrollment.Reject(r.db.WithContext(ctx), classID, requestID, guruID)
}

func (r *GuruDB) ArchiveClass(ctx context.Context, classID uuid.UUID, actorGuruID uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// the teachers lock comes first, the ranking lock second, the order that a join takes them in. A mood log or a scan that holds
		// the ranking lock finishes before the archive, and one that waits finds the class archived.
		archived, err := classteam.LockLive(tx, classID)
		if err != nil {
			return err
		}

		// the owner is checked under the lock, because a change of owner or a removal commits under it
		var owners []bool

		err = tx.Table("class_teachers AS ct").
			Select("ct.is_owner").
			Joins("JOIN gurus AS g ON g.id = ct.guru_id AND g.deleted_at IS NULL").
			Where("ct.class_id = ? AND ct.guru_id = ?", classID, actorGuruID).
			Scan(&owners).Error
		if err != nil {
			return err
		}

		if len(owners) == 0 {
			return apperror.ErrClassNotFound
		}

		if !owners[0] {
			return apperror.ErrNotClassOwner
		}

		if archived {
			return apperror.ErrClassArchived
		}

		// a teacher who is refused above never waits for the ranking lock, so a non owner cannot stall the class by asking again
		err = ranking.Lock(tx, classID)
		if err != nil {
			return err
		}

		return tx.Exec("UPDATE classes SET archived_at = now() WHERE id = ? AND archived_at IS NULL", classID).Error
	})
}

func (r *GuruDB) RestoreClass(ctx context.Context, classID uuid.UUID, actorGuruID uuid.UUID) (string, error) {
	var code string

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// a retried transaction starts from nothing
		code = ""

		// every teacher of the class gets a class back, so each of them is counted and not only the caller. The rows stay locked in the
		// order of the ids, so teachers who restore two classes at once are counted one after the other, and they come before the
		// teachers lock as they do when a teacher is added
		var guruIDs []uuid.UUID

		err := tx.Raw(`SELECT g.id FROM gurus AS g
			WHERE g.deleted_at IS NULL AND g.id IN (SELECT ct.guru_id FROM class_teachers AS ct WHERE ct.class_id = ?)
			ORDER BY g.id FOR UPDATE OF g`, classID).Scan(&guruIDs).Error
		if err != nil {
			return err
		}

		if !slices.Contains(guruIDs, actorGuruID) {
			return apperror.ErrClassNotFound
		}

		archived, err := classteam.LockLive(tx, classID)
		if err != nil {
			return err
		}

		var teaching int64

		err = tx.Table("class_teachers AS ct").
			Joins("JOIN gurus AS g ON g.id = ct.guru_id AND g.deleted_at IS NULL").
			Where("ct.class_id = ? AND ct.guru_id = ?", classID, actorGuruID).
			Count(&teaching).Error
		if err != nil {
			return err
		}

		if teaching == 0 {
			return apperror.ErrClassNotFound
		}

		if !archived {
			return apperror.ErrClassNotArchived
		}

		for _, id := range guruIDs {
			err = classteam.CheckClassRoom(tx, id, constants.MaxClassesPerTeacher)
			if err != nil {
				return err
			}
		}

		// the old code may have been shared widely while the class was closed, so the class comes back with a new one
		code, err = classcode.Replace(tx, classID)
		if err != nil {
			return err
		}

		return tx.Exec("UPDATE classes SET archived_at = NULL WHERE id = ?", classID).Error
	})

	return code, err
}

func (r *GuruDB) StudentHistory(ctx context.Context, classID uuid.UUID, studentID uuid.UUID) ([]enrollment.HistoryRow, error) {
	var rows []enrollment.HistoryRow

	// one snapshot for every read, so a child that moves out while the answer is built is either all in it or not in it at all
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error

		rows, err = enrollment.History(tx, classID, studentID)

		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})

	return rows, err
}
