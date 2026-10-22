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
	// RetryAfterSeconds is sent as a Retry-After header when it is above 0
	RetryAfterSeconds int
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
	ErrNoChild            = New(http.StatusNotFound, "NO_CHILD_REGISTERED", "This account has no child, add one with the code of a class")
	ErrStudentNotFound    = New(http.StatusNotFound, "STUDENT_NOT_FOUND", "Student not found")
	ErrChildLimitReached  = New(http.StatusConflict, "CHILD_LIMIT_REACHED", "This account already has the largest number of children")
	ErrClassLimitReached  = New(http.StatusConflict, "CLASS_LIMIT_REACHED", "This teacher already teaches the largest number of classes")
	ErrClassJoiningClosed = New(http.StatusConflict, "CLASS_JOINING_CLOSED", "This class is not taking new children right now")
	ErrClassFull          = New(http.StatusConflict, "CLASS_FULL", "This class has the largest number of children")
	ErrMoodLimitReached   = New(http.StatusConflict, "MOOD_LOG_LIMIT_REACHED", "This child already has the largest number of mood logs for that day")
	ErrProfileNotFound    = New(http.StatusNotFound, "PROFILE_NOT_FOUND", "User profile not found")
	ErrGuidanceNotFound   = New(http.StatusNotFound, "GUIDANCE_NOT_FOUND", "Guidance not found")
	ErrTeacherNotFound    = New(http.StatusNotFound, "TEACHER_NOT_FOUND", "No teacher account has this email")
	ErrTeacherInClass     = New(http.StatusConflict, "TEACHER_ALREADY_IN_CLASS", "This teacher already teaches the class")
	ErrTeacherNotInClass  = New(http.StatusNotFound, "TEACHER_NOT_IN_CLASS", "This teacher does not teach the class")
	ErrNotClassOwner      = New(http.StatusForbidden, "CLASS_OWNER_REQUIRED", "Only the owner of the class can do this")
	ErrSuccessorRequired  = New(http.StatusConflict, "SUCCESSOR_REQUIRED", "You are the only teacher of a class, so name another teacher as the successor to delete your account")
	ErrLastTeacher        = New(http.StatusConflict, "LAST_TEACHER", "The only teacher of a class cannot leave it")
	ErrForumPostNotFound  = New(http.StatusNotFound, "FORUM_POST_NOT_FOUND", "Forum post not found")
	ErrVideoNotFound      = New(http.StatusNotFound, "VIDEO_NOT_FOUND", "Learning video not found")
	ErrDailyLimitReached  = New(http.StatusTooManyRequests, "TRASH_DAILY_LIMIT_REACHED", "Today's limit for trash sorting point claims has been reached")
	ErrInvalidImage       = New(http.StatusBadRequest, "INVALID_IMAGE", "The file must be a JPEG, PNG, or WebP image")
	ErrFileTooLarge       = New(http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "The file size exceeds the allowed limit")
	ErrBodyTooLarge       = New(http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "The request body exceeds the allowed limit")
	ErrImageDimensions    = New(http.StatusBadRequest, "IMAGE_DIMENSIONS_TOO_LARGE", "The image may be at most 8192 pixels on a side and 40 megapixels in all")
	ErrStorageDisabled    = New(http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "File storage is not configured")
	ErrStorageFailed      = New(http.StatusBadGateway, "STORAGE_ERROR", "Failed to store the file")
	ErrIdempotencyReused  = New(http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REUSED", "This Idempotency-Key was used for a different request")
	ErrIdempotencyBusy    = New(http.StatusConflict, "IDEMPOTENCY_IN_PROGRESS", "A request with this Idempotency-Key is still running, try again in a moment")
	ErrRateLimited        = New(http.StatusTooManyRequests, "RATE_LIMITED", "Too many requests, please try again later")
	// ErrBusy answers a request that needed a password hash while every slot of the server was in use. It has the code of a
	// rate limit, so an app already knows to wait, and it says for how long.
	ErrBusy = &Error{
		Status: http.StatusTooManyRequests, Code: "RATE_LIMITED", Message: "The server is busy, please try again in a moment", RetryAfterSeconds: 1,
	}
	ErrNotFound = New(http.StatusNotFound, "NOT_FOUND", "Resource not found")
)

// errors of the chat, in a block of their own so that their longer names do not realign the list above
var (
	ErrChatConversationNotFound = New(http.StatusNotFound, "CHAT_CONVERSATION_NOT_FOUND", "Conversation not found")
	ErrChatMessageNotFound      = New(http.StatusNotFound, "CHAT_MESSAGE_NOT_FOUND", "Message not found")
	ErrChatContactNotAllowed    = New(
		http.StatusForbidden, "CHAT_CONTACT_NOT_ALLOWED", "You can only talk to a teacher or a parent that you share a class with",
	)
	ErrChatSendingClosed = New(
		http.StatusConflict, "CHAT_SENDING_CLOSED",
		"This conversation is closed, so you can read it but not write in it",
	)
	ErrChatSocketsFull = New(http.StatusServiceUnavailable, "CHAT_SOCKETS_FULL", "The server cannot take another chat connection right now")
)

// errors of the move of a child to another class, in a block of their own for the same reason
var (
	ErrAlreadyInClass = New(http.StatusConflict, "ALREADY_IN_CLASS", "This child is already in that class")
	ErrMovePending    = New(http.StatusConflict, "MOVE_REQUEST_PENDING", "This child already has a request that waits for a teacher")
	ErrMoveNotFound   = New(http.StatusNotFound, "MOVE_REQUEST_NOT_FOUND", "Move request not found")
)

// errors of an archived class, which is read only until a teacher restores it
var (
	ErrClassArchived    = New(http.StatusConflict, "CLASS_ARCHIVED", "This class is archived and read only")
	ErrClassNotArchived = New(http.StatusConflict, "CLASS_NOT_ARCHIVED", "This class is not archived")
)
