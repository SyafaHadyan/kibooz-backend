package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/kiboozapp/kibooz-backend/internal/constants"
)

// GuruHistoryEvent is one entry in a class activity timeline across dates
type GuruHistoryEvent struct {
	ID              uuid.UUID       `json:"id"`
	Type            string          `json:"type"`
	ClassID         uuid.UUID       `json:"classId"`
	ClassName       string          `json:"className"`
	StudentID       *uuid.UUID      `json:"studentId,omitempty"`
	StudentName     *string         `json:"studentName,omitempty"`
	ActorName       *string         `json:"actorName,omitempty"`
	MoodType        *constants.Mood `json:"moodType,omitempty"`
	ConfidenceScore *float32        `json:"confidenceScore,omitempty"`
	Title           string          `json:"title"`
	Description     *string         `json:"description,omitempty"`
	OccurredAt      time.Time       `json:"occurredAt"`
}

type GuruHistoryResponse struct {
	Events []GuruHistoryEvent `json:"events"`
	Page   int                `json:"page"`
	Limit  int                `json:"limit"`
	Total  int                `json:"total"`
}

// WaliHistoryStudent identifies the child whose history was requested
type WaliHistoryStudent struct {
	ID        uuid.UUID `json:"id"`
	FullName  string    `json:"fullName"`
	ClassName string    `json:"className"`
}

// WaliHistoryEvent is one entry in a child's activity timeline across dates inside the active class enrollment
type WaliHistoryEvent struct {
	ID               uuid.UUID            `json:"id"`
	Type             string               `json:"type"`
	MoodType         *constants.Mood      `json:"moodType,omitempty"`
	TrashType        *constants.TrashType `json:"trashType,omitempty"`
	PointsAwarded    *int                 `json:"pointsAwarded,omitempty"`
	GuidanceID       *string              `json:"guidanceId,omitempty"`
	GuidanceTitle    *string              `json:"guidanceTitle,omitempty"`
	GuidanceCategory *string              `json:"guidanceCategory,omitempty"`
	Title            string               `json:"title"`
	Description      *string              `json:"description,omitempty"`
	OccurredAt       time.Time            `json:"occurredAt"`
}

type WaliHistoryResponse struct {
	Student WaliHistoryStudent `json:"student"`
	Events  []WaliHistoryEvent `json:"events"`
	Page    int                `json:"page"`
	Limit   int                `json:"limit"`
	Total   int                `json:"total"`
}
