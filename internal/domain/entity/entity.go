// Package entity maps database tables created by the SQL migrations
package entity

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
)

type User struct {
	ID           uuid.UUID      `gorm:"type:uuid;primaryKey"`
	Email        string         `gorm:"size:255;not null;uniqueIndex"`
	PasswordHash string         `gorm:"size:255;not null"`
	Role         constants.Role `gorm:"type:user_role;not null"`
	FullName     string         `gorm:"size:150;not null"`
	PhoneNumber  *string        `gorm:"size:30"`
	AvatarURL    *string
	CreatedAt    time.Time `gorm:"autoCreateTime"`
	UpdatedAt    time.Time `gorm:"autoUpdateTime"`
	// DeletedAt hides the row from every GORM query once it is set
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

type Guru struct {
	ID         uuid.UUID      `gorm:"type:uuid;primaryKey"`
	UserID     uuid.UUID      `gorm:"type:uuid;not null;uniqueIndex"`
	NIP        *string        `gorm:"column:nip;size:50;uniqueIndex"`
	SchoolName string         `gorm:"size:150;not null"`
	DeletedAt  gorm.DeletedAt `gorm:"index"`
}

type Wali struct {
	ID             uuid.UUID `gorm:"type:uuid;primaryKey"`
	UserID         uuid.UUID `gorm:"type:uuid;not null;uniqueIndex"`
	Address        *string
	WhatsappNumber *string        `gorm:"size:30"`
	DeletedAt      gorm.DeletedAt `gorm:"index"`
}

type Class struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey"`
	JoinCode     string    `gorm:"size:12;not null;uniqueIndex"`
	SchoolName   string    `gorm:"size:150;not null"`
	Name         string    `gorm:"size:50;not null"`
	GradeLevel   string    `gorm:"size:20;not null"`
	AcademicYear string    `gorm:"size:20;not null"`
	CreatedAt    time.Time `gorm:"autoCreateTime"`
}

type ClassTeacher struct {
	ClassID   uuid.UUID `gorm:"type:uuid;primaryKey"`
	GuruID    uuid.UUID `gorm:"type:uuid;primaryKey"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
}

type Student struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey"`
	WaliID        uuid.UUID `gorm:"type:uuid;not null"`
	ClassID       uuid.UUID `gorm:"type:uuid;not null"`
	NISN          string    `gorm:"column:nisn;size:30;not null;uniqueIndex"`
	FullName      string    `gorm:"size:150;not null"`
	AvatarURL     *string
	CurrentPoints int            `gorm:"not null;default:0"`
	RankPosition  int            `gorm:"not null;default:0"`
	CreatedAt     time.Time      `gorm:"autoCreateTime"`
	DeletedAt     gorm.DeletedAt `gorm:"index"`
}

type MoodLog struct {
	ID               uuid.UUID            `gorm:"type:uuid;primaryKey"`
	StudentID        uuid.UUID            `gorm:"type:uuid;not null"`
	RecordedByGuruID uuid.UUID            `gorm:"type:uuid;not null"`
	MoodType         constants.Mood       `gorm:"type:mood_enum;not null"`
	ConfidenceScore  float32              `gorm:"not null"`
	Source           constants.MoodSource `gorm:"type:input_source_enum;not null"`
	Notes            *string
	RecordedAt       time.Time `gorm:"autoCreateTime:false;not null"`
}

type TrashScan struct {
	ID              uuid.UUID           `gorm:"type:uuid;primaryKey"`
	StudentID       uuid.UUID           `gorm:"type:uuid;not null"`
	TrashType       constants.TrashType `gorm:"type:trash_category_enum;not null"`
	ConfidenceScore float32             `gorm:"not null"`
	PointsAwarded   int                 `gorm:"not null"`
	PhotoURL        *string
	ScannedAt       time.Time `gorm:"autoCreateTime:false;not null"`
}

type GuidanceApplication struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey"`
	StudentID   uuid.UUID `gorm:"type:uuid;not null"`
	WaliID      uuid.UUID `gorm:"type:uuid;not null"`
	GuidanceID  string    `gorm:"size:64;not null"`
	ParentNotes *string
	AppliedAt   time.Time `gorm:"autoCreateTime:false;not null"`
}

type RefreshToken struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	UserID    uuid.UUID `gorm:"type:uuid;not null"`
	TokenHash string    `gorm:"column:token_hash;size:64;not null;uniqueIndex"`
	ExpiresAt time.Time `gorm:"not null"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
}
