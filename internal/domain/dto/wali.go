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
	// NISNVerified is true while a teacher vouched for the NISN
	NISNVerified bool `json:"nisnVerified"`
	// NISNNeedsUpdate is true when the NISN is empty because another child was verified with it, so the parent has to enter the right one
	NISNNeedsUpdate bool `json:"nisnNeedsUpdate"`
	// ClassArchivedAt is set while the class of the child is archived and read only, so the app can hide what the child cannot do
	ClassArchivedAt *time.Time `json:"classArchivedAt,omitempty"`
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
	SeasonPoints   int `json:"seasonPoints"`
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
	ParentNotes string    `json:"parentNotes" validate:"omitempty,max=500"`
}

// UpdateWaliProfileRequest changes the details of the parent. A missing field is left alone and an empty text clears the
// phone number, the WhatsApp number or the address. The name cannot be empty and the email cannot be changed.
type UpdateWaliProfileRequest struct {
	FullName       *string `json:"fullName" validate:"omitempty,max=150"`
	PhoneNumber    *string `json:"phoneNumber" validate:"omitempty,max=30"`
	WhatsappNumber *string `json:"whatsappNumber" validate:"omitempty,max=30"`
	Address        *string `json:"address" validate:"omitempty,max=1000"`
}

type Guardian struct {
	FullName       string  `json:"fullName"`
	Email          string  `json:"email"`
	PhoneNumber    *string `json:"phoneNumber"`
	WhatsappNumber *string `json:"whatsappNumber"`
	Address        *string `json:"address"`
}

type ChildProfile struct {
	ID       uuid.UUID `json:"id"`
	FullName string    `json:"fullName"`
	NISN     string    `json:"nisn"`
	// NISNVerified is true while a teacher vouched for the NISN
	NISNVerified bool `json:"nisnVerified"`
	// NISNNeedsUpdate is true when the NISN is empty because another child was verified with it, so the parent has to enter the right one
	NISNNeedsUpdate bool      `json:"nisnNeedsUpdate"`
	AvatarURL       *string   `json:"avatarUrl"`
	ClassID         uuid.UUID `json:"classId"`
	ClassName       string    `json:"className"`
	GradeLevel      string    `json:"gradeLevel"`
	SchoolName      string    `json:"schoolName"`
	AcademicYear    string    `json:"academicYear"`
	CurrentPoints   int       `json:"currentPoints"`
	SeasonPoints    int       `json:"seasonPoints"`
	ClassRank       int       `json:"classRank"`
	Guardian        Guardian  `json:"guardian"`
}

// AddChildRequest registers one more child of a parent in the class that owns classCode
type AddChildRequest struct {
	ClassCode string          `json:"classCode" validate:"required,alphanum,min=4,max=12"`
	Student   RegisterStudent `json:"student" validate:"required"`
}

// UpdateChildRequest changes the name or the NISN of a child, a missing field is left alone
type UpdateChildRequest struct {
	FullName *string `json:"fullName" validate:"omitempty,max=150"`
	NISN     *string `json:"nisn" validate:"omitempty,max=30"`
}

// ChildSummary is a child of the parent with the class the child is in
type ChildSummary struct {
	ID       uuid.UUID `json:"id"`
	FullName string    `json:"fullName"`
	NISN     string    `json:"nisn"`
	// NISNVerified is true while a teacher vouched for the NISN
	NISNVerified bool `json:"nisnVerified"`
	// NISNNeedsUpdate is true when the NISN is empty because another child was verified with it, so the parent has to enter the right one
	NISNNeedsUpdate bool      `json:"nisnNeedsUpdate"`
	AvatarURL       *string   `json:"avatarUrl"`
	ClassID         uuid.UUID `json:"classId"`
	ClassName       string    `json:"className"`
	// ClassArchivedAt is set while the class of the child is archived and read only
	ClassArchivedAt *time.Time `json:"classArchivedAt,omitempty"`
}

type ChildList struct {
	Children []ChildSummary `json:"children"`
}
