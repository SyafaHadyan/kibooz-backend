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
	Email        string
	RefreshToken string
	DeviceToken  string
}

// rawAccountRequest reads the same fields as the handlers do and by the same rules, because encoding/json matches the
// names without regard to case in a struct. Each value is kept raw, so a field of the wrong type cannot make the whole
// parse fail and let a request that the handler goes on to read slip past the limiter.
type rawAccountRequest struct {
	Email        json.RawMessage `json:"email"`
	RefreshToken json.RawMessage `json:"refreshToken"`
	DeviceToken  json.RawMessage `json:"deviceToken"`
}

// text returns the value of a JSON string, and "" for a missing field or any other type
func text(raw json.RawMessage) string {
	var value string

	err := json.Unmarshal(raw, &value)
	if err != nil {
		return ""
	}

	return value
}

// limiterKeyLocal keeps the key of a request between the skip check and the limiter, so the body is parsed once
type limiterKeyLocal struct{}

// accountLimiter limits by the identifier that identify picks from the body, and each route keeps its own budget.
// The identifier is hashed so that neither an email, a refresh token nor a device id is kept in clear in the limiter storage.
// A request without an identifier is not limited, because the handler rejects it before it touches the database.
// That holds for a body that is not a JSON object and for a field that is not a string, and a field that the handler
// does read is always read here the same way.
// The route is named by the path it was registered with and not by the path the client sent, because the router ignores
// the case and a trailing slash, so each spelling of the path would otherwise get a budget of its own.
func (f *Fiber) accountLimiter(identify func(accountRequest) (string, string)) fiber.Handler {
	key := func(c fiber.Ctx) string {
		var raw rawAccountRequest

		err := json.Unmarshal(c.Body(), &raw)
		if err != nil {
			return ""
		}

		kind, identifier := identify(accountRequest{
			Email:        text(raw.Email),
			RefreshToken: text(raw.RefreshToken),
			DeviceToken:  text(raw.DeviceToken),
		})
		if identifier == "" {
			return ""
		}

		sum := sha256.Sum256([]byte(identifier))

		return kind + ":" + c.Route().Path + ":" + hex.EncodeToString(sum[:])
	}

	keyOf := func(c fiber.Ctx) string {
		if cached, ok := c.Locals(limiterKeyLocal{}).(string); ok {
			return cached
		}

		computed := key(c)
		c.Locals(limiterKeyLocal{}, computed)

		return computed
	}

	return f.newLimiter(f.authMax, keyOf, func(c fiber.Ctx) bool { return keyOf(c) == "" })
}
