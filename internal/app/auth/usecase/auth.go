// Package usecase holds the authentication business rules
package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/auth/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/jwt"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/redis"
)

const (
	bcryptMaxBytes = 72

	// usedTTL is how long Redis remembers a consumed token to reject quick replays
	usedTTL = time.Minute
)

// dummyHash lets login spend the same time for unknown emails as for known ones
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("kibooz-dummy-password"), bcrypt.DefaultCost)

type AuthUseCaseItf interface {
	Register(ctx context.Context, req dto.RegisterRequest) (dto.AuthResponse, error)
	Login(ctx context.Context, req dto.LoginRequest) (dto.AuthResponse, error)
	Refresh(ctx context.Context, refreshToken string) (dto.AuthResponse, error)
	Logout(ctx context.Context, refreshToken string) error
}

type AuthUseCase struct {
	repo  repository.AuthDBItf
	jwt   jwt.JWTItf
	cache redis.CacheItf
	cfg   *env.Env
	now   func() time.Time
}

func NewAuthUseCase(repo repository.AuthDBItf, jwt jwt.JWTItf, cache redis.CacheItf, cfg *env.Env) AuthUseCaseItf {
	return &AuthUseCase{repo: repo, jwt: jwt, cache: cache, cfg: cfg, now: time.Now}
}

func (u *AuthUseCase) Register(ctx context.Context, req dto.RegisterRequest) (dto.AuthResponse, error) {
	if len([]byte(req.Password)) > bcryptMaxBytes {
		//nolint:gosec // this is a validation message and not a credential
		return dto.AuthResponse{}, apperror.Validation(map[string]string{"password": "too long, maximum 72 bytes"})
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return dto.AuthResponse{}, apperror.Internal(err)
	}

	user := &entity.User{
		ID:           uuid.New(),
		Email:        normalizeEmail(req.Email),
		PasswordHash: string(hash),
		Role:         req.Role,
		FullName:     strings.TrimSpace(req.FullName),
		PhoneNumber:  optional(req.PhoneNumber),
	}

	switch req.Role {
	case constants.RoleGuru:
		err = u.registerGuru(ctx, user, req)
	case constants.RoleWali:
		err = u.registerWali(ctx, user, req)
	default:
		return dto.AuthResponse{}, apperror.Validation(map[string]string{"role": "must be one of GURU WALI"})
	}

	if err != nil {
		return dto.AuthResponse{}, err
	}

	return u.issueSession(ctx, user)
}

func (u *AuthUseCase) registerGuru(ctx context.Context, user *entity.User, req dto.RegisterRequest) error {
	school := firstNonEmpty(req.Class.SchoolName, req.SchoolName, constants.DefaultSchoolName)

	guru := &entity.Guru{
		ID:         uuid.New(),
		UserID:     user.ID,
		NIP:        optional(req.NIP),
		SchoolName: school,
	}

	class := &entity.Class{
		ID:           uuid.New(),
		SchoolName:   school,
		Name:         strings.TrimSpace(req.Class.Name),
		GradeLevel:   firstNonEmpty(req.Class.GradeLevel, constants.DefaultGradeLevel),
		AcademicYear: firstNonEmpty(req.Class.AcademicYear, constants.DefaultAcademicYr),
	}

	return wrap(u.repo.CreateGuru(ctx, user, guru, class))
}

func (u *AuthUseCase) registerWali(ctx context.Context, user *entity.User, req dto.RegisterRequest) error {
	wali := &entity.Wali{
		ID:             uuid.New(),
		UserID:         user.ID,
		Address:        optional(req.Address),
		WhatsappNumber: optional(req.WhatsappNumber),
	}

	student := &entity.Student{
		ID:       uuid.New(),
		NISN:     strings.TrimSpace(req.Student.NISN),
		FullName: strings.TrimSpace(req.Student.FullName),
	}

	classID, err := u.repo.CreateWali(ctx, user, wali, strings.ToUpper(strings.TrimSpace(req.ClassCode)), student)
	if err != nil {
		return wrap(err)
	}

	// a new child changes the ranking so the cached leaderboard is stale
	_ = u.cache.Del(ctx, constants.LeaderboardKeyPrefix+classID.String())

	return nil
}

func (u *AuthUseCase) Login(ctx context.Context, req dto.LoginRequest) (dto.AuthResponse, error) {
	user, err := u.repo.FindUserByEmail(ctx, normalizeEmail(req.Email))
	if err != nil {
		return dto.AuthResponse{}, apperror.Internal(err)
	}

	if user == nil {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(req.Password))

		return dto.AuthResponse{}, apperror.ErrInvalidCredentials
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password))
	if err != nil || user.Role != req.Role {
		return dto.AuthResponse{}, apperror.ErrInvalidCredentials
	}

	return u.issueSession(ctx, user)
}

// Refresh swaps a refresh token for a new session. Postgres alone decides whether the token is valid,
// Redis only rejects obvious replays earlier and its absence changes nothing.
func (u *AuthUseCase) Refresh(ctx context.Context, refreshToken string) (dto.AuthResponse, error) {
	hash := hashToken(refreshToken)

	used, err := u.cache.MarkUsed(ctx, refreshKey(hash), usedTTL)
	if err == nil && used {
		return dto.AuthResponse{}, apperror.ErrRefreshInvalid
	}

	raw, next := u.newRefreshToken()

	user, err := u.repo.RotateRefreshToken(ctx, hash, u.now(), next)
	if err != nil {
		return dto.AuthResponse{}, apperror.Internal(err)
	}

	if user == nil {
		return dto.AuthResponse{}, apperror.ErrRefreshInvalid
	}

	u.remember(ctx, next)

	return u.buildResponse(user, raw)
}

func (u *AuthUseCase) Logout(ctx context.Context, refreshToken string) error {
	hash := hashToken(refreshToken)

	err := u.repo.DeleteRefreshToken(ctx, hash)
	if err != nil {
		return apperror.Internal(err)
	}

	_ = u.cache.Del(ctx, refreshKey(hash))

	return nil
}

func (u *AuthUseCase) issueSession(ctx context.Context, user *entity.User) (dto.AuthResponse, error) {
	raw, row := u.newRefreshToken()
	row.UserID = user.ID

	err := u.repo.CreateRefreshToken(ctx, row)
	if err != nil {
		return dto.AuthResponse{}, apperror.Internal(err)
	}

	u.remember(ctx, row)

	return u.buildResponse(user, raw)
}

func (u *AuthUseCase) buildResponse(user *entity.User, refreshToken string) (dto.AuthResponse, error) {
	token, err := u.jwt.GenerateToken(user.ID, user.Role)
	if err != nil {
		return dto.AuthResponse{}, apperror.Internal(err)
	}

	return dto.AuthResponse{
		Token:        token,
		RefreshToken: refreshToken,
		User: dto.UserResponse{
			ID:        user.ID,
			Email:     user.Email,
			FullName:  user.FullName,
			Role:      user.Role,
			AvatarURL: user.AvatarURL,
		},
	}, nil
}

// newRefreshToken returns the secret for the client and the row to store, which only holds its hash
func (u *AuthUseCase) newRefreshToken() (string, *entity.RefreshToken) {
	raw := uuid.NewString()
	now := u.now()

	return raw, &entity.RefreshToken{
		ID:        uuid.New(),
		TokenHash: hashToken(raw),
		ExpiresAt: now.Add(time.Duration(u.cfg.JWTRefreshExpiredDays) * 24 * time.Hour),
		CreatedAt: now,
	}
}

// remember lets Redis know the token exists so a later replay can be flagged. Failures are ignored on purpose.
func (u *AuthUseCase) remember(ctx context.Context, token *entity.RefreshToken) {
	ttl := time.Until(token.ExpiresAt)
	if ttl <= 0 {
		return
	}

	_ = u.cache.Set(ctx, refreshKey(token.TokenHash), token.UserID.String(), ttl)
}

func refreshKey(hash string) string {
	return constants.RefreshKeyPrefix + hash
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))

	return hex.EncodeToString(sum[:])
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func optional(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	return &value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}

	return ""
}

// wrap keeps application errors intact and marks everything else as internal
func wrap(err error) error {
	if err == nil {
		return nil
	}

	return apperror.As(err)
}
