// Command kibooz-backend starts the Kibooz REST API
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/gofiber/fiber/v3"

	"github.com/SyafaHadyan/kibooz-backend/internal/bootstrap"
	"github.com/SyafaHadyan/kibooz-backend/internal/healthcheck"
	"github.com/SyafaHadyan/kibooz-backend/internal/logging"
)

// version is set at build time with -ldflags
var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck.Run(os.Getenv("APP_PORT")))
	}

	// before the first line, so everything the process prints is JSON
	logging.Setup()

	log.Printf("starting kibooz-backend %s", version)

	// before Start, because Start runs the migrations and the default action of SIGTERM ends the process in the middle of one,
	// which leaves the migration table marked dirty and the next start refused. A signal that arrives meanwhile waits in the
	// channel until Start has returned. Everything else that Start does is bounded and fails fast.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	app, err := bootstrap.Start(version)
	if err != nil {
		log.Fatalf("failed to start %v", err)
	}

	defer app.Close()

	// a stop that arrived while it started, and nothing is listening yet that has to be shut down
	select {
	case <-quit:
		log.Println("stopped while starting")

		return
	default:
	}

	serverErr := make(chan error, 1)

	go func() {
		serverErr <- app.App.Fiber.Listen(fmt.Sprintf(":%d", app.Config.AppPort), fiber.ListenConfig{DisableStartupMessage: true})
	}()

	select {
	case err := <-serverErr:
		log.Fatalf("server stopped %v", err)
	case <-quit:
		log.Println("gracefully shutting down")
	}

	err = app.App.Fiber.ShutdownWithTimeout(10 * time.Second)
	if err != nil {
		log.Printf("graceful shutdown failed %v", err)
	}

	log.Println("shutdown complete")
}
