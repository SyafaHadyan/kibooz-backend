// Package response builds the JSON envelope shared by every endpoint
package response

import (
	"slices"
	"strconv"

	"github.com/gofiber/fiber/v3"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
)

type Success struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

type Failure struct {
	Success   bool              `json:"success"`
	Message   string            `json:"message"`
	ErrorCode string            `json:"errorCode"`
	Details   map[string]string `json:"details,omitempty"`
}

func JSON(c fiber.Ctx, status int, message string, data any) error {
	return c.Status(status).JSON(Success{
		Success: true,
		Message: message,
		Data:    data,
	})
}

// FailureNote is what a failed request leaves for the access log to read, the code and the message that the client got and the
// names of the fields that a validation refused
type FailureNote struct {
	Code    string
	Message string
	Fields  []string
}

type failureKey struct{}

// FailureOf returns the note that Error left on the request, and false when the request did not fail through Error
func FailureOf(c fiber.Ctx) (FailureNote, bool) {
	note, ok := c.Locals(failureKey{}).(FailureNote)

	return note, ok
}

func Error(c fiber.Ctx, appErr *apperror.Error) error {
	note := FailureNote{Code: appErr.Code, Message: appErr.Message}
	for field := range appErr.Details {
		note.Fields = append(note.Fields, field)
	}

	slices.Sort(note.Fields)
	c.Locals(failureKey{}, note)

	if appErr.RetryAfterSeconds > 0 {
		c.Set(fiber.HeaderRetryAfter, strconv.Itoa(appErr.RetryAfterSeconds))
	}

	return c.Status(appErr.Status).JSON(Failure{
		Success:   false,
		Message:   appErr.Message,
		ErrorCode: appErr.Code,
		Details:   appErr.Details,
	})
}
