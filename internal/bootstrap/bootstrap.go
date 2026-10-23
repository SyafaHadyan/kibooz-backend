// Package bootstrap wires configuration, infrastructure and modules into a runnable application
package bootstrap

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"

	authhandler "github.com/SyafaHadyan/kibooz-backend/internal/app/auth/interface/rest"
	authrepository "github.com/SyafaHadyan/kibooz-backend/internal/app/auth/repository"
	authusecase "github.com/SyafaHadyan/kibooz-backend/internal/app/auth/usecase"
	chathandler "github.com/SyafaHadyan/kibooz-backend/internal/app/chat/interface/rest"
	chatrepository "github.com/SyafaHadyan/kibooz-backend/internal/app/chat/repository"
	chatusecase "github.com/SyafaHadyan/kibooz-backend/internal/app/chat/usecase"
	classroomhandler "github.com/SyafaHadyan/kibooz-backend/internal/app/classroom/interface/rest"
	classroomrepository "github.com/SyafaHadyan/kibooz-backend/internal/app/classroom/repository"
	classroomusecase "github.com/SyafaHadyan/kibooz-backend/internal/app/classroom/usecase"
	guruhandler "github.com/SyafaHadyan/kibooz-backend/internal/app/guru/interface/rest"
	gurure "github.com/SyafaHadyan/kibooz-backend/internal/app/guru/repository"
	guruusecase "github.com/SyafaHadyan/kibooz-backend/internal/app/guru/usecase"
	pushhandler "github.com/SyafaHadyan/kibooz-backend/internal/app/push/interface/rest"
	pushrepository "github.com/SyafaHadyan/kibooz-backend/internal/app/push/repository"
	pushusecase "github.com/SyafaHadyan/kibooz-backend/internal/app/push/usecase"
	trashhandler "github.com/SyafaHadyan/kibooz-backend/internal/app/trash/interface/rest"
	trashrepository "github.com/SyafaHadyan/kibooz-backend/internal/app/trash/repository"
	trashusecase "github.com/SyafaHadyan/kibooz-backend/internal/app/trash/usecase"
	userhandler "github.com/SyafaHadyan/kibooz-backend/internal/app/user/interface/rest"
	userrepository "github.com/SyafaHadyan/kibooz-backend/internal/app/user/repository"
	userusecase "github.com/SyafaHadyan/kibooz-backend/internal/app/user/usecase"
	walihandler "github.com/SyafaHadyan/kibooz-backend/internal/app/wali/interface/rest"
	walirepository "github.com/SyafaHadyan/kibooz-backend/internal/app/wali/repository"
	waliusecase "github.com/SyafaHadyan/kibooz-backend/internal/app/wali/usecase"
	"github.com/SyafaHadyan/kibooz-backend/internal/chatlive"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/errorreport"
	"github.com/SyafaHadyan/kibooz-backend/internal/filecleanup"
	"github.com/SyafaHadyan/kibooz-backend/internal/idempotency"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/db"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	fiberapp "github.com/SyafaHadyan/kibooz-backend/internal/infra/fiber"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/jwt"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/passwords"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/redis"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/s3"
	"github.com/SyafaHadyan/kibooz-backend/internal/keepalive"
	"github.com/SyafaHadyan/kibooz-backend/internal/metrics"
	"github.com/SyafaHadyan/kibooz-backend/internal/middleware"
	"github.com/SyafaHadyan/kibooz-backend/internal/push"
	"github.com/SyafaHadyan/kibooz-backend/internal/season"
	"github.com/SyafaHadyan/kibooz-backend/internal/sessions"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"gorm.io/gorm"
)

type Bootstrap struct {
	App      *fiberapp.Fiber
	Config   *env.Env
	Database *gorm.DB
	Redis    *redis.Redis

	stopKeepalive func()
	stopCleanup   func()
	stopSeason    func()
	stopKeys      func()
	stopTokens    func()
	stopReports   func()
	stopLive      func()
	stopPush      func()
	metrics       *metrics.Listener
	healthDB      *sql.DB
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

	privateStorage, err := s3.NewPrivate(cfg)
	if err != nil {
		return nil, err
	}

	if !privateStorage.Enabled() {
		log.Println("the private bucket is not configured, chat photos and videos are disabled")
	}

	// the requests that remove a file again after a failure use these, so a delete that the bucket refused is queued for the sweep
	retrying := filecleanup.RetryFailedDeletes(storage, database)
	retryingPrivate := filecleanup.RetryFailedDeletes(privateStorage, database)

	jwtService := jwt.New(cfg)
	// the metrics middleware runs inside the access log and ahead of every check and route, so each request is measured,
	// the ones that the body checks refuse too, and a refusal is counted by its route
	stats := metrics.New()
	app := fiberapp.New(cfg, redis.NewRateStore(cache), stats.Middleware())

	err = stats.Registry().Register(collectors.NewDBStatsCollector(sqlDB, "kibooz"))
	if err != nil {
		return nil, fmt.Errorf("register database pool metrics: %w", err)
	}

	keys := idempotency.New(
		database, time.Duration(cfg.IdempotencyTTLHours)*time.Hour, time.Duration(cfg.RequestTimeoutSeconds)*time.Second,
	)
	mw := middleware.NewMiddleware(jwtService, app.UserLimiter(middleware.UserKey), keys.Handler(), app.UploadGate(middleware.UserKey))

	// every bucket that is on is probed, so a private bucket that refuses writes shows in /healthz as well
	var pingStorage pinger
	if storage.Enabled() {
		probed := []s3.StorageItf{storage}
		if privateStorage.Enabled() {
			probed = append(probed, privateStorage)
		}

		pingStorage = newStorageProbe(probed...).Ping
	}

	// a connection of its own, so the health check does not queue behind the requests that use the pool
	healthDB, err := db.NewHealth(cfg)
	if err != nil {
		return nil, err
	}

	app.Fiber.Get("/healthz", healthHandler(healthDB.PingContext, cache.Ping, pingStorage, version))

	// one pool for every use case that hashes or checks a password, so the slots are the budget of the whole process
	hasher := passwords.New(cfg)

	authhandler.NewAuthHandler(app.Router, app.EmailLimiter(), app.ClassCodeLimiter(), app.TokenLimiter(), authusecase.NewAuthUseCase(
		authrepository.NewAuthDB(database), jwtService, cache, cfg, hasher,
	))
	walihandler.NewWaliHandler(app.Router, mw, app.ClassCodeLimiter(), waliusecase.NewWaliUseCase(
		walirepository.NewWaliDB(database, filecleanup.OwnFiles(storage, constants.AvatarDirectory, constants.TrashDirectory)), cache, cfg,
	))
	guruhandler.NewGuruHandler(app.Router, mw, app.MoodBatchLimiter(middleware.UserKey), guruusecase.NewGuruUseCase(
		gurure.NewGuruDB(database, filecleanup.OwnFiles(storage, constants.AvatarDirectory, constants.TrashDirectory)), cache, cfg,
	))
	trashhandler.NewTrashHandler(app.Router, mw, trashusecase.NewTrashUseCase(
		trashrepository.NewTrashDB(database), cache, retrying, cfg,
	))
	classroomhandler.NewClassroomHandler(app.Router, mw, classroomusecase.NewClassroomUseCase(
		classroomrepository.NewClassroomDB(database), retrying, cfg,
	))
	// the hub delivers chat events to the sockets of this copy, and to the other copies over Redis, which is only a shortcut
	hub := chatlive.NewHub(chatlive.Config{
		MaxSockets: cfg.ChatMaxSockets, MaxPerUser: cfg.ChatMaxSocketsPerUser, Bus: chatlive.NewRedisBus(cache),
	})

	for _, collector := range hub.Collectors() {
		err = stats.Registry().Register(collector)
		if err != nil {
			return nil, fmt.Errorf("register chat socket metrics: %w", err)
		}
	}

	liveCtx, cancelLive := context.WithCancel(context.Background())

	var live sync.WaitGroup

	live.Go(func() { hub.Run(liveCtx) })

	stopLive := func() {
		// the sockets get a going away frame and the apps connect to another copy, then the subscription ends
		hub.Shutdown()
		cancelLive()
		live.Wait()
	}

	pushRepository := pushrepository.NewPushDB(database)

	pushhandler.NewPushHandler(app.Router, mw, pushusecase.NewPushUseCase(pushRepository))

	// a closed app is woken through Firebase when a key is set. A key that cannot be used leaves the pushes off and the API running,
	// because a notification is a shortcut and never a reason to stay down.
	var (
		pusher   chatusecase.Pusher
		stopPush = func() {}
	)

	if cfg.FirebaseCredentialsFile != "" {
		sender, pushErr := push.NewFCM(cfg.FirebaseCredentialsFile)
		if pushErr != nil {
			log.Printf("push notifications are off because the Firebase key cannot be used %v", pushErr)
		} else {
			dispatcher := push.NewDispatcher(pushRepository, sender)

			for _, collector := range dispatcher.Collectors() {
				err = stats.Registry().Register(collector)
				if err != nil {
					return nil, fmt.Errorf("register push metrics: %w", err)
				}
			}

			dispatcher.Start()

			pusher, stopPush = dispatcher, dispatcher.Stop

			log.Println("push notifications are on")
		}
	}

	chathandler.NewChatHandler(
		app.Router, mw, chatusecase.NewChatUseCase(chatrepository.NewChatDB(database), retryingPrivate, cfg, hub, pusher), hub, cfg,
	)
	userhandler.NewUserHandler(app.Router, app.PasswordLimiter(middleware.UserKey), mw, userusecase.NewUserUseCase(
		userrepository.NewUserDB(database), retrying, cache, hasher,
	))

	stats.AllowRoutes(app.Fiber.GetRoutes())

	metricsListener, err := stats.Serve(cfg.MetricsAddress)
	if err != nil {
		// nothing else was started yet, so the connections are all that has to be let go
		_ = healthDB.Close()
		_ = sqlDB.Close()
		_ = cache.Close()

		return nil, err
	}

	log.Printf("startup time %v", time.Since(startTime))

	// a keepalive that is switched off has no job to age, so only a running one is registered
	var observeDatabase, observeRedis func(error)
	if cfg.KeepaliveSeconds > 0 {
		observeDatabase, observeRedis = stats.Job("keepalive_database"), stats.Job("keepalive_redis")
	}

	stopKeepalive := keepalive.Start(
		time.Duration(cfg.KeepaliveSeconds)*time.Second,
		keepalive.Target{Name: "database", Observe: observeDatabase, Ping: func(ctx context.Context) error {
			_, err := sqlDB.ExecContext(ctx, "SELECT 1")

			return err
		}},
		keepalive.Target{Name: "redis", Observe: observeRedis, Ping: cache.Ping},
	)

	stopSeason := season.New(database, cache, cfg.Location(), cfg.SemesterStartMonths).
		Observe(stats.Job("semester_check")).Start(season.Interval)
	stopKeys := keys.Observe(stats.Job("idempotency_purge")).Start(idempotency.Interval)
	stopTokens := sessions.NewSweeper(database, time.Duration(cfg.JWTSessionMaxDays)*24*time.Hour).
		Observe(stats.Job("refresh_token_purge")).Start(sessions.Interval)

	stopCleanup := func() {}
	if storage.Enabled() {
		stopCleanup = filecleanup.New(database, storage).WithPrivate(privateStorage).
			Observe(stats.Job("file_cleanup")).Start(filecleanup.Interval)
	}

	// the reports start last, so that a failure while the API starts is only logged, and they stop last, so that the errors of the
	// shutdown still leave
	stopReports := errorreport.Start(errorreport.Config{
		DSN: cfg.SentryDSN, Environment: cfg.SentryEnvironment, ServerName: cfg.SentryServerName, Release: version,
		EventsPerMinute: cfg.SentryEventsPerMinute,
	})

	return &Bootstrap{
		App: app, Config: cfg, Database: database, Redis: cache, stopKeepalive: stopKeepalive, stopCleanup: stopCleanup,
		stopSeason: stopSeason, stopKeys: stopKeys, stopTokens: stopTokens, stopReports: stopReports, stopLive: stopLive, stopPush: stopPush, metrics: metricsListener, healthDB: healthDB,
	}, nil
}

// Close stops the keepalive, then releases the database and Redis connections
func (b *Bootstrap) Close() {
	b.stopLive()
	b.stopPush()
	b.stopKeepalive()
	b.stopCleanup()
	b.stopSeason()
	b.stopKeys()
	b.stopTokens()
	b.metrics.Close()

	_ = b.healthDB.Close()

	if sqlDB, err := b.Database.DB(); err == nil {
		_ = sqlDB.Close()
	}

	_ = b.Redis.Close()

	b.stopReports()
}

type pinger func(ctx context.Context) error

// healthHandler fails only when the database is down, and a Redis outage or a bucket that refuses writes marks the
// service degraded. A nil pingStorage means storage is not configured, which is a choice and not a fault.
func healthHandler(pingDB pinger, pingRedis pinger, pingStorage pinger, version string) fiber.Handler {
	return func(c fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.Context(), 2*time.Second)
		defer cancel()

		status := http.StatusOK
		state := "ok"
		checks := fiber.Map{"database": "ok", "redis": "ok", "storage": "ok"}

		if pingStorage == nil {
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

		if pingStorage != nil {
			if err := pingStorage(ctx); err != nil {
				log.Printf("storage write probe failed %v", err)

				checks["storage"] = "down"

				if status == http.StatusOK {
					state = "degraded"
				}
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
