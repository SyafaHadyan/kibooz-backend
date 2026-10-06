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
		Message: "Data yang dikirim tidak valid",
		Details: details,
	}
}

// Internal wraps an unexpected failure without leaking details to the client
func Internal(err error) *Error {
	return &Error{
		Status:  http.StatusInternalServerError,
		Code:    "INTERNAL_ERROR",
		Message: "Terjadi kesalahan pada server",
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
	ErrInvalidCredentials = New(http.StatusUnauthorized, "AUTH_INVALID_CREDENTIALS", "Email atau kata sandi tidak cocok untuk peran yang dipilih")
	ErrTokenMissing       = New(http.StatusUnauthorized, "AUTH_TOKEN_MISSING", "Token autentikasi tidak ditemukan")
	ErrTokenInvalid       = New(http.StatusUnauthorized, "AUTH_TOKEN_INVALID", "Token autentikasi tidak valid atau sudah kedaluwarsa")
	ErrRefreshInvalid     = New(http.StatusUnauthorized, "AUTH_REFRESH_INVALID", "Sesi telah berakhir, silakan masuk kembali")
	ErrForbidden          = New(http.StatusForbidden, "AUTH_FORBIDDEN", "Anda tidak memiliki akses ke sumber daya ini")
	ErrPasswordIncorrect  = New(http.StatusForbidden, "AUTH_PASSWORD_INCORRECT", "Incorrect password")
	ErrEmailTaken         = New(http.StatusConflict, "EMAIL_ALREADY_REGISTERED", "Email sudah terdaftar")
	ErrNIPTaken           = New(http.StatusConflict, "NIP_ALREADY_REGISTERED", "NIP sudah terdaftar")
	ErrNISNTaken          = New(http.StatusConflict, "NISN_ALREADY_REGISTERED", "NISN sudah terdaftar")
	ErrClassNotFound      = New(http.StatusNotFound, "CLASS_NOT_FOUND", "Kelas tidak ditemukan")
	ErrClassCodeNotFound  = New(http.StatusNotFound, "CLASS_CODE_NOT_FOUND", "Kode kelas tidak ditemukan")
	ErrStudentNotFound    = New(http.StatusNotFound, "STUDENT_NOT_FOUND", "Siswa tidak ditemukan")
	ErrProfileNotFound    = New(http.StatusNotFound, "PROFILE_NOT_FOUND", "Profil pengguna tidak ditemukan")
	ErrGuidanceNotFound   = New(http.StatusNotFound, "GUIDANCE_NOT_FOUND", "Panduan penanganan tidak ditemukan")
	ErrDailyLimitReached  = New(http.StatusTooManyRequests, "TRASH_DAILY_LIMIT_REACHED", "Batas klaim poin pilah sampah hari ini sudah tercapai")
	ErrInvalidImage       = New(http.StatusBadRequest, "INVALID_IMAGE", "Berkas harus berupa gambar JPEG, PNG, atau WebP")
	ErrFileTooLarge       = New(http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "Ukuran berkas melebihi batas yang diizinkan")
	ErrStorageDisabled    = New(http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "Penyimpanan berkas belum dikonfigurasi")
	ErrStorageFailed      = New(http.StatusBadGateway, "STORAGE_ERROR", "Gagal menyimpan berkas")
	ErrRateLimited        = New(http.StatusTooManyRequests, "RATE_LIMITED", "Terlalu banyak permintaan, coba lagi nanti")
	ErrNotFound           = New(http.StatusNotFound, "NOT_FOUND", "Sumber daya tidak ditemukan")
)
