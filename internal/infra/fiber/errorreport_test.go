package fiber_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	gofiber "github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/errorreport"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/fiber"
)

// reportRecorder keeps the events that the reports would send
type reportRecorder struct {
	mu     sync.Mutex
	events []*sentry.Event
}

func (r *reportRecorder) Flush(time.Duration) bool              { return true }
func (r *reportRecorder) FlushWithContext(context.Context) bool { return true }
func (r *reportRecorder) Configure(sentry.ClientOptions)        {}
func (r *reportRecorder) Close()                                {}

func (r *reportRecorder) SendEvent(event *sentry.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.events = append(r.events, event)
}

func (r *reportRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.events)
}

func (r *reportRecorder) last() *sentry.Event {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.events[len(r.events)-1]
}

func reportingServer(t *testing.T) (*gofiber.App, *reportRecorder) {
	t.Helper()

	rec := &reportRecorder{}

	stop := errorreport.Start(errorreport.Config{
		DSN: "https://public@o0.ingest.example.invalid/1", Environment: "test", EventsPerMinute: 100, Release: "test", Transport: rec,
	})
	t.Cleanup(stop)

	server := fiber.New(&env.Env{UserLimiterMax: 100, LimiterExpirationSeconds: 60, AuthLimiterMax: 10, BodyLimitMB: 1}, nil)

	server.Fiber.Get("/internal/:id", func(gofiber.Ctx) error {
		return apperror.Internal(errors.New("insert failed for ana@example.com"))
	})
	server.Fiber.Get("/plain", func(gofiber.Ctx) error { return errors.New("not an apperror, 1234567890") })
	server.Fiber.Get("/panic", func(gofiber.Ctx) error { panic("handler broke") })
	server.Fiber.Get("/missing", func(gofiber.Ctx) error { return apperror.ErrChatConversationNotFound })
	server.Fiber.Get("/storage", func(gofiber.Ctx) error { return apperror.ErrStorageDisabled })
	server.Fiber.Get("/gateway", func(gofiber.Ctx) error { return apperror.ErrStorageFailed })

	return server.Fiber, rec
}

func hit(t *testing.T, app *gofiber.App, path string) int {
	t.Helper()

	res, err := app.Test(httptest.NewRequest(http.MethodGet, path, nil), gofiber.TestConfig{Timeout: 10 * time.Second})
	require.NoError(t, err)

	defer res.Body.Close()

	return res.StatusCode
}

func TestAnUnexpectedFailureIsReportedWithItsRouteAndNothingOfTheRequest(t *testing.T) {
	app, rec := reportingServer(t)

	require.Equal(t, http.StatusInternalServerError, hit(t, app, "/internal/ana@example.com?token=secret"))
	require.Equal(t, 1, rec.count())

	event := rec.last()
	require.Equal(t, "/internal/:id", event.Tags["route"], "the pattern of the route and not the address that was called")
	require.Equal(t, "500", event.Tags["status"])
	require.NotEmpty(t, event.Tags["request_id"])
	require.Nil(t, event.Request)

	for _, exception := range event.Exception {
		require.NotContains(t, exception.Value, "ana@example.com")
	}
}

func TestAnErrorThatIsNotAnAppErrorIsReported(t *testing.T) {
	app, rec := reportingServer(t)

	require.Equal(t, http.StatusInternalServerError, hit(t, app, "/plain"))
	require.Equal(t, 1, rec.count())

	for _, exception := range rec.last().Exception {
		require.NotContains(t, exception.Value, "1234567890")
	}
}

func TestAPanicIsReported(t *testing.T) {
	app, rec := reportingServer(t)

	require.Equal(t, http.StatusInternalServerError, hit(t, app, "/panic"))
	require.Equal(t, 1, rec.count())
	require.Equal(t, "true", rec.last().Tags["panic"])
	require.Equal(t, "/panic", rec.last().Tags["route"])
}

func TestAnAnswerThatTheAPIChoseIsNotReported(t *testing.T) {
	app, rec := reportingServer(t)

	require.Equal(t, http.StatusNotFound, hit(t, app, "/missing"))
	require.Equal(t, http.StatusServiceUnavailable, hit(t, app, "/storage"))
	require.Zero(t, rec.count(), "a 404 is part of the API and a 503 says that the storage is not set up")

	require.Equal(t, http.StatusBadGateway, hit(t, app, "/gateway"))
	require.Equal(t, 1, rec.count(), "a failure of the storage is a failure of the server")
}
