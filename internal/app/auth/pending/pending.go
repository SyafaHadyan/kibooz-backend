// Package pending removes the accounts that waited too long for the code of their email
package pending

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/kiboozapp/kibooz-backend/internal/app/auth/repository"
	"github.com/kiboozapp/kibooz-backend/internal/constants"
)

const (
	// Interval is how often the accounts that waited too long are removed
	Interval = time.Hour

	// startDelay is how long after the start the first run happens, which is shorter than the interval so that a copy that is
	// restarted more often than that still runs
	startDelay = time.Minute

	// batch is how many accounts one run looks at, so a long backlog never holds the database for long. The rest wait for the next run.
	batch = 100
)

// Source lists the accounts that waited since before a time
type Source interface {
	OlderPendingAccounts(ctx context.Context, before time.Time, limit int) ([]repository.PendingAccount, error)
}

// Remover removes one account with everything that hangs on it, as long as it still waits since before the given time
type Remover func(ctx context.Context, account repository.PendingAccount, before time.Time) error

// Sweeper removes the accounts that waited for the code of their email for PendingPurgeAfterDays. It must only run while the
// verification is required, because an account that waits for a code can sign in when it is not.
type Sweeper struct {
	source  Source
	remove  Remover
	observe func(error)
	now     func() time.Time
	// startAfter is how long after the start the first run happens, a field for the tests
	startAfter time.Duration
}

func NewSweeper(source Source, remove Remover) *Sweeper {
	return &Sweeper{source: source, remove: remove, now: time.Now, startAfter: startDelay}
}

// Observe sets the function that gets the result of every run, nil for a run that went well, and returns the sweeper
func (s *Sweeper) Observe(observe func(error)) *Sweeper {
	s.observe = observe

	return s
}

// Sweep removes the accounts that waited too long and returns how many it removed. An account that cannot be removed is left for
// the next run and does not stop the others, and the run then returns the errors.
func (s *Sweeper) Sweep(ctx context.Context) (int, error) {
	before := s.now().Add(-constants.PendingPurgeAfterDays * 24 * time.Hour)

	accounts, err := s.source.OlderPendingAccounts(ctx, before, batch)
	if err != nil {
		return 0, err
	}

	removed := 0

	var failures []error

	for _, account := range accounts {
		if ctx.Err() != nil {
			return removed, ctx.Err()
		}

		err = s.remove(ctx, account, before)
		if err != nil {
			failures = append(failures, fmt.Errorf("account %s was not removed: %w", account.ID, err))

			continue
		}

		removed++
	}

	return removed, errors.Join(failures...)
}

// run is one sweep with its report to the observer and the log
func (s *Sweeper) run(ctx context.Context) {
	removed, err := s.Sweep(ctx)
	if s.observe != nil && ctx.Err() == nil {
		s.observe(err)
	}

	if err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("pending account sweep failed %v", err)
	}

	if removed > 0 {
		log.Printf("pending account sweep removed %d accounts that waited more than %d days for their code", removed, constants.PendingPurgeAfterDays)
	}
}

// Start runs once per interval until the returned stop function is called, which waits for a run that is under way.
// A zero or negative interval disables it.
func (s *Sweeper) Start(interval time.Duration) (stop func()) {
	if interval <= 0 {
		return func() {}
	}

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup

	wg.Go(func() {
		timer := time.NewTimer(min(interval, s.startAfter))
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				s.run(ctx)
				timer.Reset(interval)
			}
		}
	})

	return func() {
		cancel()
		wg.Wait()
	}
}
