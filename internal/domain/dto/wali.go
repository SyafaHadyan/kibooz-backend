package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
)

type WaliStudent struct {
	ID        uuid.UUID `json:"id"`
	FullName  string    `json:"fullName"`
	ClassID   uuid.UUID `json:"classId"`
	ClassName string    `json:"className"`
	AvatarURL *string   `json:"avatarUrl"`
	NISN      string    `json:"nisn"`
}

type TodayMood struct {
	MoodType     constants.Mood `json:"moodType"`
	Label        string         `json:"label"`
	Confidence   float32        `json:"confidence"`
	RecordedAt   time.Time      `json:"recordedAt"`
	TeacherNotes *string        `json:"teacherNotes"`
}

type PointsSummary struct {
	TotalPoints    int `json:"totalPoints"`
	ClassRank      int `json:"classRank"`
	OrganicCount   int `json:"organicCount"`
	AnorganicCount int `json:"anorganicCount"`
}

type RecommendedGuidance struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Category  string `json:"category"`
	BannerURL string `json:"bannerUrl"`
}

type WaliDashboardResponse struct {
	Student             WaliStudent          `json:"student"`
	TodayMood           *TodayMood           `json:"todayMood"`
	PointsSummary       PointsSummary        `json:"pointsSummary"`
	RecommendedGuidance *RecommendedGuidance `json:"recommendedGuidance"`
}

type ApplyGuidanceRequest struct {
	GuidanceID  string    `json:"guidanceId" validate:"required,max=64"`
	StudentID   uuid.UUID `json:"studentId" validate:"required"`
	ParentNotes string    `json:"parentNotes" validate:"omitempty,max=2000"`
}
