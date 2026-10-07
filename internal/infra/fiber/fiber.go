// Package fiber builds the HTTP server with its shared middleware and error handling
package fiber

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/helmet"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/requestid"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/response"
)

type Fiber struct {
	Fiber  *fiber.App
	Router fiber.Router

	// the rate limiters in limits.go are built from these
	storage fiber.Storage
	window  time.Duration
	userMax int
	authMax int
}

func New(cfg *env.Env, limiterStorage fiber.Storage) *Fiber {
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

	app := fiber.New(config)

	app.Use(
		recover.New(),
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
	)

	return &Fiber{
		Fiber:   app,
		Router:  app.Group("/api/v1"),
		storage: limiterStorage,
		window:  time.Duration(cfg.LimiterExpirationSeconds) * time.Second,
		userMax: cfg.UserLimiterMax,
		authMax: cfg.AuthLimiterMax,
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
			return response.Error(c, apperror.ErrFileTooLarge)
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
