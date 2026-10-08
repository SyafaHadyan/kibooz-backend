package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
)

// PageInfo describes which part of a list a response holds
type PageInfo struct {
	Page  int `json:"page"`
	Limit int `json:"limit"`
	Total int `json:"total"`
}

type AddVideoRequest struct {
	Title           string `json:"title" validate:"required,max=150"`
	Description     string `json:"description" validate:"omitempty,max=2000"`
	VideoURL        string `json:"videoUrl" validate:"required,max=2048"`
	ThumbnailURL    string `json:"thumbnailUrl" validate:"omitempty,max=2048"`
	DurationSeconds *int   `json:"durationSeconds" validate:"omitempty,gte=1,lte=86400"`
}

// UpdateVideoRequest changes the details of a video. A missing field stays, an empty text clears the description or the thumbnail
// and 0 clears the duration. The address of the video itself cannot change, so a different file is a new video.
type UpdateVideoRequest struct {
	Title           *string `json:"title" validate:"omitempty,max=150"`
	Description     *string `json:"description" validate:"omitempty,max=2000"`
	ThumbnailURL    *string `json:"thumbnailUrl" validate:"omitempty,max=2048"`
	DurationSeconds *int    `json:"durationSeconds" validate:"omitempty,gte=0,lte=86400"`
}

type VideoUploadRequest struct {
	ContentType string `json:"contentType" validate:"required,oneof=video/mp4 video/webm"`
	SizeBytes   int64  `json:"sizeBytes" validate:"required,gte=1"`
}

// VideoUpload tells the app where to send the file and which address to register afterwards
type VideoUpload struct {
	UploadURL string            `json:"uploadUrl"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers"`
	VideoURL  string            `json:"videoUrl"`
	ExpiresAt time.Time         `json:"expiresAt"`
}

type Video struct {
	ID              uuid.UUID `json:"id"`
	Title           string    `json:"title"`
	Description     *string   `json:"description"`
	VideoURL        string    `json:"videoUrl"`
	ThumbnailURL    *string   `json:"thumbnailUrl"`
	DurationSeconds *int      `json:"durationSeconds"`
	CreatedAt       time.Time `json:"createdAt"`
}

type VideoList struct {
	Videos []Video `json:"videos"`
	PageInfo
}

type ForumAuthor struct {
	ID        uuid.UUID      `json:"id"`
	FullName  string         `json:"fullName"`
	Role      constants.Role `json:"role"`
	AvatarURL *string        `json:"avatarUrl"`
}

type CreateThreadRequest struct {
	Title string `json:"title" validate:"required,max=150"`
	Body  string `json:"body" validate:"required,max=5000"`
}

type CreateReplyRequest struct {
	Body string `json:"body" validate:"required,max=5000"`
}

// UpdateThreadRequest changes the title or the text of a thread, a missing field stays as it was
type UpdateThreadRequest struct {
	Title *string `json:"title" validate:"omitempty,max=150"`
	Body  *string `json:"body" validate:"omitempty,max=5000"`
}

type UpdateReplyRequest struct {
	Body string `json:"body" validate:"required,max=5000"`
}

type ForumThread struct {
	ID         uuid.UUID   `json:"id"`
	Title      string      `json:"title"`
	Body       string      `json:"body"`
	Author     ForumAuthor `json:"author"`
	ReplyCount int         `json:"replyCount"`
	CreatedAt  time.Time   `json:"createdAt"`
}

type ForumThreadList struct {
	Threads []ForumThread `json:"threads"`
	PageInfo
}

type ForumReply struct {
	ID        uuid.UUID   `json:"id"`
	Body      string      `json:"body"`
	Author    ForumAuthor `json:"author"`
	CreatedAt time.Time   `json:"createdAt"`
}

type ForumReplyList struct {
	Replies []ForumReply `json:"replies"`
	PageInfo
}

type ClassStudent struct {
	ID            uuid.UUID       `json:"id"`
	FullName      string          `json:"fullName"`
	NISN          string          `json:"nisn"`
	AvatarURL     *string         `json:"avatarUrl"`
	CurrentPoints int             `json:"currentPoints"`
	ClassRank     int             `json:"classRank"`
	TodayMood     *constants.Mood `json:"todayMood"`
}

type ClassStudentList struct {
	Students []ClassStudent `json:"students"`
	PageInfo
}
