// Package middleware authenticates requests and enforces role based access
package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/jwt"
)

type localsKey string

const (
	userIDKey localsKey = "userID"
	roleKey   localsKey = "role"
)

type MiddlewareItf interface {
	Authentication(c fiber.Ctx) error
	RequireRole(roles ...constants.Role) fiber.Handler
}

type Middleware struct {
	jwt   jwt.JWTItf
	limit fiber.Handler
}

// NewMiddleware builds the authentication and role checks. The limit handler runs right after a request is
// authenticated, so it can use UserKey, and a nil limit lets every authenticated request through.
func NewMiddleware(jwt jwt.JWTItf, limit fiber.Handler) MiddlewareItf {
	if limit == nil {
		limit = func(c fiber.Ctx) error { return c.Next() }
	}

	return &Middleware{jwt: jwt, limit: limit}
}

func (m *Middleware) Authentication(c fiber.Ctx) error {
	header := c.Get(fiber.HeaderAuthorization)
	if header == "" {
		return apperror.ErrTokenMissing
	}

	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return apperror.ErrTokenInvalid
	}

	claims, err := m.jwt.ValidateToken(strings.TrimSpace(token))
	if err != nil {
		return apperror.ErrTokenInvalid
	}

	userID, err := claims.UserID()
	if err != nil {
		return apperror.ErrTokenInvalid
	}

	fiber.Locals(c, userIDKey, userID)
	fiber.Locals(c, roleKey, claims.Role)

	return m.limit(c)
}

func (m *Middleware) RequireRole(roles ...constants.Role) fiber.Handler {
	return func(c fiber.Ctx) error {
		role := RoleFrom(c)

		for _, allowed := range roles {
			if role == allowed {
				return c.Next()
			}
		}

		return apperror.ErrForbidden
	}
}

// UserIDFrom returns the authenticated user id set by Authentication
func UserIDFrom(c fiber.Ctx) uuid.UUID {
	return fiber.Locals[uuid.UUID](c, userIDKey)
}

// UserKey names the authenticated user for the rate limiters, it is only set after Authentication
func UserKey(c fiber.Ctx) string {
	return UserIDFrom(c).String()
}

// RoleFrom returns the authenticated role set by Authentication
func RoleFrom(c fiber.Ctx) constants.Role {
	return fiber.Locals[constants.Role](c, roleKey)
}
