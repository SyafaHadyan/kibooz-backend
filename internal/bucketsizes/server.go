package bucketsizes

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Handler answers GET /metrics with the last numbers and GET /healthz with 200 while the process runs
func Handler(registry *prometheus.Registry) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{ErrorLog: log.Default(), ErrorHandling: promhttp.ContinueOnError}))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	return mux
}

// Serve listens on the address and answers until the context ends. It listens before it returns, so a taken address stops the start.
func Serve(ctx context.Context, address string, handler http.Handler) error {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("listen on %s %w", address, err)
	}

	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Serve returns as soon as Shutdown starts, so the process waits for Shutdown to finish before it ends and a scrape that is under way
	// is answered
	stopped := make(chan struct{})

	go func() {
		defer close(stopped)

		<-ctx.Done()

		stop, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()

		_ = server.Shutdown(stop)
	}()

	log.Printf("bucket sizes listening on %s", listener.Addr())

	err = server.Serve(listener)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve %w", err)
	}

	<-stopped

	return nil
}
