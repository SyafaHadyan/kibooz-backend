package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/bootstrap"
)

var mailedCode = regexp.MustCompile(`adalah (\d{6})\.`)

// resendStub stands in for the Resend API, keeps the codes it was asked to mail and can be told to fail
type resendStub struct {
	mu    sync.Mutex
	codes map[string][]string
	auth  []string
	fail  bool
}

func newResendStub(t *testing.T) (*resendStub, string) {
	t.Helper()

	stub := &resendStub{codes: map[string][]string{}}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)

		var sent struct {
			To   []string `json:"to"`
			Text string   `json:"text"`
		}

		_ = json.Unmarshal(raw, &sent)

		stub.mu.Lock()
		defer stub.mu.Unlock()

		stub.auth = append(stub.auth, r.Header.Get("Authorization"))

		if stub.fail {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		if match := mailedCode.FindStringSubmatch(sent.Text); match != nil && len(sent.To) == 1 {
			stub.codes[sent.To[0]] = append(stub.codes[sent.To[0]], match[1])
		}

		_, _ = w.Write([]byte(`{"id":"stub"}`))
	}))

	t.Cleanup(server.Close)

	return stub, server.URL
}

func (s *resendStub) setFail(fail bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.fail = fail
}

func (s *resendStub) count(email string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.codes[email])
}

func (s *resendStub) last(t *testing.T, email string) string {
	t.Helper()

	s.mu.Lock()
	defer s.mu.Unlock()

	require.NotEmpty(t, s.codes[email], "no code was mailed to %s", email)

	return s.codes[email][len(s.codes[email])-1]
}

// startVerifying starts an API that requires the code of the mail, with the mail sent to the stub
func startVerifying(t *testing.T) (*fiber.App, *resendStub) {
	t.Helper()

	app(t) // makes sure the shared environment defaults are set

	stub, url := newResendStub(t)

	t.Setenv("EMAIL_VERIFICATION_REQUIRED", "true")
	t.Setenv("RESEND_API_KEY", "re_e2e_stub_key")
	t.Setenv("RESEND_API_URL", url)
	// one test sends many calls for one email, and the budget of the limiter is not what it looks at
	t.Setenv("AUTH_LIMITER_MAX", "1000")
	t.Setenv("REDIS_KEY_PREFIX", "e2e-verify-"+suffix()+":")

	started, err := bootstrap.Start("e2e-verification")
	require.NoError(t, err)

	t.Cleanup(started.Close)

	return started.App.Fiber, stub
}

func registerWaitingGuru(t *testing.T, target *fiber.App) (string, result) {
	t.Helper()

	email := fmt.Sprintf("guru.%s@example.com", suffix())

	res := callApp(t, target, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email": email, "password": testPassword, "fullName": "Waiting Teacher", "role": "GURU", "class": map[string]any{"name": "Waiting"},
	})
	require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

	return email, res
}

func login(t *testing.T, target *fiber.App, email string, password string) result {
	t.Helper()

	return callApp(t, target, http.MethodPost, "/api/v1/auth/login", "", map[string]any{"email": email, "password": password, "role": "GURU"})
}

func verify(t *testing.T, target *fiber.App, email string, code string) result {
	t.Helper()

	return callApp(t, target, http.MethodPost, "/api/v1/auth/verify-email", "", map[string]any{"email": email, "code": code})
}

func resend(t *testing.T, target *fiber.App, email string) result {
	t.Helper()

	return callApp(t, target, http.MethodPost, "/api/v1/auth/resend-verification", "", map[string]any{"email": email, "password": testPassword})
}

// ageTheCode moves the last send of an account into the past, which is how a test lets the cooldown pass without waiting for it
func ageTheCode(t *testing.T, email string, extra string) {
	t.Helper()

	require.NoError(t, testDB.Exec(
		"UPDATE email_verifications SET last_sent_at = last_sent_at - interval '2 minutes'"+extra+
			" WHERE user_id = (SELECT id FROM users WHERE email = ?)", email,
	).Error)
}

func TestAnAccountWaitsForItsCodeAndTheFirstSessionComesWithIt(t *testing.T) {
	target, stub := startVerifying(t)
	email, registered := registerWaitingGuru(t, target)

	t.Run("the registration returns no session and says how the code went", func(t *testing.T) {
		require.Equal(t, true, registered.data("verificationRequired"))
		require.Empty(t, registered.data("token"))
		require.Empty(t, registered.data("refreshToken"))
		require.Equal(t, true, registered.data("verification", "sent"))
		require.Equal(t, email, registered.data("user", "email"))
		require.Equal(t, 1, stub.count(email))
		require.Equal(t, []string{"Bearer re_e2e_stub_key"}, stub.auth, "the key goes in the header of the call")
	})

	t.Run("the database holds a hash of the code and no session", func(t *testing.T) {
		var pending bool

		require.NoError(t, testDB.Raw("SELECT email_verification_pending FROM users WHERE email = ?", email).Scan(&pending).Error)
		require.True(t, pending)

		var sessions int64

		require.NoError(t, testDB.Raw(
			"SELECT count(*) FROM refresh_tokens WHERE user_id = (SELECT id FROM users WHERE email = ?)", email,
		).Scan(&sessions).Error)
		require.Zero(t, sessions)

		var stored string

		require.NoError(t, testDB.Raw(
			"SELECT code_hash FROM email_verifications WHERE user_id = (SELECT id FROM users WHERE email = ?)", email,
		).Scan(&stored).Error)
		require.Len(t, stored, 64)
		require.NotContains(t, stored, stub.last(t, email))
	})

	t.Run("a sign in is refused only after the password matched", func(t *testing.T) {
		login(t, target, email, testPassword).requireError(t, http.StatusForbidden, "EMAIL_NOT_VERIFIED")
		login(t, target, email, testPassword+"x").requireError(t, http.StatusUnauthorized, "AUTH_INVALID_CREDENTIALS")
	})

	t.Run("a wrong code counts an attempt and the right one opens the session", func(t *testing.T) {
		code := stub.last(t, email)
		wrong := "000000"

		if code == wrong {
			wrong = "000001"
		}

		verify(t, target, email, wrong).requireError(t, http.StatusBadRequest, "VERIFICATION_CODE_INVALID")

		var attempts int

		require.NoError(t, testDB.Raw(
			"SELECT attempts FROM email_verifications WHERE user_id = (SELECT id FROM users WHERE email = ?)", email,
		).Scan(&attempts).Error)
		require.Equal(t, 1, attempts)

		verified := verify(t, target, email, code)
		require.Equal(t, http.StatusOK, verified.Status, "body %v", verified.Body)

		token, _ := verified.data("token").(string)
		require.NotEmpty(t, token)
		require.NotEmpty(t, verified.data("refreshToken"))

		require.Equal(t, http.StatusOK, callApp(t, target, http.MethodGet, "/api/v1/notifications", token, nil).Status)
	})

	t.Run("the code works once and the account signs in as any other", func(t *testing.T) {
		verify(t, target, email, stub.last(t, email)).requireError(t, http.StatusBadRequest, "VERIFICATION_CODE_INVALID")

		var rows int64

		require.NoError(t, testDB.Raw(
			"SELECT count(*) FROM email_verifications WHERE user_id = (SELECT id FROM users WHERE email = ?)", email,
		).Scan(&rows).Error)
		require.Zero(t, rows, "the code is gone once it was used")

		signedIn := login(t, target, email, testPassword)
		require.Equal(t, http.StatusOK, signedIn.Status, "body %v", signedIn.Body)
		require.NotEmpty(t, signedIn.data("token"))

		resend(t, target, email).requireError(t, http.StatusConflict, "EMAIL_ALREADY_VERIFIED")
	})

	t.Run("an email that no account waits on gets the same answer as a wrong code", func(t *testing.T) {
		verify(t, target, "nobody."+suffix()+"@example.com", "123456").requireError(t, http.StatusBadRequest, "VERIFICATION_CODE_INVALID")
	})

	t.Run("a code that is not six digits is a validation error", func(t *testing.T) {
		for _, code := range []string{"12345", "1234567", "12345a", ""} {
			verify(t, target, email, code).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		}
	})
}

func TestACodeIsLimitedByCooldownAttemptsAgeAndTheHour(t *testing.T) {
	target, stub := startVerifying(t)
	email, _ := registerWaitingGuru(t, target)

	t.Run("a second code right away is refused and nothing is mailed", func(t *testing.T) {
		resend(t, target, email).requireError(t, http.StatusTooManyRequests, "VERIFICATION_RESEND_TOO_SOON")
		require.Equal(t, 1, stub.count(email))
	})

	t.Run("a wrong password mails nobody", func(t *testing.T) {
		wrong := callApp(t, target, http.MethodPost, "/api/v1/auth/resend-verification", "", map[string]any{
			"email": email, "password": testPassword + "x",
		})
		wrong.requireError(t, http.StatusUnauthorized, "AUTH_INVALID_CREDENTIALS")
		require.Equal(t, 1, stub.count(email))
	})

	t.Run("a code that took all its attempts is refused until a new one is sent", func(t *testing.T) {
		code := stub.last(t, email)
		wrong := "000000"

		if code == wrong {
			wrong = "000001"
		}

		for range 5 {
			verify(t, target, email, wrong).requireError(t, http.StatusBadRequest, "VERIFICATION_CODE_INVALID")
		}

		verify(t, target, email, code).requireError(t, http.StatusBadRequest, "VERIFICATION_CODE_INVALID")

		ageTheCode(t, email, "")

		sent := resend(t, target, email)
		require.Equal(t, http.StatusOK, sent.Status, "body %v", sent.Body)
		require.Equal(t, 2, stub.count(email))

		var attempts int

		require.NoError(t, testDB.Raw(
			"SELECT attempts FROM email_verifications WHERE user_id = (SELECT id FROM users WHERE email = ?)", email,
		).Scan(&attempts).Error)
		require.Zero(t, attempts, "a new code starts with its own attempts")
	})

	t.Run("a code that is older than its time is refused", func(t *testing.T) {
		require.NoError(t, testDB.Exec(
			"UPDATE email_verifications SET expires_at = now() - interval '1 minute' WHERE user_id = (SELECT id FROM users WHERE email = ?)", email,
		).Error)

		verify(t, target, email, stub.last(t, email)).requireError(t, http.StatusBadRequest, "VERIFICATION_CODE_INVALID")
	})

	t.Run("the codes of an hour are capped and the window opens again", func(t *testing.T) {
		require.NoError(t, testDB.Exec(
			"UPDATE email_verifications SET sends_in_window = 5, window_started_at = now() WHERE user_id = (SELECT id FROM users WHERE email = ?)", email,
		).Error)
		ageTheCode(t, email, "")

		resend(t, target, email).requireError(t, http.StatusTooManyRequests, "VERIFICATION_RESEND_TOO_SOON")

		require.NoError(t, testDB.Exec(
			"UPDATE email_verifications SET window_started_at = now() - interval '2 hours' WHERE user_id = (SELECT id FROM users WHERE email = ?)", email,
		).Error)

		fresh := resend(t, target, email)
		require.Equal(t, http.StatusOK, fresh.Status, "body %v", fresh.Body)

		verified := verify(t, target, email, stub.last(t, email))
		require.Equal(t, http.StatusOK, verified.Status, "body %v", verified.Body)
	})
}

func TestARegistrationSurvivesAMailServiceThatFails(t *testing.T) {
	target, stub := startVerifying(t)

	stub.setFail(true)

	email, registered := registerWaitingGuru(t, target)
	require.Equal(t, true, registered.data("verificationRequired"))
	require.Equal(t, false, registered.data("verification", "sent"), "the answer says the mail did not go out")
	require.Zero(t, stub.count(email))

	t.Run("a new code cannot be sent while the service is down, and the answer says so", func(t *testing.T) {
		ageTheCode(t, email, "")
		resend(t, target, email).requireError(t, http.StatusServiceUnavailable, "EMAIL_SEND_FAILED")
	})

	t.Run("it can once the service is back", func(t *testing.T) {
		stub.setFail(false)
		ageTheCode(t, email, "")

		sent := resend(t, target, email)
		require.Equal(t, http.StatusOK, sent.Status, "body %v", sent.Body)

		verified := verify(t, target, email, stub.last(t, email))
		require.Equal(t, http.StatusOK, verified.Status, "body %v", verified.Body)
	})
}

func TestAnAccountThatWaitsSignsInWhenTheSettingIsOff(t *testing.T) {
	target, stub := startVerifying(t)
	email, _ := registerWaitingGuru(t, target)
	require.Equal(t, 1, stub.count(email))

	t.Setenv("EMAIL_VERIFICATION_REQUIRED", "false")

	started, err := bootstrap.Start("e2e-verification-off")
	require.NoError(t, err)

	t.Cleanup(started.Close)

	off := started.App.Fiber

	signedIn := login(t, off, email, testPassword)
	require.Equal(t, http.StatusOK, signedIn.Status, "body %v", signedIn.Body)

	other := callApp(t, off, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email": fmt.Sprintf("guru.%s@example.com", suffix()), "password": testPassword, "fullName": "Plain Teacher", "role": "GURU",
		"class": map[string]any{"name": "Plain"},
	})
	require.Equal(t, http.StatusCreated, other.Status, "body %v", other.Body)
	require.NotEmpty(t, other.data("token"), "with the setting off a registration opens its session as before")
	require.Nil(t, other.data("verificationRequired"))
}
