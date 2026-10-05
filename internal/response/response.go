// Package response builds the JSON envelope shared by every endpoint
package response

import (
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

func Error(c fiber.Ctx, appErr *apperror.Error) error {
	return c.Status(appErr.Status).JSON(Failure{
		Success:   false,
		Message:   appErr.Message,
		ErrorCode: appErr.Code,
		Details:   appErr.Details,
	})
}
