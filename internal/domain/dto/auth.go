// Package dto defines request and response payloads of the REST API
package dto

import (
	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
)

type RegisterClass struct {
	Name         string `json:"name" validate:"required,min=1,max=50"`
	GradeLevel   string `json:"gradeLevel" validate:"omitempty,max=20"`
	AcademicYear string `json:"academicYear" validate:"omitempty,max=20"`
	SchoolName   string `json:"schoolName" validate:"omitempty,max=150"`
}

type RegisterStudent struct {
	NISN     string `json:"nisn" validate:"required,numeric,min=5,max=30"`
	FullName string `json:"fullName" validate:"required,min=2,max=150"`
}

// RegisterRequest creates a GURU together with a new class, or a WALI together with a child who joins a class by code
type RegisterRequest struct {
	Email          string           `json:"email" validate:"required,email,max=255"`
	Password       string           `json:"password" validate:"required,min=8,max=72"`
	FullName       string           `json:"fullName" validate:"required,min=2,max=150"`
	Role           constants.Role   `json:"role" validate:"required,oneof=GURU WALI"`
	PhoneNumber    string           `json:"phoneNumber" validate:"omitempty,max=30"`
	NIP            string           `json:"nip" validate:"omitempty,max=50"`
	SchoolName     string           `json:"schoolName" validate:"omitempty,max=150"`
	Class          *RegisterClass   `json:"class" validate:"required_if=Role GURU"`
	Address        string           `json:"address" validate:"omitempty,max=1000"`
	WhatsappNumber string           `json:"whatsappNumber" validate:"omitempty,max=30"`
	ClassCode      string           `json:"classCode" validate:"required_if=Role WALI,omitempty,alphanum,min=4,max=12"`
	Student        *RegisterStudent `json:"student" validate:"required_if=Role WALI"`
}

type LoginRequest struct {
	Email    string         `json:"email" validate:"required,email,max=255"`
	Password string         `json:"password" validate:"required,max=72"`
	Role     constants.Role `json:"role" validate:"required,oneof=GURU WALI ADMIN"`

	// DeviceToken is the one that an earlier register or login on this device returned, it only affects the rate limit
	DeviceToken string `json:"deviceToken" validate:"omitempty,max=256"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refreshToken" validate:"required,max=128"`
}

type LogoutRequest struct {
	RefreshToken string `json:"refreshToken" validate:"required,max=128"`
}

type UserResponse struct {
	ID        uuid.UUID      `json:"id"`
	Email     string         `json:"email"`
	FullName  string         `json:"fullName"`
	Role      constants.Role `json:"role"`
	AvatarURL *string        `json:"avatarUrl"`
}

type AuthResponse struct {
	Token        string       `json:"token"`
	RefreshToken string       `json:"refreshToken"`
	User         UserResponse `json:"user"`

	// DeviceToken is set by register and login, and is empty for a token refresh
	DeviceToken string `json:"deviceToken,omitempty"`
}
