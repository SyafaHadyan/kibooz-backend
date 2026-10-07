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
)

// version is set at build time with -ldflags
var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck.Run(os.Getenv("APP_PORT")))
	}

	log.Printf("starting kibooz-backend %s", version)

	app, err := bootstrap.Start(version)
	if err != nil {
		log.Fatalf("failed to start %v", err)
	}

	defer app.Close()

	serverErr := make(chan error, 1)

	go func() {
		serverErr <- app.App.Fiber.Listen(fmt.Sprintf(":%d", app.Config.AppPort), fiber.ListenConfig{DisableStartupMessage: true})
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

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
