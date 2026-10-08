// Package bootstrap wires configuration, infrastructure and modules into a runnable application
package bootstrap

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v3"

	authhandler "github.com/SyafaHadyan/kibooz-backend/internal/app/auth/interface/rest"
	authrepository "github.com/SyafaHadyan/kibooz-backend/internal/app/auth/repository"
	authusecase "github.com/SyafaHadyan/kibooz-backend/internal/app/auth/usecase"
	classroomhandler "github.com/SyafaHadyan/kibooz-backend/internal/app/classroom/interface/rest"
	classroomrepository "github.com/SyafaHadyan/kibooz-backend/internal/app/classroom/repository"
	classroomusecase "github.com/SyafaHadyan/kibooz-backend/internal/app/classroom/usecase"
	guruhandler "github.com/SyafaHadyan/kibooz-backend/internal/app/guru/interface/rest"
	gurure "github.com/SyafaHadyan/kibooz-backend/internal/app/guru/repository"
	guruusecase "github.com/SyafaHadyan/kibooz-backend/internal/app/guru/usecase"
	trashhandler "github.com/SyafaHadyan/kibooz-backend/internal/app/trash/interface/rest"
	trashrepository "github.com/SyafaHadyan/kibooz-backend/internal/app/trash/repository"
	trashusecase "github.com/SyafaHadyan/kibooz-backend/internal/app/trash/usecase"
	userhandler "github.com/SyafaHadyan/kibooz-backend/internal/app/user/interface/rest"
	userrepository "github.com/SyafaHadyan/kibooz-backend/internal/app/user/repository"
	userusecase "github.com/SyafaHadyan/kibooz-backend/internal/app/user/usecase"
	walihandler "github.com/SyafaHadyan/kibooz-backend/internal/app/wali/interface/rest"
	walirepository "github.com/SyafaHadyan/kibooz-backend/internal/app/wali/repository"
	waliusecase "github.com/SyafaHadyan/kibooz-backend/internal/app/wali/usecase"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/db"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	fiberapp "github.com/SyafaHadyan/kibooz-backend/internal/infra/fiber"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/jwt"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/redis"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/s3"
	"github.com/SyafaHadyan/kibooz-backend/internal/keepalive"
	"github.com/SyafaHadyan/kibooz-backend/internal/middleware"
	"gorm.io/gorm"
)

type Bootstrap struct {
	App      *fiberapp.Fiber
	Config   *env.Env
	Database *gorm.DB
	Redis    *redis.Redis

	stopKeepalive func()
}

// Start loads configuration, connects every dependency, runs migrations and registers all routes
func Start(version string) (*Bootstrap, error) {
	startTime := time.Now()

	cfg, err := env.New()
	if err != nil {
		return nil, err
	}

	database, err := db.New(cfg)
	if err != nil {
		return nil, err
	}

	sqlDB, err := database.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql handle: %w", err)
	}

	err = db.Migrate(sqlDB)
	if err != nil {
		return nil, err
	}

	cache := redis.New(cfg)

	storage, err := s3.New(cfg)
	if err != nil {
		return nil, err
	}

	if !storage.Enabled() {
		log.Println("object storage is not configured, uploads are disabled")
	}

	jwtService := jwt.New(cfg)
	app := fiberapp.New(cfg, redis.NewLimiterStorage(cache, "limiter:"))
	mw := middleware.NewMiddleware(jwtService, app.UserLimiter(middleware.UserKey))

	app.Fiber.Get("/healthz", healthHandler(sqlDB.PingContext, cache.Ping, storage.Enabled(), version))

	authhandler.NewAuthHandler(app.Router, app.EmailLimiter(), app.TokenLimiter(), authusecase.NewAuthUseCase(
		authrepository.NewAuthDB(database), jwtService, cache, cfg,
	))
	walihandler.NewWaliHandler(app.Router, mw, waliusecase.NewWaliUseCase(
		walirepository.NewWaliDB(database), cfg,
	))
	guruhandler.NewGuruHandler(app.Router, mw, guruusecase.NewGuruUseCase(
		gurure.NewGuruDB(database), cfg,
	))
	trashhandler.NewTrashHandler(app.Router, mw, trashusecase.NewTrashUseCase(
		trashrepository.NewTrashDB(database), cache, storage, cfg,
	))
	classroomhandler.NewClassroomHandler(app.Router, mw, classroomusecase.NewClassroomUseCase(
		classroomrepository.NewClassroomDB(database), storage, cfg,
	))
	userhandler.NewUserHandler(app.Router, app.PasswordLimiter(middleware.UserKey), mw, userusecase.NewUserUseCase(
		userrepository.NewUserDB(database), storage, cache,
	))

	log.Printf("startup time %v", time.Since(startTime))

	stopKeepalive := keepalive.Start(
		time.Duration(cfg.KeepaliveSeconds)*time.Second,
		keepalive.Target{Name: "database", Ping: func(ctx context.Context) error {
			_, err := sqlDB.ExecContext(ctx, "SELECT 1")

			return err
		}},
		keepalive.Target{Name: "redis", Ping: cache.Ping},
	)

	return &Bootstrap{App: app, Config: cfg, Database: database, Redis: cache, stopKeepalive: stopKeepalive}, nil
}

// Close stops the keepalive, then releases the database and Redis connections
func (b *Bootstrap) Close() {
	b.stopKeepalive()

	if sqlDB, err := b.Database.DB(); err == nil {
		_ = sqlDB.Close()
	}

	_ = b.Redis.Close()
}

type pinger func(ctx context.Context) error

// healthHandler fails only when the database is down and a Redis outage marks the service degraded.
// Storage is a configuration choice, so it is reported in the checks without changing the status.
func healthHandler(pingDB pinger, pingRedis pinger, storageEnabled bool, version string) fiber.Handler {
	return func(c fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.Context(), 2*time.Second)
		defer cancel()

		status := http.StatusOK
		state := "ok"
		checks := fiber.Map{"database": "ok", "redis": "ok", "storage": "ok"}

		if !storageEnabled {
			checks["storage"] = "disabled"
		}

		if err := pingDB(ctx); err != nil {
			status = http.StatusServiceUnavailable
			state = "down"
			checks["database"] = "down"
		}

		if err := pingRedis(ctx); err != nil {
			checks["redis"] = "down"

			if status == http.StatusOK {
				state = "degraded"
			}
		}

		return c.Status(status).JSON(fiber.Map{
			"success": status == http.StatusOK,
			"status":  state,
			"version": version,
			"checks":  checks,
		})
	}
}
