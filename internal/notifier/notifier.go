// Package notifier exposes a narrow hook for emitting persistent in-app notifications on domain events.
package notifier

import (
	"context"

	"github.com/google/uuid"
)

// StudentNotice is one notification for the parent of a student, so that many students are notified in one call.
type StudentNotice struct {
	StudentID uuid.UUID
	Title     string
	Body      string
	RefID     uuid.UUID
}

// Notifier persists in-app notifications without coupling use cases to notification storage.
type Notifier interface {
	NotifyStudentParent(ctx context.Context, studentID uuid.UUID, kind string, title string, body string, refType string, refID uuid.UUID)
	// NotifyStudentParents stores one notification of the same kind and reference type for each notice, in one statement
	NotifyStudentParents(ctx context.Context, kind string, refType string, notices []StudentNotice)
	NotifyMoveRequestParent(ctx context.Context, requestID uuid.UUID, kind string, title string, body string)
	NotifyClassTeachers(ctx context.Context, classID uuid.UUID, kind string, title string, body string, refType string, refID uuid.UUID)
	NotifyClassParents(ctx context.Context, classID uuid.UUID, kind string, title string, body string, refType string, refID uuid.UUID)
}
