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
	JoinOpen        bool            `json:"joinOpen"`
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
	Notes           string               `json:"notes" validate:"omitempty,max=500"`
	// RecordedAt is when the mood was seen, for a log that was queued while the device was offline. It defaults to now.
	RecordedAt *time.Time `json:"recordedAt"`
}

type LogMoodResponse struct {
	LogID      uuid.UUID `json:"logId"`
	RecordedAt time.Time `json:"recordedAt"`
}

type LogMoodBatchEntry struct {
	StudentID       uuid.UUID            `json:"studentId" validate:"required"`
	MoodType        constants.Mood       `json:"moodType" validate:"required,oneof=SENANG SEDIH MARAH BINGUNG"`
	Source          constants.MoodSource `json:"source" validate:"omitempty,oneof=AI_CAMERA MANUAL_INPUT"`
	ConfidenceScore *float32             `json:"confidenceScore" validate:"omitempty,gte=0,lte=1"`
	Notes           string               `json:"notes" validate:"omitempty,max=500"`
}

// LogMoodBatchRequest max must match constants.MoodBatchMax
type LogMoodBatchRequest struct {
	ClassID uuid.UUID           `json:"classId" validate:"required"`
	Entries []LogMoodBatchEntry `json:"entries" validate:"required,min=1,max=30,dive"`
	// RecordedAt is when the moods were seen, one time for the whole batch. It defaults to now.
	RecordedAt *time.Time `json:"recordedAt"`
}

type LogMoodBatchItem struct {
	LogID      uuid.UUID `json:"logId"`
	StudentID  uuid.UUID `json:"studentId"`
	RecordedAt time.Time `json:"recordedAt"`
}

type LogMoodBatchResponse struct {
	Logs []LogMoodBatchItem `json:"logs"`
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

type ClassSummary struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	GradeLevel    string    `json:"gradeLevel"`
	SchoolName    string    `json:"schoolName"`
	AcademicYear  string    `json:"academicYear"`
	JoinCode      string    `json:"joinCode"`
	JoinOpen      bool      `json:"joinOpen"`
	TotalStudents int       `json:"totalStudents"`
	CreatedAt     time.Time `json:"createdAt"`
	// ArchivedAt is set while the class is archived, which makes it read only. The class is listed with it until a teacher restores it.
	ArchivedAt *time.Time `json:"archivedAt,omitempty"`
}

// SetJoiningRequest opens or closes a class for new children
type SetJoiningRequest struct {
	JoinOpen *bool `json:"joinOpen" validate:"required"`
}

// JoiningResponse tells whether a class takes new children
type JoiningResponse struct {
	JoinOpen bool `json:"joinOpen"`
}

// JoinCodeResponse is the new join code of a class
type JoinCodeResponse struct {
	JoinCode string `json:"joinCode"`
}

type ClassList struct {
	Classes []ClassSummary `json:"classes"`
	PageInfo
}

// ClassDetail adds the amount of learning content to the summary
type ClassDetail struct {
	ClassSummary
	TotalVideos  int `json:"totalVideos"`
	TotalThreads int `json:"totalThreads"`
}

type CreateClassRequest struct {
	Name         string `json:"name" validate:"required,max=50"`
	GradeLevel   string `json:"gradeLevel" validate:"omitempty,max=20"`
	AcademicYear string `json:"academicYear" validate:"omitempty,max=20"`
	SchoolName   string `json:"schoolName" validate:"omitempty,max=150"`
}

type GuruProfile struct {
	ID         uuid.UUID `json:"id"`
	FullName   string    `json:"fullName"`
	NIP        *string   `json:"nip"`
	SchoolName string    `json:"schoolName"`
	AvatarURL  *string   `json:"avatarUrl"`
}

type GuruProfileDetail struct {
	GuruProfile
	Email       string    `json:"email"`
	PhoneNumber *string   `json:"phoneNumber"`
	Address     *string   `json:"address"`
	JoinedAt    time.Time `json:"joinedAt"`
}

// UpdateGuruProfileRequest leaves a field alone when it is missing and clears it when it is an empty text
type UpdateGuruProfileRequest struct {
	FullName    *string `json:"fullName" validate:"omitempty,max=150"`
	PhoneNumber *string `json:"phoneNumber" validate:"omitempty,max=30"`
	Address     *string `json:"address" validate:"omitempty,max=1000"`
}

// AddTeacherRequest names the account of the teacher to add to a class
type AddTeacherRequest struct {
	Email string `json:"email" validate:"required,email,max=255"`
}

// Teacher is a teacher of a class. ID is the id of the teacher in the class routes, UserID is the id of the account, the
// one that GuruProfile and a forum author carry, and IsSelf says whether the account is the signed in one
type Teacher struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"userId"`
	IsSelf    bool      `json:"isSelf"`
	FullName  string    `json:"fullName"`
	NIP       *string   `json:"nip"`
	AvatarURL *string   `json:"avatarUrl"`
	IsOwner   bool      `json:"isOwner"`
	JoinedAt  time.Time `json:"joinedAt"`
}

// TransferOwnershipRequest names the teacher of the class who becomes its owner
type TransferOwnershipRequest struct {
	TeacherID uuid.UUID `json:"teacherId" validate:"required"`
}

type TeacherList struct {
	Teachers []Teacher `json:"teachers"`
}

// NISNVerification answers a teacher who verified or unverified the NISN of a child
type NISNVerification struct {
	NISNVerified bool `json:"nisnVerified"`
	// Displaced is how many other children lost the same number because they had not been verified
	Displaced int64 `json:"displaced"`
}
