package errorreport

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/require"
)

const testDSN = "https://public@o0.ingest.example.invalid/1"

// recorder is a transport that keeps the events instead of sending them
type recorder struct {
	mu     sync.Mutex
	events []*sentry.Event
}

func (r *recorder) Flush(time.Duration) bool              { return true }
func (r *recorder) FlushWithContext(context.Context) bool { return true }
func (r *recorder) Configure(sentry.ClientOptions)        {}
func (r *recorder) Close()                                {}
func (r *recorder) SendEvent(event *sentry.Event)         { r.add(event) }

func (r *recorder) add(event *sentry.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.events = append(r.events, event)
}

func (r *recorder) all() []*sentry.Event {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]*sentry.Event(nil), r.events...)
}

// start turns the reports on with a recorder and a clock that the test moves. The reports are global, so the tests of this
// package do not run in parallel.
func start(t *testing.T, perMinute int) (*recorder, *time.Time) {
	t.Helper()

	rec := &recorder{}
	clock := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)

	stop := Start(Config{
		DSN: testDSN, Environment: "test", ServerName: "sora", Release: "1.2.3", EventsPerMinute: perMinute,
		Transport: rec, Now: func() time.Time { return clock },
	})
	t.Cleanup(stop)

	return rec, &clock
}

// textsOf joins every text of an event that could hold a secret
func textsOf(event *sentry.Event) string {
	parts := []string{event.Message, event.Transaction}

	for _, exception := range event.Exception {
		parts = append(parts, exception.Type, exception.Value)
	}

	for _, value := range event.Tags {
		parts = append(parts, value)
	}

	return strings.Join(parts, "\n")
}

func TestTheReportsAreOffWithoutADSN(t *testing.T) {
	stop := Start(Config{EventsPerMinute: 30})
	defer stop()

	require.Nil(t, active.Load())

	// nothing is sent and nothing panics
	Report(errors.New("boom"), Info{Status: 500})
	Panic("boom", Info{})
}

func TestADSNThatCannotBeReadLeavesTheReportsOff(t *testing.T) {
	stop := Start(Config{DSN: "this is not a dsn", EventsPerMinute: 30})
	defer stop()

	require.Nil(t, active.Load(), "a bad setting must not keep the API from starting")
}

func TestAReportCarriesTheNamesAndIdsAndNothingOfTheRequest(t *testing.T) {
	rec, _ := start(t, 30)

	Report(errors.New("insert failed"), Info{
		Route: "/api/v1/classes/:classId/forum", Status: 500, RequestID: "0b8f6a52-3c4d-4e5f-8a6b-7c8d9e0f1a2b",
		UserID: "7b1d2c3e-4f56-4a78-9b01-2c3d4e5f6a7b",
	})

	events := rec.all()
	require.Len(t, events, 1)

	event := events[0]
	require.Equal(t, "1.2.3", event.Release)
	require.Equal(t, "test", event.Environment)
	require.Equal(t, "sora", event.ServerName)
	require.Equal(t, "/api/v1/classes/:classId/forum", event.Tags["route"])
	require.Equal(t, "500", event.Tags["status"])
	require.Equal(t, "0b8f6a52-3c4d-4e5f-8a6b-7c8d9e0f1a2b", event.Tags["request_id"], "the request id is not scrubbed, it is how the log line is found")
	require.Equal(t, sentry.User{ID: "7b1d2c3e-4f56-4a78-9b01-2c3d4e5f6a7b"}, event.User)
	require.Nil(t, event.Request)
	require.Empty(t, event.Breadcrumbs)
	require.NotEmpty(t, event.Exception)
}

func TestAReportHidesEmailsTokensAndLongNumbers(t *testing.T) {
	rec, _ := start(t, 30)

	secrets := []string{
		"ana.putri@example.com",
		// built here so that no token stays in the source, the secret scan reads the files and the history
		strings.Join([]string{"eyJhbGciOiJIUzI1NiJ9", "eyJzdWIiOiIxMjM0In0", "c2lnbmF0dXJl"}, "."),
		"abcDEF123456abcDEF123456abcDEF123456",
		"1234567890123",
	}

	Report(
		errors.New("ERROR: duplicate key value violates unique constraint, Key (email)=("+secrets[0]+"), Bearer "+secrets[1]+
			", refresh "+secrets[2]+", nisn "+secrets[3]),
		Info{Route: "/api/v1/auth/register", Status: 500},
	)

	events := rec.all()
	require.Len(t, events, 1)

	text := textsOf(events[0])

	for _, secret := range secrets {
		require.NotContains(t, text, secret)
	}

	require.Contains(t, text, "[email]")
	require.Contains(t, text, "[token]")
	require.Contains(t, text, "[secret]")
	require.Contains(t, text, "[number]")
	require.Contains(t, text, "duplicate key value violates unique constraint", "the rest of the text stays so the error can be read")
}

func TestAPanicIsReportedWithTheSamePrivacy(t *testing.T) {
	rec, _ := start(t, 30)

	Panic("nil pointer for ana.putri@example.com", Info{Route: "/api/v1/guru/mood/log", Status: 500, RequestID: "req-1"})

	events := rec.all()
	require.Len(t, events, 1)

	text := textsOf(events[0])
	require.NotContains(t, text, "ana.putri@example.com")
	require.True(t, len(events[0].Threads) > 0 || (len(events[0].Exception) > 0 && events[0].Exception[0].Stacktrace != nil), "a panic report holds the stack")
	require.Equal(t, "true", events[0].Tags["panic"])
	require.Equal(t, "/api/v1/guru/mood/log", events[0].Tags["route"])
	require.Contains(t, text+events[0].Message, "[email]")
}

func TestTheCapDropsTheEventsOverTheLimitAndStartsAgainEachMinute(t *testing.T) {
	rec, clock := start(t, 3)

	for range 5 {
		Report(errors.New("again"), Info{Status: 500})
	}

	require.Len(t, rec.all(), 3, "two of the five are over the cap of three a minute")

	*clock = clock.Add(30 * time.Second)

	Report(errors.New("still the same minute"), Info{Status: 500})
	require.Len(t, rec.all(), 3)

	*clock = clock.Add(31 * time.Second)

	Report(errors.New("a new minute"), Info{Status: 500})
	Report(errors.New("a new minute"), Info{Status: 500})
	require.Len(t, rec.all(), 5)
}

func TestScrubTextCasesOneByOne(t *testing.T) {
	tests := map[string]struct{ in, want string }{
		"an email":            {"a.b+c@mail.example.co.id failed", "[email] failed"},
		"a jwt":               {"token " + strings.Join([]string{"eyJhbGciOiJIUzI1NiJ9", "eyJzdWIiOiIxIn0", "sig"}, ".") + " end", "token [token] end"},
		"a bearer header":     {"Authorization: Bearer abc.DEF-123_x end", "Authorization: Bearer [token] end"},
		"a long key":          {"key 0123456789abcdef0123456789abcdef0123456789abcdef end", "key [secret] end"},
		"a nisn":              {"nisn 123456789012", "nisn [number]"},
		"a short number":      {"status 500 after 1234 ms", "status 500 after 1234 ms"},
		"a uuid stays":        {"row 0b8f6a52-3c4d-4e5f-8a6b-7c8d9e0f1a2b", "row 0b8f6a52-3c4d-4e5f-8a6b-7c8d9e0f1a2b"},
		"a text without data": {"context deadline exceeded", "context deadline exceeded"},
	}

	for name, test := range tests {
		require.Equal(t, test.want, scrubText(test.in), name)
	}
}

func TestScrubEventDropsTheRequestTheBreadcrumbsAndTheContactsOfTheUser(t *testing.T) {
	event := &sentry.Event{
		Message:     "failed for ana@example.com",
		Request:     &sentry.Request{URL: "https://kibooz-api.syafahadyan.com/api/v1/x?token=abc", Cookies: "session=abc"},
		Breadcrumbs: []*sentry.Breadcrumb{{Message: "log line"}},
		User:        sentry.User{ID: "u1", Email: "ana@example.com", IPAddress: "10.0.0.1", Username: "ana"},
		Tags:        map[string]string{"route": "/x", "other": "ana@example.com"},
	}

	scrubEvent(event)

	require.Nil(t, event.Request)
	require.Empty(t, event.Breadcrumbs)
	require.Equal(t, sentry.User{ID: "u1"}, event.User)
	require.Equal(t, "failed for [email]", event.Message)
	require.Equal(t, "/x", event.Tags["route"])
	require.Equal(t, "[email]", event.Tags["other"])
}
