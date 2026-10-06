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

	RequestTimeout = 10 * time.Second
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
