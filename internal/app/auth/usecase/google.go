package usecase

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/googleauth"
)

func (u *AuthUseCase) SetGoogleVerifier(verifier googleauth.Verifier) {
	if u.google != nil {
		u.google.Set(verifier)
	}
}

func (u *AuthUseCase) GoogleLogin(ctx context.Context, req dto.GoogleLoginRequest) (dto.GoogleAuthResponse, error) {
	claims, err := u.verifyGoogleToken(ctx, req.IDToken)
	if err != nil {
		return dto.GoogleAuthResponse{}, err
	}

	user, err := u.findGoogleUser(ctx, claims)
	if err != nil {
		return dto.GoogleAuthResponse{}, err
	}

	if user == nil {
		return dto.GoogleAuthResponse{
			NeedsRegistration: true,
			GoogleProfile: &dto.GoogleProfile{
				Email:     normalizeEmail(claims.Email),
				FullName:  strings.TrimSpace(claims.FullName),
				AvatarURL: optionalHTTPURL(claims.AvatarURL),
			},
		}, nil
	}

	if user.Role != req.Role {
		return dto.GoogleAuthResponse{}, apperror.ErrRoleMismatch
	}

	if user.GoogleSub == nil {
		if err := u.repo.LinkGoogleSub(ctx, user.ID, claims.Subject); err != nil {
			return dto.GoogleAuthResponse{}, wrap(err)
		}

		sub := claims.Subject
		user.GoogleSub = &sub
	}

	sessionRes, err := u.startSession(ctx, user, req.DeviceToken)
	if err != nil {
		return dto.GoogleAuthResponse{}, err
	}

	return dto.GoogleAuthResponse{
		NeedsRegistration: false,
		Token:             sessionRes.Token,
		RefreshToken:      sessionRes.RefreshToken,
		DeviceToken:       sessionRes.DeviceToken,
		User:              &sessionRes.User,
	}, nil
}

func (u *AuthUseCase) GoogleRegister(ctx context.Context, req dto.GoogleRegisterRequest) (dto.AuthResponse, error) {
	claims, err := u.verifyGoogleToken(ctx, req.IDToken)
	if err != nil {
		return dto.AuthResponse{}, err
	}

	regReq := dto.RegisterRequest{
		Email:          claims.Email,
		FullName:       req.FullName,
		Role:           req.Role,
		PhoneNumber:    req.PhoneNumber,
		NIP:            req.NIP,
		SchoolName:     req.SchoolName,
		Address:        req.Address,
		WhatsappNumber: req.WhatsappNumber,
		Class:          req.Class,
		ClassCode:      req.ClassCode,
		Student:        req.Student,
	}

	if details := checkNames(regReq); len(details) > 0 {
		return dto.AuthResponse{}, apperror.Validation(details)
	}

	switch req.Role {
	case constants.RoleGuru:
		if req.Class == nil {
			return dto.AuthResponse{}, apperror.Validation(map[string]string{"class": "is required"})
		}
	case constants.RoleWali:
		if strings.TrimSpace(req.ClassCode) == "" {
			return dto.AuthResponse{}, apperror.Validation(map[string]string{"classCode": "is required"})
		}

		if req.Student == nil {
			return dto.AuthResponse{}, apperror.Validation(map[string]string{"student": "is required"})
		}
	default:
		return dto.AuthResponse{}, apperror.Validation(map[string]string{"role": "must be one of GURU WALI"})
	}

	existingBySub, err := u.repo.FindUserByGoogleSub(ctx, claims.Subject)
	if err != nil {
		return dto.AuthResponse{}, apperror.Internal(err)
	}

	if existingBySub != nil {
		return dto.AuthResponse{}, apperror.ErrGoogleAlreadyRegistered
	}

	if req.Role == constants.RoleWali {
		found, err := u.repo.ClassCodeExists(ctx, normalizeClassCode(req.ClassCode))
		if err != nil {
			return dto.AuthResponse{}, apperror.Internal(err)
		}

		if !found {
			return dto.AuthResponse{}, apperror.ErrClassCodeNotFound
		}
	}

	var nip, nisn string
	if req.Role == constants.RoleGuru {
		nip = strings.TrimSpace(req.NIP)
	} else {
		nisn = strings.TrimSpace(req.Student.NISN)
	}

	if err := u.repo.RegistrationTaken(ctx, normalizeEmail(claims.Email), nip, nisn); err != nil {
		return dto.AuthResponse{}, wrap(err)
	}

	sub := claims.Subject
	user := &entity.User{
		ID:          uuid.New(),
		Email:       normalizeEmail(claims.Email),
		GoogleSub:   &sub,
		Role:        req.Role,
		FullName:    strings.TrimSpace(req.FullName),
		PhoneNumber: optional(req.PhoneNumber),
		AvatarURL:   optionalHTTPURL(claims.AvatarURL),
	}

	raw, session := u.newRefreshToken()
	session.UserID = user.ID

	switch req.Role {
	case constants.RoleGuru:
		err = u.registerGuru(ctx, user, regReq, session)
	case constants.RoleWali:
		err = u.registerWali(ctx, user, regReq, session)
	}

	if err != nil {
		return dto.AuthResponse{}, err
	}

	res, err := u.buildResponse(user, raw)
	if err != nil {
		return dto.AuthResponse{}, err
	}

	return u.withDeviceToken(res, user, req.DeviceToken)
}

func (u *AuthUseCase) verifyGoogleToken(ctx context.Context, idToken string) (googleauth.Claims, error) {
	if u.google == nil {
		return googleauth.Claims{}, apperror.ErrGoogleUnavailable
	}

	claims, err := u.google.Verify(ctx, idToken)
	if err == nil {
		return claims, nil
	}

	if errors.Is(err, googleauth.ErrNotConfigured) {
		return googleauth.Claims{}, apperror.ErrGoogleUnavailable
	}

	if errors.Is(err, googleauth.ErrInvalidToken) {
		return googleauth.Claims{}, apperror.ErrGoogleInvalid
	}

	return googleauth.Claims{}, apperror.ErrGoogleUnavailable.WithErr(err)
}

func (u *AuthUseCase) findGoogleUser(ctx context.Context, claims googleauth.Claims) (*entity.User, error) {
	user, err := u.repo.FindUserByGoogleSub(ctx, claims.Subject)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	if user != nil {
		return user, nil
	}

	user, err = u.repo.FindUserByEmail(ctx, normalizeEmail(claims.Email))
	if err != nil {
		return nil, apperror.Internal(err)
	}

	if user == nil {
		return nil, nil
	}

	// Refuse silently switching an existing account that is already bound to a different Google subject.
	if user.GoogleSub != nil && *user.GoogleSub != claims.Subject {
		return nil, apperror.ErrGoogleInvalid
	}

	return user, nil
}

func optionalHTTPURL(raw string) *string {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "https://") || strings.HasPrefix(trimmed, "http://") {
		return &trimmed
	}

	return nil
}
