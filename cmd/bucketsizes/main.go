// Command bucketsizes measures the size of the buckets of an S3 compatible store and serves the numbers as Prometheus metrics
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/SyafaHadyan/kibooz-backend/internal/bucketsizes"
	"github.com/SyafaHadyan/kibooz-backend/internal/healthcheck"
	"github.com/SyafaHadyan/kibooz-backend/internal/logging"
)

// scanTimeout is how long the listing of one bucket may take, which is a lot of pages for a bucket with millions of objects
const scanTimeout = 15 * time.Minute

func main() {
	settings, err := bucketsizes.LoadSettings(os.Getenv)
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		// the address is the same one that the server listens on, and a setting that cannot be read falls back to the default port
		os.Exit(healthcheck.Run(bucketsizes.Port(valueOr(settings.Address, ":9101"))))
	}

	logging.Setup()

	if err != nil {
		log.Fatalf("settings %v", err)
	}

	lister, err := bucketsizes.NewS3Lister(context.Background(), settings.S3)
	if err != nil {
		log.Fatalf("store %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	registry := prometheus.NewRegistry()
	scanner := bucketsizes.NewScanner(lister, settings.Targets, scanTimeout, registry)

	go scanner.Run(ctx, settings.Interval)

	log.Printf("measuring %d buckets every %s", len(settings.Targets), settings.Interval)

	err = bucketsizes.Serve(ctx, settings.Address, bucketsizes.Handler(registry))
	if err != nil {
		log.Fatalf("server %v", err)
	}
}

func valueOr(value string, fallback string) string {
	if value != "" {
		return value
	}

	return fallback
}
