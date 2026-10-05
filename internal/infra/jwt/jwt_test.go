package jwt

import (
	"testing"
	"time"

	golangjwt "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
)

func newService(secret string, minutes int) *JWT {
	return New(&env.Env{JWTSecretKey: secret, JWTAccessExpiredMinutes: minutes})
}

func TestGenerateAndValidate(t *testing.T) {
	service := newService("0123456789abcdef0123456789abcdef", 60)
	userID := uuid.New()

	token, err := service.GenerateToken(userID, constants.RoleGuru)
	require.NoError(t, err)

	claims, err := service.ValidateToken(token)
	require.NoError(t, err)
	require.Equal(t, constants.RoleGuru, claims.Role)

	parsed, err := claims.UserID()
	require.NoError(t, err)
	require.Equal(t, userID, parsed)
}

func TestValidateRejectsExpiredToken(t *testing.T) {
	service := newService("0123456789abcdef0123456789abcdef", 60)
	service.clockSource = func() time.Time { return time.Now().Add(-2 * time.Hour) }

	token, err := service.GenerateToken(uuid.New(), constants.RoleWali)
	require.NoError(t, err)

	service.clockSource = time.Now

	_, err = service.ValidateToken(token)
	require.Error(t, err)
}

func TestValidateRejectsWrongSecret(t *testing.T) {
	token, err := newService("0123456789abcdef0123456789abcdef", 60).GenerateToken(uuid.New(), constants.RoleWali)
	require.NoError(t, err)

	_, err = newService("ffffffffffffffffffffffffffffffff", 60).ValidateToken(token)
	require.Error(t, err)
}

func TestValidateRejectsUnsignedToken(t *testing.T) {
	service := newService("0123456789abcdef0123456789abcdef", 60)

	claims := Claims{
		Role: constants.RoleAdmin,
		RegisteredClaims: golangjwt.RegisteredClaims{
			Subject:   uuid.NewString(),
			Issuer:    constants.AccessTokenIssuer,
			ExpiresAt: golangjwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}

	unsigned, err := golangjwt.NewWithClaims(golangjwt.SigningMethodNone, claims).SignedString(golangjwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	_, err = service.ValidateToken(unsigned)
	require.Error(t, err)
}

func TestValidateRejectsUnknownRole(t *testing.T) {
	service := newService("0123456789abcdef0123456789abcdef", 60)

	claims := Claims{
		Role: constants.Role("ROOT"),
		RegisteredClaims: golangjwt.RegisteredClaims{
			Subject:   uuid.NewString(),
			Issuer:    constants.AccessTokenIssuer,
			ExpiresAt: golangjwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}

	signed, err := golangjwt.NewWithClaims(golangjwt.SigningMethodHS256, claims).SignedString(service.secretKey)
	require.NoError(t, err)

	_, err = service.ValidateToken(signed)
	require.Error(t, err)
}
