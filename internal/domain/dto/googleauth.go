package dto

import "github.com/SyafaHadyan/kibooz-backend/internal/constants"

type GoogleLoginRequest struct {
	IDToken     string         `json:"idToken" validate:"required,max=4096"`
	Role        constants.Role `json:"role" validate:"required,oneof=GURU WALI"`
	DeviceToken string         `json:"deviceToken" validate:"max=256"`
}

type GoogleProfile struct {
	Email     string  `json:"email"`
	FullName  string  `json:"fullName"`
	AvatarURL *string `json:"avatarUrl"`
}

type GoogleAuthResponse struct {
	NeedsRegistration bool           `json:"needsRegistration"`
	GoogleProfile     *GoogleProfile `json:"googleProfile,omitempty"`
	Token             string         `json:"token,omitempty"`
	RefreshToken      string         `json:"refreshToken,omitempty"`
	DeviceToken       string         `json:"deviceToken,omitempty"`
	User              *UserResponse  `json:"user,omitempty"`
}

type GoogleRegisterRequest struct {
	IDToken        string           `json:"idToken" validate:"required,max=4096"`
	FullName       string           `json:"fullName" validate:"required,min=2,max=150"`
	Role           constants.Role   `json:"role" validate:"required,oneof=GURU WALI"`
	PhoneNumber    string           `json:"phoneNumber" validate:"max=30"`
	NIP            string           `json:"nip" validate:"max=50"`
	SchoolName     string           `json:"schoolName" validate:"max=150"`
	Address        string           `json:"address" validate:"max=1000"`
	WhatsappNumber string           `json:"whatsappNumber" validate:"max=30"`
	Class          *RegisterClass   `json:"class" validate:"required_if=Role GURU"`
	ClassCode      string           `json:"classCode" validate:"required_if=Role WALI,omitempty,alphanum,min=4,max=12"`
	Student        *RegisterStudent `json:"student" validate:"required_if=Role WALI"`
	DeviceToken    string           `json:"deviceToken" validate:"max=256"`
}
