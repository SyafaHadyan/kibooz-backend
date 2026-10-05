// Package jwt issues and validates short lived access tokens
package jwt

import (
	"errors"
	"fmt"
	"time"

	golangjwt "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
)

type JWTItf interface {
	GenerateToken(userID uuid.UUID, role constants.Role) (string, error)
	ValidateToken(tokenString string) (*Claims, error)
}

type JWT struct {
	secretKey   []byte
	accessTTL   time.Duration
	clockSource func() time.Time
}

type Claims struct {
	Role constants.Role `json:"role"`
	golangjwt.RegisteredClaims
}

func (c *Claims) UserID() (uuid.UUID, error) {
	return uuid.Parse(c.Subject)
}

func New(cfg *env.Env) *JWT {
	return &JWT{
		secretKey:   []byte(cfg.JWTSecretKey),
		accessTTL:   time.Duration(cfg.JWTAccessExpiredMinutes) * time.Minute,
		clockSource: time.Now,
	}
}

func (j *JWT) GenerateToken(userID uuid.UUID, role constants.Role) (string, error) {
	now := j.clockSource()

	claims := Claims{
		Role: role,
		RegisteredClaims: golangjwt.RegisteredClaims{
			Subject:   userID.String(),
			Issuer:    constants.AccessTokenIssuer,
			IssuedAt:  golangjwt.NewNumericDate(now),
			ExpiresAt: golangjwt.NewNumericDate(now.Add(j.accessTTL)),
		},
	}

	token := golangjwt.NewWithClaims(golangjwt.SigningMethodHS256, claims)

	signed, err := token.SignedString(j.secretKey)
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}

	return signed, nil
}

func (j *JWT) ValidateToken(tokenString string) (*Claims, error) {
	claims := new(Claims)

	token, err := golangjwt.ParseWithClaims(
		tokenString,
		claims,
		func(*golangjwt.Token) (any, error) { return j.secretKey, nil },
		golangjwt.WithValidMethods([]string{golangjwt.SigningMethodHS256.Alg()}),
		golangjwt.WithIssuer(constants.AccessTokenIssuer),
		golangjwt.WithExpirationRequired(),
		golangjwt.WithTimeFunc(j.clockSource),
	)
	if err != nil {
		return nil, err
	}

	if !token.Valid {
		return nil, errors.New("token invalid")
	}

	if !claims.Role.Valid() {
		return nil, errors.New("token role invalid")
	}

	return claims, nil
}

// AccessTTL exposes the access token lifetime for clients that want expiry hints
func (j *JWT) AccessTTL() time.Duration {
	return j.accessTTL
}
