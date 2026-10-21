// Package constants stores values shared across modules
package constants

import "time"

type Role string

const (
	RoleGuru  Role = "GURU"
	RoleWali  Role = "WALI"
	RoleAdmin Role = "ADMIN"
)

func (r Role) Valid() bool {
	return r == RoleGuru || r == RoleWali || r == RoleAdmin
}

type Mood string

const (
	MoodSenang  Mood = "SENANG"
	MoodSedih   Mood = "SEDIH"
	MoodMarah   Mood = "MARAH"
	MoodBingung Mood = "BINGUNG"
)

// Moods lists every mood in the order used by charts
var Moods = []Mood{MoodSenang, MoodBingung, MoodSedih, MoodMarah}

func (m Mood) Valid() bool {
	switch m {
	case MoodSenang, MoodSedih, MoodMarah, MoodBingung:
		return true
	default:
		return false
	}
}

type MoodSource string

const (
	SourceAICamera    MoodSource = "AI_CAMERA"
	SourceManualInput MoodSource = "MANUAL_INPUT"
)

func (s MoodSource) Valid() bool {
	return s == SourceAICamera || s == SourceManualInput
}

type TrashType string

const (
	TrashOrganik   TrashType = "ORGANIK"
	TrashAnorganik TrashType = "ANORGANIK"
	TrashB3        TrashType = "B3"
)

func (t TrashType) Valid() bool {
	return t == TrashOrganik || t == TrashAnorganik || t == TrashB3
}

// EnrollmentStatus says where an enrollment of a child in a class stands, see docs/adr/0014-class-enrollments.md
type EnrollmentStatus string

const (
	EnrollmentPending  EnrollmentStatus = "pending"
	EnrollmentActive   EnrollmentStatus = "active"
	EnrollmentEnded    EnrollmentStatus = "ended"
	EnrollmentRejected EnrollmentStatus = "rejected"
)

// Period is a calendar window of the school timezone that the leaderboard and the trash stats can be limited to. An empty
// period means the default view, the running semester for the leaderboard and all time for the stats.
type Period string

const (
	PeriodToday Period = "today"
	PeriodWeek  Period = "week"
	PeriodMonth Period = "month"
)

// ParsePeriod accepts an empty string as the default view and the three names of a period, and reports false for anything else
func ParsePeriod(raw string) (Period, bool) {
	switch period := Period(raw); period {
	case "", PeriodToday, PeriodWeek, PeriodMonth:
		return period, true
	default:
		return "", false
	}
}

type StorageDirectory string

const (
	AvatarDirectory StorageDirectory = "avatars"
	TrashDirectory  StorageDirectory = "trash-scans"
)

const (
	AccessTokenIssuer = "kibooz"
	DefaultSchoolName = "TK Pertiwi Harapan Bangsa"
	DefaultGradeLevel = "Class A"
	DefaultAcademicYr = "2026/2027"

	RefreshKeyPrefix     = "refresh:"
	LeaderboardKeyPrefix = "leaderboard:"

	AvatarMaxBytes     = 2 * 1024 * 1024
	TrashPhotoMaxBytes = 4 * 1024 * 1024

	MoodBatchMax = 30

	// MaxClassesPerTeacher is the most classes that a teacher can teach at once, archived classes not counted
	MaxClassesPerTeacher = 20

	// MaxChildrenPerClass is the most active children of one class, counted when a child joins
	MaxChildrenPerClass = 60

	// JSONBodyMaxBytes is the largest body of any request except the two uploads, whose bodies are photos. The largest real
	// body is a batch of mood logs with 500 character notes, which stays well below this.
	JSONBodyMaxBytes = 64 * 1024

	// MoodLogsPerStudentPerDay is the most mood logs one child can have on one local day, whoever records them
	MoodLogsPerStudentPerDay = 20

	// MoodRecordedAtMaxAge is how far back a client may date a mood log that it queued while it was offline. It matches the
	// time an Idempotency-Key is kept, so a replay that is old enough to be refused here could not be repeated safely anyway.
	MoodRecordedAtMaxAge = 7 * 24 * time.Hour
	// MoodRecordedAtSkew is how far ahead of the server clock a client clock may run. A later time is refused, and a time
	// inside the skew is taken as now, so a fast clock cannot put a log ahead of the ones that come after it.
	MoodRecordedAtSkew = 5 * time.Minute
)

// MoodLabels are the parent facing captions shown on the wali dashboard
var MoodLabels = map[Mood]string{
	MoodSenang:  "Happy / Cheerful",
	MoodSedih:   "Sad / Needs Cheering Up",
	MoodMarah:   "Angry / Needs Calming",
	MoodBingung: "Unsure / Needs Support",
}

// MoodColors are the donut chart colors fixed by the API contract
var MoodColors = map[Mood]string{
	MoodSenang:  "#34C759",
	MoodBingung: "#FF9500",
	MoodSedih:   "#5856D6",
	MoodMarah:   "#FF3B30",
}

// The two kinds of chat conversation. A direct one is between a teacher and a parent, a class one is the room of a class.
const (
	ChatKindDirect = "direct"
	ChatKindClass  = "class"
)
