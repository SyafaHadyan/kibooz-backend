package fiber

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/limiter"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
)

// None of these limits looks at the IP address. A school network or an ISP puts many people behind one public address,
// so the address says little about who is asking and one noisy person would throttle everyone else on it.
// Signed-in requests are limited per user and the public auth routes per account.

func (f *Fiber) newLimiter(max int, key func(fiber.Ctx) string, skip func(fiber.Ctx) bool) fiber.Handler {
	return limiter.New(limiter.Config{
		Max:               max,
		Expiration:        f.window,
		Storage:           f.storage,
		KeyGenerator:      key,
		Next:              skip,
		LimiterMiddleware: limiter.SlidingWindow{},
		LimitReached: func(fiber.Ctx) error {
			return apperror.ErrRateLimited
		},
	})
}

// UserLimiter gives every signed-in user USER_LIMITER_MAX requests per window. userKey names the user
// and the limiter has to run after authentication, which is why the middleware package calls it.
func (f *Fiber) UserLimiter(userKey func(fiber.Ctx) string) fiber.Handler {
	return f.newLimiter(f.userMax, func(c fiber.Ctx) string { return "user:" + userKey(c) }, nil)
}

// PasswordLimiter gives every signed-in user AUTH_LIMITER_MAX attempts per window at a request that confirms the
// password, so a stolen access token cannot be used to guess it. It runs after authentication as well.
func (f *Fiber) PasswordLimiter(userKey func(fiber.Ctx) string) fiber.Handler {
	return f.newLimiter(f.authMax, func(c fiber.Ctx) string { return "password:" + userKey(c) }, nil)
}

// AccountLimiter gives every account AUTH_LIMITER_MAX requests per window on each public auth route, so guessing
// the password of one account is stopped from any number of addresses. A request that names no account is not limited.
func (f *Fiber) AccountLimiter() fiber.Handler {
	return f.newLimiter(f.authMax, accountKey, func(c fiber.Ctx) bool { return accountKey(c) == "" })
}

type accountRequest struct {
	Email        string `json:"email"`
	RefreshToken string `json:"refreshToken"`
}

// accountKey names the account that a public auth request is about, or returns an empty string when it names none.
// The email is normalized like the sign in does, and both identifiers are hashed so that neither an address nor a
// refresh token is kept in clear in the limiter storage.
func accountKey(c fiber.Ctx) string {
	var body accountRequest
	if err := json.Unmarshal(c.Body(), &body); err != nil {
		return ""
	}

	identifier := strings.ToLower(strings.TrimSpace(body.Email))
	if identifier == "" {
		identifier = body.RefreshToken
	}

	if identifier == "" {
		return ""
	}

	sum := sha256.Sum256([]byte(identifier))

	return "account:" + c.Path() + ":" + hex.EncodeToString(sum[:])
}
