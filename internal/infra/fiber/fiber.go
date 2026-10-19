// Package fiber builds the HTTP server with its shared middleware and error handling
package fiber

import (
	"context"
	"errors"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/helmet"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/requestid"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/devicetoken"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/ratelimit"
	"github.com/SyafaHadyan/kibooz-backend/internal/response"
)

type Fiber struct {
	Fiber  *fiber.App
	Router fiber.Router

	// the rate limiters in limits.go are built from these
	limits  ratelimit.Store
	window  time.Duration
	userMax int
	authMax int
	// classCodeMax is the allowance of each route for requests that name a class code, see ClassCodeLimiter
	classCodeMax int
	devices      *devicetoken.Tokens
}

// New builds the server. limits counts the requests for the rate limiters, and a nil store counts in the memory of the process.
func New(cfg *env.Env, limits ratelimit.Store) *Fiber {
	config := fiber.Config{
		AppName:      "kibooz-backend",
		BodyLimit:    cfg.BodyLimitMB * 1024 * 1024,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
		ErrorHandler: errorHandler,
	}

	if cfg.TrustProxy {
		config.TrustProxy = true
		config.ProxyHeader = cfg.ProxyHeader
		config.TrustProxyConfig = fiber.TrustProxyConfig{
			Loopback:  true,
			LinkLocal: true,
			Private:   true,
		}
	}

	if limits == nil {
		limits = ratelimit.NewMemory()
	}

	app := fiber.New(config)

	app.Use(
		recover.New(),
		deadline(time.Duration(cfg.RequestTimeoutSeconds)*time.Second),
		// the API only answers with JSON, so nothing may be framed or loaded from the response. HSTS stays off here
		// because includeSubDomains would reach every subdomain of the host, so the proxy that ends TLS sets it.
		helmet.New(helmet.Config{
			XFrameOptions:         "DENY",
			ContentSecurityPolicy: "default-src 'none'; frame-ancestors 'none'",
		}),
		requestid.New(),
		logger.New(logger.Config{
			Format: "${time} ${ip} ${method} ${path} ${status} ${latency} ${locals:requestid}\n",
		}),
		// after the headers, the request id and the log, so a refused request has all three
		plainBody(),
		smallBody(constants.JSONBodyMaxBytes),
	)

	return &Fiber{
		Fiber:   app,
		Router:  app.Group("/api/v1"),
		limits:  limits,
		window:  time.Duration(cfg.LimiterExpirationSeconds) * time.Second,
		userMax: cfg.UserLimiterMax,
		authMax: cfg.AuthLimiterMax,

		classCodeMax: cfg.ClassCodeLimiterMax,
		devices:      devicetoken.New(cfg),
	}
}

// plainBody refuses a request that says its body is encoded. Fiber decodes a body by its Content-Encoding when a handler
// reads it, and the size limit only counts the bytes that were sent, so a few kilobytes of zstd or gzip could grow to the
// memory of the decoder. No client of this API compresses what it sends, so the encoding is never needed.
func plainBody() fiber.Handler {
	return func(c fiber.Ctx) error {
		// a request can repeat the header, and Fiber joins the lines when it decodes the body
		for _, line := range c.Request().Header.PeekAll(fiber.HeaderContentEncoding) {
			encoding := strings.TrimSpace(string(line))
			if encoding != "" && !strings.EqualFold(encoding, "identity") {
				return fiber.ErrUnsupportedMediaType
			}
		}

		return c.Next()
	}
}

// uploadPaths are the two routes that take a photo and have the size limits of a photo
var uploadPaths = []string{"/api/v1/users/avatar", "/api/v1/trash/scan-claim"}

// isUpload tells whether a request path leads to one of the photo uploads. Fiber routes without regard to case and to a
// trailing slash, so the path is cleaned the same way before it is compared.
func isUpload(path string) bool {
	return slices.Contains(uploadPaths, strings.ToLower(strings.TrimRight(path, "/")))
}

// smallBody refuses a request whose body is larger than max, except on the paths that take a photo. Fiber has read the body
// by now, up to BodyLimit, so this does not save the memory of that one request. It keeps a huge body from reaching a JSON
// decoder, a validator and the database, and gives the client the same answer on every route.
func smallBody(max int) fiber.Handler {
	return func(c fiber.Ctx) error {
		if len(c.Body()) <= max || isUpload(c.Path()) {
			return c.Next()
		}

		return apperror.ErrBodyTooLarge
	}
}

// deadline gives the context that every handler passes on to the database, the cache and the storage a time limit. Fiber
// hands out a context that never ends, so without it a stalled query or storage call holds its pooled connection for as long
// as it lasts. A timeout that is not positive leaves the context as it is.
func deadline(timeout time.Duration) fiber.Handler {
	return func(c fiber.Ctx) error {
		if timeout <= 0 {
			return c.Next()
		}

		ctx, cancel := context.WithTimeout(c.Context(), timeout)
		defer cancel()

		c.SetContext(ctx)

		return c.Next()
	}
}

func errorHandler(c fiber.Ctx, err error) error {
	var appErr *apperror.Error
	if errors.As(err, &appErr) {
		if appErr.Status >= http.StatusInternalServerError || appErr.Err != nil {
			log.Printf("request %v failed %s %s", c.Locals("requestid"), c.Method(), appErr.Error())
		}

		return response.Error(c, appErr)
	}

	var fiberErr *fiber.Error
	if errors.As(err, &fiberErr) {
		switch fiberErr.Code {
		case http.StatusNotFound:
			return response.Error(c, apperror.ErrNotFound)
		case http.StatusRequestEntityTooLarge:
			// Fiber refuses a body over BODY_LIMIT_MB before any handler runs, and only a photo can be that large
			if isUpload(c.Path()) {
				return response.Error(c, apperror.ErrFileTooLarge)
			}

			return response.Error(c, apperror.ErrBodyTooLarge)
		case http.StatusMethodNotAllowed:
			return response.Error(c, apperror.New(fiberErr.Code, "METHOD_NOT_ALLOWED", "HTTP method not allowed"))
		default:
			if fiberErr.Code < http.StatusInternalServerError {
				return response.Error(c, apperror.New(fiberErr.Code, "BAD_REQUEST", "The request could not be processed"))
			}
		}
	}

	log.Printf("request %v unexpected error %s %s %v", c.Locals("requestid"), c.Method(), c.Path(), err)

	return response.Error(c, apperror.Internal(err))
}
