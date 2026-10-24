// Command bucketsizes measures the size of the buckets of an S3 compatible store and serves the numbers as Prometheus metrics
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/SyafaHadyan/kibooz-backend/internal/bucketsizes"
	"github.com/SyafaHadyan/kibooz-backend/internal/healthcheck"
	"github.com/SyafaHadyan/kibooz-backend/internal/logging"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		port, err := bucketsizes.ListenPort(os.Getenv)
		if err != nil {
			log.Fatalf("settings %v", err)
		}

		os.Exit(healthcheck.Run(port))
	}

	logging.Setup()

	settings, err := bucketsizes.LoadSettings(os.Getenv)
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
	scanner := bucketsizes.NewScanner(lister, settings.Targets, settings.ScanTimeout, registry)

	go scanner.Run(ctx, settings.Interval)

	log.Printf("measuring %d buckets every %s", len(settings.Targets), settings.Interval)

	err = bucketsizes.Serve(ctx, settings.Address, bucketsizes.Handler(registry))
	if err != nil {
		log.Fatalf("server %v", err)
	}
}
