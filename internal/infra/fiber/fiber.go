// Package fiber builds the HTTP server with its shared middleware and error handling
package fiber

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/requestid"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/response"
)

type Fiber struct {
	Fiber       *fiber.App
	Router      fiber.Router
	AuthLimiter fiber.Handler
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

	expiration := time.Duration(cfg.LimiterExpirationSeconds) * time.Second

	app.Use(
		recover.New(),
		requestid.New(),
		logger.New(logger.Config{
			Format: "${time} ${ip} ${method} ${path} ${status} ${latency} ${locals:requestid}\n",
		}),
		limiter.New(limiter.Config{
			Max:               cfg.LimiterMax,
			Expiration:        expiration,
			Storage:           limiterStorage,
			LimiterMiddleware: limiter.SlidingWindow{},
			LimitReached: func(fiber.Ctx) error {
				return apperror.ErrRateLimited
			},
		}),
	)

	authLimiter := limiter.New(limiter.Config{
		Max:        cfg.AuthLimiterMax,
		Expiration: expiration,
		Storage:    limiterStorage,
		KeyGenerator: func(c fiber.Ctx) string {
			return "auth:" + c.IP()
		},
		LimiterMiddleware: limiter.SlidingWindow{},
		LimitReached: func(fiber.Ctx) error {
			return apperror.ErrRateLimited
		},
	})

	return &Fiber{
		Fiber:       app,
		Router:      app.Group("/api/v1"),
		AuthLimiter: authLimiter,
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
			return response.Error(c, apperror.New(fiberErr.Code, "METHOD_NOT_ALLOWED", "Metode HTTP tidak diizinkan"))
		default:
			if fiberErr.Code < http.StatusInternalServerError {
				return response.Error(c, apperror.New(fiberErr.Code, "BAD_REQUEST", "Permintaan tidak dapat diproses"))
			}
		}
	}

	log.Printf("request %v unexpected error %s %s %v", c.Locals("requestid"), c.Method(), c.Path(), err)

	return response.Error(c, apperror.Internal(err))
}
