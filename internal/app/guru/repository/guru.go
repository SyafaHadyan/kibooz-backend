// Package repository handles the guru related database operations
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/SyafaHadyan/kibooz-backend/internal/classcode"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
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

// ClassRow is a class with the number of children in it
type ClassRow struct {
	entity.Class  `gorm:"embedded"`
	TotalStudents int
}

// ProfileUpdate holds the profile fields to change, a Set flag tells a missing field from one that is cleared
type ProfileUpdate struct {
	SetPhone   bool
	Phone      *string
	SetAddress bool
	Address    *string
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
	UpdateProfile(ctx context.Context, userID uuid.UUID, guruID uuid.UUID, update ProfileUpdate) error
	ListTaughtClasses(ctx context.Context, guruID uuid.UUID, limit int, offset int) ([]ClassRow, int, error)
	// FindClassRow returns nil when the guru does not teach the class
	FindClassRow(ctx context.Context, guruID uuid.UUID, classID uuid.UUID) (*ClassRow, error)
	// CountClassContent returns how many learning videos and forum threads the class holds
	CountClassContent(ctx context.Context, classID uuid.UUID) (videos int, threads int, err error)
	// CreateClass stores the class under a new join code and makes the guru its teacher
	CreateClass(ctx context.Context, guruID uuid.UUID, class *entity.Class) error
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

func (r *GuruDB) UpdateProfile(ctx context.Context, userID uuid.UUID, guruID uuid.UUID, update ProfileUpdate) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
		err := classcode.Create(tx, class)
		if err != nil {
			return err
		}

		return tx.Create(&entity.ClassTeacher{ClassID: class.ID, GuruID: guruID}).Error
	})
}

// classQuery selects the classes of a teacher together with the number of children that are still active
func (r *GuruDB) classQuery(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).
		Table("classes AS c").
		Select("c.*, (SELECT COUNT(*) FROM students AS s WHERE s.class_id = c.id AND s.deleted_at IS NULL) AS total_students").
		Joins("JOIN class_teachers AS ct ON ct.class_id = c.id")
}
