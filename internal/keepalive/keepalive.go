// Package keepalive sends a cheap request to the database and Redis on a timer, so hosted instances that
// pause or evict idle connections keep seeing activity
package keepalive

import (
	"context"
	"log"
	"sync"
	"time"
)

// timeout bounds one request so a stuck dependency cannot pile up behind the next tick
const timeout = 5 * time.Second

// Target is one dependency and the cheapest request that proves it is alive
type Target struct {
	Name string
	Ping func(ctx context.Context) error
}

// Start pings every target once per interval until the returned stop function is called.
// A zero or negative interval disables it. Stop waits for a request that is already running.
func Start(interval time.Duration, targets ...Target) (stop func()) {
	if interval <= 0 || len(targets) == 0 {
		return func() {}
	}

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup

	wg.Go(func() { run(ctx, interval, targets) })

	return func() {
		cancel()
		wg.Wait()
	}
}

func run(ctx context.Context, interval time.Duration, targets []Target) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	failing := make(map[string]bool, len(targets))

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, target := range targets {
				ping(ctx, target, failing)
			}
		}
	}
}

// ping logs only when a target changes between healthy and failing, so a long outage does not flood the log
func ping(ctx context.Context, target Target, failing map[string]bool) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	err := target.Ping(ctx)

	switch {
	case err != nil && !failing[target.Name]:
		failing[target.Name] = true

		log.Printf("keepalive %s failed %v", target.Name, err)
	case err == nil && failing[target.Name]:
		failing[target.Name] = false

		log.Printf("keepalive %s recovered", target.Name)
	}
}
