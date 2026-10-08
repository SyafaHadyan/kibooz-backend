// Package apperror defines typed application errors that map directly to API error responses
package apperror

import (
	"errors"
	"net/http"
)

type Error struct {
	Status  int
	Code    string
	Message string
	Details map[string]string
	Err     error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return e.Code + " " + e.Err.Error()
	}

	return e.Code + " " + e.Message
}

func (e *Error) Unwrap() error {
	return e.Err
}

func New(status int, code string, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// WithErr returns a copy of the error that keeps the underlying cause for logging
func (e *Error) WithErr(err error) *Error {
	clone := *e
	clone.Err = err

	return &clone
}

// Validation builds a 400 error carrying per-field details
func Validation(details map[string]string) *Error {
	return &Error{
		Status:  http.StatusBadRequest,
		Code:    "VALIDATION_ERROR",
		Message: "The submitted data is invalid",
		Details: details,
	}
}

// Internal wraps an unexpected failure without leaking details to the client
func Internal(err error) *Error {
	return &Error{
		Status:  http.StatusInternalServerError,
		Code:    "INTERNAL_ERROR",
		Message: "An internal server error occurred",
		Err:     err,
	}
}

// As converts any error to an application error, falling back to Internal
func As(err error) *Error {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr
	}

	return Internal(err)
}

var (
	ErrInvalidCredentials = New(http.StatusUnauthorized, "AUTH_INVALID_CREDENTIALS", "Email or password does not match the selected role")
	ErrTokenMissing       = New(http.StatusUnauthorized, "AUTH_TOKEN_MISSING", "Authentication token not found")
	ErrTokenInvalid       = New(http.StatusUnauthorized, "AUTH_TOKEN_INVALID", "Authentication token is invalid or has expired")
	ErrRefreshInvalid     = New(http.StatusUnauthorized, "AUTH_REFRESH_INVALID", "Your session has ended, please sign in again")
	ErrForbidden          = New(http.StatusForbidden, "AUTH_FORBIDDEN", "You do not have access to this resource")
	ErrPasswordIncorrect  = New(http.StatusForbidden, "AUTH_PASSWORD_INCORRECT", "Incorrect password")
	ErrEmailTaken         = New(http.StatusConflict, "EMAIL_ALREADY_REGISTERED", "Email is already registered")
	ErrNIPTaken           = New(http.StatusConflict, "NIP_ALREADY_REGISTERED", "NIP is already registered")
	ErrNISNTaken          = New(http.StatusConflict, "NISN_ALREADY_REGISTERED", "NISN is already registered")
	ErrClassNotFound      = New(http.StatusNotFound, "CLASS_NOT_FOUND", "Class not found")
	ErrClassNoTeacher     = New(http.StatusConflict, "CLASS_NO_ACTIVE_TEACHER", "This class has no active teacher and cannot accept new students")
	ErrClassCodeNotFound  = New(http.StatusNotFound, "CLASS_CODE_NOT_FOUND", "Class code not found")
	ErrStudentNotFound    = New(http.StatusNotFound, "STUDENT_NOT_FOUND", "Student not found")
	ErrProfileNotFound    = New(http.StatusNotFound, "PROFILE_NOT_FOUND", "User profile not found")
	ErrGuidanceNotFound   = New(http.StatusNotFound, "GUIDANCE_NOT_FOUND", "Guidance not found")
	ErrForumPostNotFound  = New(http.StatusNotFound, "FORUM_POST_NOT_FOUND", "Forum post not found")
	ErrVideoNotFound      = New(http.StatusNotFound, "VIDEO_NOT_FOUND", "Learning video not found")
	ErrDailyLimitReached  = New(http.StatusTooManyRequests, "TRASH_DAILY_LIMIT_REACHED", "Today's limit for trash sorting point claims has been reached")
	ErrInvalidImage       = New(http.StatusBadRequest, "INVALID_IMAGE", "The file must be a JPEG, PNG, or WebP image")
	ErrFileTooLarge       = New(http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "The file size exceeds the allowed limit")
	ErrStorageDisabled    = New(http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "File storage is not configured")
	ErrStorageFailed      = New(http.StatusBadGateway, "STORAGE_ERROR", "Failed to store the file")
	ErrRateLimited        = New(http.StatusTooManyRequests, "RATE_LIMITED", "Too many requests, please try again later")
	ErrNotFound           = New(http.StatusNotFound, "NOT_FOUND", "Resource not found")
)
