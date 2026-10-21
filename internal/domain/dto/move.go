package dto

import (
	"time"

	"github.com/google/uuid"
)

// MoveRequestInput asks for a child to move to the class that owns classCode
type MoveRequestInput struct {
	ClassCode string `json:"classCode" validate:"required,alphanum,min=4,max=12"`
}

// MoveRequest is a request of a parent to move a child, as the parent sees it. A request that was rejected stays in the list
// with its decision time, and one that was accepted is no longer a request but the class of the child.
type MoveRequest struct {
	ID          uuid.UUID  `json:"id"`
	StudentID   uuid.UUID  `json:"studentId"`
	ClassID     uuid.UUID  `json:"classId"`
	ClassName   string     `json:"className"`
	Status      string     `json:"status"`
	RequestedAt time.Time  `json:"requestedAt"`
	DecidedAt   *time.Time `json:"decidedAt,omitempty"`
}

type MoveRequestList struct {
	Requests []MoveRequest `json:"requests"`
}

// ClassMoveRequest is a request that waits for a teacher of the class, as the teacher sees it
type ClassMoveRequest struct {
	ID          uuid.UUID `json:"id"`
	StudentID   uuid.UUID `json:"studentId"`
	StudentName string    `json:"studentName"`
	RequestedAt time.Time `json:"requestedAt"`
}

type ClassMoveRequestList struct {
	Requests []ClassMoveRequest `json:"requests"`
}
