// Package repository handles the learning video and forum database operations
package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
)

// PostRow is a forum post joined with the account columns shown next to it
type PostRow struct {
	entity.ForumPost `gorm:"embedded"`
	AuthorName       string
	AuthorAvatarURL  *string
	AuthorRole       constants.Role
	AuthorDeleted    bool
	ReplyCount       int
}

// StudentRow is a child of the class with the latest mood of the requested day
type StudentRow struct {
	entity.Student `gorm:"embedded"`
	TodayMood      *constants.Mood
}

const (
	threadColumns = `p.*, u.full_name AS author_name, u.avatar_url AS author_avatar_url, u.role AS author_role,
		(u.deleted_at IS NOT NULL) AS author_deleted,
		(SELECT COUNT(*) FROM forum_posts AS r WHERE r.parent_id = p.id) AS reply_count`
	replyColumns = `p.*, u.full_name AS author_name, u.avatar_url AS author_avatar_url, u.role AS author_role,
		(u.deleted_at IS NOT NULL) AS author_deleted, 0 AS reply_count`
)

type ClassroomDBItf interface {
	// TeacherOfClass returns the guru id of the account when it teaches the class, and nil otherwise
	TeacherOfClass(ctx context.Context, userID uuid.UUID, classID uuid.UUID) (*uuid.UUID, error)
	// ParentInClass reports whether the account is the parent of a child in the class
	ParentInClass(ctx context.Context, userID uuid.UUID, classID uuid.UUID) (bool, error)
	ListVideos(ctx context.Context, classID uuid.UUID, limit int, offset int) ([]entity.LearningVideo, int, error)
	CreateVideo(ctx context.Context, video *entity.LearningVideo) error
	ListThreads(ctx context.Context, classID uuid.UUID, limit int, offset int) ([]PostRow, int, error)
	ThreadExists(ctx context.Context, classID uuid.UUID, threadID uuid.UUID) (bool, error)
	ListReplies(ctx context.Context, threadID uuid.UUID, limit int, offset int) ([]PostRow, int, error)
	CreatePost(ctx context.Context, post *entity.ForumPost) error
	// FindPost returns a post with its author, or nil when it does not exist
	FindPost(ctx context.Context, postID uuid.UUID) (*PostRow, error)
	// ListStudents returns the children of the class by name, with the latest mood each recorded between from and to
	ListStudents(ctx context.Context, classID uuid.UUID, from time.Time, to time.Time, limit int, offset int) ([]StudentRow, int, error)
}

type ClassroomDB struct {
	db *gorm.DB
}

func NewClassroomDB(db *gorm.DB) ClassroomDBItf {
	return &ClassroomDB{db: db}
}

func (r *ClassroomDB) TeacherOfClass(ctx context.Context, userID uuid.UUID, classID uuid.UUID) (*uuid.UUID, error) {
	var ids []uuid.UUID

	err := r.db.WithContext(ctx).
		Table("gurus AS g").
		Select("g.id").
		Joins("JOIN class_teachers AS ct ON ct.guru_id = g.id").
		Where("g.user_id = ? AND ct.class_id = ? AND g.deleted_at IS NULL", userID, classID).
		Limit(1).
		Scan(&ids).Error
	if err != nil {
		return nil, err
	}

	if len(ids) == 0 {
		return nil, nil
	}

	return &ids[0], nil
}

func (r *ClassroomDB) ParentInClass(ctx context.Context, userID uuid.UUID, classID uuid.UUID) (bool, error) {
	var total int64

	err := r.db.WithContext(ctx).
		Table("students AS s").
		Joins("JOIN walis AS w ON w.id = s.wali_id").
		Where("w.user_id = ? AND s.class_id = ? AND s.deleted_at IS NULL AND w.deleted_at IS NULL", userID, classID).
		Count(&total).Error

	return total > 0, err
}

func (r *ClassroomDB) ListVideos(ctx context.Context, classID uuid.UUID, limit int, offset int) ([]entity.LearningVideo, int, error) {
	var (
		videos []entity.LearningVideo
		total  int64
	)

	query := r.db.WithContext(ctx).Model(&entity.LearningVideo{}).Where("class_id = ?", classID)

	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	err = query.Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&videos).Error
	if err != nil {
		return nil, 0, err
	}

	return videos, int(total), nil
}

func (r *ClassroomDB) CreateVideo(ctx context.Context, video *entity.LearningVideo) error {
	return r.db.WithContext(ctx).Create(video).Error
}

func (r *ClassroomDB) ListThreads(ctx context.Context, classID uuid.UUID, limit int, offset int) ([]PostRow, int, error) {
	var total int64

	err := r.db.WithContext(ctx).Model(&entity.ForumPost{}).
		Where("class_id = ? AND parent_id IS NULL", classID).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	var rows []PostRow

	err = r.db.WithContext(ctx).
		Table("forum_posts AS p").
		Select(threadColumns).
		Joins("JOIN users AS u ON u.id = p.author_user_id").
		Where("p.class_id = ? AND p.parent_id IS NULL", classID).
		Order("p.created_at DESC, p.id DESC").
		Limit(limit).Offset(offset).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	return rows, int(total), nil
}

func (r *ClassroomDB) ThreadExists(ctx context.Context, classID uuid.UUID, threadID uuid.UUID) (bool, error) {
	var total int64

	err := r.db.WithContext(ctx).Model(&entity.ForumPost{}).
		Where("id = ? AND class_id = ? AND parent_id IS NULL", threadID, classID).Count(&total).Error

	return total > 0, err
}

func (r *ClassroomDB) ListReplies(ctx context.Context, threadID uuid.UUID, limit int, offset int) ([]PostRow, int, error) {
	var total int64

	err := r.db.WithContext(ctx).Model(&entity.ForumPost{}).Where("parent_id = ?", threadID).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	var rows []PostRow

	err = r.db.WithContext(ctx).
		Table("forum_posts AS p").
		Select(replyColumns).
		Joins("JOIN users AS u ON u.id = p.author_user_id").
		Where("p.parent_id = ?", threadID).
		Order("p.created_at ASC, p.id ASC").
		Limit(limit).Offset(offset).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	return rows, int(total), nil
}

func (r *ClassroomDB) CreatePost(ctx context.Context, post *entity.ForumPost) error {
	return r.db.WithContext(ctx).Create(post).Error
}

func (r *ClassroomDB) FindPost(ctx context.Context, postID uuid.UUID) (*PostRow, error) {
	var rows []PostRow

	err := r.db.WithContext(ctx).
		Table("forum_posts AS p").
		Select(threadColumns).
		Joins("JOIN users AS u ON u.id = p.author_user_id").
		Where("p.id = ?", postID).
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

func (r *ClassroomDB) ListStudents(
	ctx context.Context, classID uuid.UUID, from time.Time, to time.Time, limit int, offset int,
) ([]StudentRow, int, error) {
	var total int64

	err := r.db.WithContext(ctx).Model(&entity.Student{}).Where("class_id = ?", classID).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	var rows []StudentRow

	err = r.db.WithContext(ctx).
		Table("students AS s").
		Select(`s.*, (SELECT m.mood_type FROM mood_logs AS m
			WHERE m.student_id = s.id AND m.recorded_at >= ? AND m.recorded_at < ?
			ORDER BY m.recorded_at DESC, m.id DESC LIMIT 1) AS today_mood`, from, to).
		Where("s.class_id = ? AND s.deleted_at IS NULL", classID).
		Order("s.full_name ASC, s.id ASC").
		Limit(limit).Offset(offset).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	return rows, int(total), nil
}
