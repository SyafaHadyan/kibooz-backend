package fiber

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/requestid"

	"github.com/SyafaHadyan/kibooz-backend/internal/errorreport"
	"github.com/SyafaHadyan/kibooz-backend/internal/response"
)

// healthPath is the route that a monitor asks every few seconds, so an answer of 200 is logged below the default level
const healthPath = "/healthz"

// accessLog writes one JSON line for every request, after it was answered. A failed request says why in the same line, with the
// code and the message that the client got and the names of the fields that a validation refused. The values of those
// fields are left out, because a rejected password or email is still a password or an email. A request that panicked is
// logged as a 500, because the recover middleware that answers it sits outside this one.
func accessLog() fiber.Handler {
	return func(c fiber.Ctx) error {
		start := time.Now()
		finished := false

		defer func() {
			if !finished {
				writeAccessLog(c, http.StatusInternalServerError, start)
			}
		}()

		err := c.Next()
		if err != nil {
			if handled := c.App().ErrorHandler(c, err); handled != nil {
				_ = c.SendStatus(http.StatusInternalServerError)
			}
		}

		finished = true

		writeAccessLog(c, c.Response().StatusCode(), start)

		return nil
	}
}

func writeAccessLog(c fiber.Ctx, status int, start time.Time) {
	level := slog.LevelInfo

	switch {
	case status >= http.StatusInternalServerError:
		level = slog.LevelError
	case status >= http.StatusBadRequest:
		level = slog.LevelWarn
	case c.Path() == healthPath:
		level = slog.LevelDebug
	}

	attrs := []slog.Attr{
		slog.String("request_id", requestid.FromContext(c)),
		slog.String("ip", c.IP()),
		slog.String("method", c.Method()),
		slog.String("path", c.Path()),
		slog.Int("status", status),
		slog.Float64("latency_ms", float64(time.Since(start).Microseconds())/1000),
		slog.Int("bytes_out", len(c.Response().Body())),
	}

	if route := c.Route(); route != nil {
		attrs = append(attrs, slog.String("route", route.Path))
	}

	if failure, ok := response.FailureOf(c); ok && status >= http.StatusBadRequest {
		attrs = append(attrs, slog.String("error_code", failure.Code), slog.String("error_message", failure.Message))

		if len(failure.Fields) > 0 {
			attrs = append(attrs, slog.String("error_fields", strings.Join(failure.Fields, ",")))
		}
	}

	if retry := c.GetRespHeader(fiber.HeaderRetryAfter); retry != "" && status == http.StatusTooManyRequests {
		attrs = append(attrs, slog.String("retry_after", retry))
	}

	slog.LogAttrs(context.Background(), level, "request", attrs...)
}

// logPanic writes the stack of a handler that panicked, which the recover middleware would otherwise swallow
func logPanic(c fiber.Ctx, recovered any) {
	slog.Error("panic recovered",
		slog.String("request_id", requestid.FromContext(c)),
		slog.String("method", c.Method()),
		slog.String("path", c.Path()),
		slog.String("panic", fmt.Sprint(recovered)),
		slog.String("stack", string(debug.Stack())),
	)

	errorreport.Panic(recovered, infoOf(c, http.StatusInternalServerError))
	c.Locals(reportedKey{}, true)
}
