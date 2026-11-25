// Package notifier exposes a narrow hook for emitting persistent in-app notifications on domain events.
package notifier

import (
	"context"

	"github.com/google/uuid"
)

// Notifier persists in-app notifications without coupling use cases to notification storage.
type Notifier interface {
	NotifyStudentParent(ctx context.Context, studentID uuid.UUID, kind string, title string, body string, refType string, refID uuid.UUID)
	NotifyMoveRequestParent(ctx context.Context, requestID uuid.UUID, kind string, title string, body string)
	NotifyClassTeachers(ctx context.Context, classID uuid.UUID, kind string, title string, body string, refType string, refID uuid.UUID)
	NotifyClassParents(ctx context.Context, classID uuid.UUID, kind string, title string, body string, refType string, refID uuid.UUID)
}
