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
	return f.newLimiter(f.authMax, func(c fiber.Ctx) string { return "confirm:" + userKey(c) }, nil)
}

// EmailLimiter gives every email AUTH_LIMITER_MAX requests per window on each route it guards, which are the routes
// that take an email, so guessing the password of one account is stopped from any number of addresses.
// A device that sends a valid device token for the email has a budget of its own, so someone without that token,
// who can only use up the shared budget of the email, cannot lock out a device that has signed in before.
// A request that names no email is not limited.
func (f *Fiber) EmailLimiter() fiber.Handler {
	return f.accountLimiter(func(body accountRequest) (string, string) {
		email := strings.ToLower(strings.TrimSpace(body.Email))

		if deviceID, ok := f.devices.Verify(body.DeviceToken, email); ok && email != "" {
			return "device", hex.EncodeToString(deviceID)
		}

		return "email", email
	})
}

// TokenLimiter gives every refresh token AUTH_LIMITER_MAX requests per window on each route it guards. Those routes
// read only the token, so an email in the same body must not give a caller a new budget.
func (f *Fiber) TokenLimiter() fiber.Handler {
	return f.accountLimiter(func(body accountRequest) (string, string) {
		return "token", body.RefreshToken
	})
}

type accountRequest struct {
	Email        string `json:"email"`
	RefreshToken string `json:"refreshToken"`
	DeviceToken  string `json:"deviceToken"`
}

// accountLimiter limits by the identifier that identify picks from the body, and each route keeps its own budget.
// The identifier is hashed so that neither an email, a refresh token nor a device id is kept in clear in the limiter storage.
// A request without an identifier is not limited, because it is rejected before it touches the database.
func (f *Fiber) accountLimiter(identify func(accountRequest) (string, string)) fiber.Handler {
	key := func(c fiber.Ctx) string {
		var body accountRequest
		if err := json.Unmarshal(c.Body(), &body); err != nil {
			return ""
		}

		kind, identifier := identify(body)
		if identifier == "" {
			return ""
		}

		sum := sha256.Sum256([]byte(identifier))

		return kind + ":" + c.Path() + ":" + hex.EncodeToString(sum[:])
	}

	return f.newLimiter(f.authMax, key, func(c fiber.Ctx) bool { return key(c) == "" })
}
