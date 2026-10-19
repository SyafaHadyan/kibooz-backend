package fiber

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
)

// None of these limits looks at the IP address. A school network or an ISP puts many people behind one public address,
// so the address says little about who is asking and one noisy person would throttle everyone else on it.
// Signed-in requests are limited per user and the public auth routes per account.

// newLimiter lets limit requests per window through for each key and refuses the rest with 429 and a Retry-After header.
// A request is counted before it is judged, so one that is refused counts too and a client that keeps hammering stays
// refused. The counting is left to the store, which is atomic when Redis backs it, so the handler holds no lock while
// it waits for Redis and instances that share a Redis share the limit.
func (f *Fiber) newLimiter(limit int, key func(fiber.Ctx) string, skip func(fiber.Ctx) bool) fiber.Handler {
	return func(c fiber.Ctx) error {
		if skip != nil && skip(c) {
			return c.Next()
		}

		hits, err := f.limits.Hit(c.Context(), key(c), f.window)
		if err != nil {
			// a store that cannot count must not take the API down with it
			return c.Next()
		}

		used := hits.Estimate(f.window)
		refused := used > int64(limit)

		// the end of the window is when the counter starts over, but the previous window still weighs on the next one, so
		// a client that was refused is told when a request fits again and not when the window ends
		reset := f.window - hits.Elapsed
		if refused {
			reset = hits.RetryAfter(f.window, int64(limit))
		}

		untilReset := strconv.Itoa(max(int((reset+time.Second-1)/time.Second), 1))

		c.Set("X-RateLimit-Limit", strconv.Itoa(limit))
		c.Set("X-RateLimit-Remaining", strconv.FormatInt(max(int64(limit)-used, 0), 10))
		c.Set("X-RateLimit-Reset", untilReset)

		if refused {
			c.Set(fiber.HeaderRetryAfter, untilReset)

			return apperror.ErrRateLimited
		}

		return c.Next()
	}
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

// MoodBatchLimiter gives every signed-in user MOOD_BATCH_LIMITER_MAX requests per window on the batch mood route, on top of
// the limit of every signed-in route. One batch writes up to 30 rows under a lock of the class, so it costs more than an
// ordinary request. It runs after authentication, like UserLimiter.
func (f *Fiber) MoodBatchLimiter(userKey func(fiber.Ctx) string) fiber.Handler {
	return f.newLimiter(f.moodBatchMax, func(c fiber.Ctx) string { return "moodbatch:" + userKey(c) }, nil)
}

// EmailLimiter gives every email AUTH_LIMITER_MAX requests per window on each route it guards, which are the routes
// that take an email, so guessing the password of one account is stopped from any number of addresses.
// A device that sends a valid device token for the email has a budget of its own, so someone without that token,
// who can only use up the shared budget of the email, cannot lock out a device that has signed in before.
// A request that names no email is not limited.
func (f *Fiber) EmailLimiter() fiber.Handler {
	return f.accountLimiter(f.authMax, func(body accountRequest) (string, string) {
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
	return f.accountLimiter(f.authMax, func(body accountRequest) (string, string) {
		return "token", body.RefreshToken
	})
}

// ClassCodeLimiter gives each route CLASS_CODE_LIMITER_MAX requests per window that name a class code. A code is the
// only secret between a stranger and a class, and a wrong code is answered at once, so guessing is limited by the number
// of guesses and not by who sends them. The budget of a route is shared by every caller, because a guesser can use any
// number of emails, devices and addresses, and the key holds the path, so each route has a budget of its own. A request
// that names no class code looks nothing up and is not limited.
func (f *Fiber) ClassCodeLimiter() fiber.Handler {
	return f.accountLimiter(f.classCodeMax, func(body accountRequest) (string, string) {
		if body.ClassCode == "" {
			return "", ""
		}

		return "classcode", "all"
	})
}

type accountRequest struct {
	Email        string
	RefreshToken string
	DeviceToken  string
	ClassCode    string
}

// rawAccountRequest reads the same fields as the handlers do and by the same rules, because encoding/json matches the
// names without regard to case in a struct. Each value goes through jsonText, so a field of the wrong type cannot make the
// whole parse fail and let a request that the handler goes on to read slip past the limiter.
type rawAccountRequest struct {
	Email        jsonText `json:"email"`
	RefreshToken jsonText `json:"refreshToken"`
	DeviceToken  jsonText `json:"deviceToken"`
	ClassCode    jsonText `json:"classCode"`
}

// jsonText is the text of a JSON string member, read the way a string field of a handler request is read. The handlers
// decode into a struct with string fields, where a member that is null changes nothing and a later member of the same name
// replaces an earlier one. A body like {"email":"a@example.com","email":null} therefore names a@example.com for the handler,
// and the limiter has to see the same account or the request would not be counted. A member that is not a string, which
// the handler refuses as a whole, reads as empty here.
type jsonText string

func (t *jsonText) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}

	var value string

	err := json.Unmarshal(data, &value)
	if err != nil {
		*t = ""

		return nil //nolint:nilerr // a member of another type is the handler's to refuse, so it only reads as empty here
	}

	*t = jsonText(value)

	return nil
}

// limiterKeyLocal names where one limiter keeps the key of a request between its skip check and the limiter, so the body
// is parsed once. Every limiter has a slot of its own, because two limiters on one route have different keys.
type limiterKeyLocal struct{ _ byte }

// accountLimiter lets limit requests per window through for the identifier that identify picks from the body, and each
// route keeps its own budget.
// The identifier is hashed so that neither an email, a refresh token nor a device id is kept in clear in the limiter storage.
// A request without an identifier is not limited, because the handler rejects it before it touches the database.
// That holds for a body that is not a JSON object and for a field that is not a string, and a field that the handler
// does read is always read here the same way.
// The route is named by the path it was registered with and not by the path the client sent, because the router ignores
// the case and a trailing slash, so each spelling of the path would otherwise get a budget of its own.
func (f *Fiber) accountLimiter(limit int, identify func(accountRequest) (string, string)) fiber.Handler {
	key := func(c fiber.Ctx) string {
		var raw rawAccountRequest

		err := json.Unmarshal(c.Body(), &raw)
		if err != nil {
			return ""
		}

		kind, identifier := identify(accountRequest{
			Email:        string(raw.Email),
			RefreshToken: string(raw.RefreshToken),
			DeviceToken:  string(raw.DeviceToken),
			ClassCode:    string(raw.ClassCode),
		})
		if identifier == "" {
			return ""
		}

		sum := sha256.Sum256([]byte(identifier))

		return kind + ":" + c.Route().Path + ":" + hex.EncodeToString(sum[:])
	}

	slot := new(limiterKeyLocal)

	keyOf := func(c fiber.Ctx) string {
		if cached, ok := c.Locals(slot).(string); ok {
			return cached
		}

		computed := key(c)
		c.Locals(slot, computed)

		return computed
	}

	return f.newLimiter(limit, keyOf, func(c fiber.Ctx) bool { return keyOf(c) == "" })
}
