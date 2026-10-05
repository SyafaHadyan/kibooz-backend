package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
)

type GuruTeacher struct {
	FullName  string  `json:"fullName"`
	NIP       *string `json:"nip"`
	AvatarURL *string `json:"avatarUrl"`
}

type ClassOverview struct {
	ClassID         uuid.UUID       `json:"classId"`
	ClassName       string          `json:"className"`
	JoinCode        string          `json:"joinCode"`
	TotalStudents   int             `json:"totalStudents"`
	PresentStudents int             `json:"presentStudents"`
	DominantMood    *constants.Mood `json:"dominantMood"`
}

type DailyMoodDistribution struct {
	Senang  int `json:"senang"`
	Sedih   int `json:"sedih"`
	Marah   int `json:"marah"`
	Bingung int `json:"bingung"`
}

type GuruDashboardResponse struct {
	Teacher               GuruTeacher           `json:"teacher"`
	ClassOverview         ClassOverview         `json:"classOverview"`
	DailyMoodDistribution DailyMoodDistribution `json:"dailyMoodDistribution"`
}

type LogMoodRequest struct {
	StudentID       uuid.UUID            `json:"studentId" validate:"required"`
	MoodType        constants.Mood       `json:"moodType" validate:"required,oneof=SENANG SEDIH MARAH BINGUNG"`
	Source          constants.MoodSource `json:"source" validate:"omitempty,oneof=AI_CAMERA MANUAL_INPUT"`
	ConfidenceScore *float32             `json:"confidenceScore" validate:"omitempty,gte=0,lte=1"`
	Notes           string               `json:"notes" validate:"omitempty,max=2000"`
}

type LogMoodResponse struct {
	LogID      uuid.UUID `json:"logId"`
	RecordedAt time.Time `json:"recordedAt"`
}

type MoodAnalyticsQuery struct {
	ClassID uuid.UUID
	Range   string
}

type DonutSlice struct {
	Mood       constants.Mood `json:"mood"`
	Percentage int            `json:"percentage"`
	ColorHex   string         `json:"colorHex"`
}

type TrendPoint struct {
	Day               string `json:"day"`
	AverageHappyScore int    `json:"averageHappyScore"`
}

type MoodCount struct {
	Mood  constants.Mood `json:"mood"`
	Count int            `json:"count"`
}

type MoodAnalyticsResponse struct {
	DonutSummary        []DonutSlice `json:"donutSummary"`
	WeeklyTrend         []TrendPoint `json:"weeklyTrend"`
	MonthlyDistribution []MoodCount  `json:"monthlyDistribution,omitempty"`
}
