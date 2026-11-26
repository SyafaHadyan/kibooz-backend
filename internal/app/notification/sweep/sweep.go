// Package sweep deletes the notifications that are older than the retention, which no inbox shows any more.
package sweep

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"gorm.io/gorm"
)

const (
	// Interval is how often the old notifications are deleted
	Interval = time.Hour

	// RetentionDays is how long a notification is kept, read or not
	RetentionDays = 90

	// startDelay is how long after the start the first sweep runs, which is shorter than the interval so that a copy that is
	// restarted more often than that still sweeps
	startDelay = time.Minute

	// sweepBatch is how many notifications one statement deletes, so a long backlog never holds a lock for long
	sweepBatch = 1000
)

// Sweeper deletes the notifications older than RetentionDays. A notification names a child, so it does not stay for ever.
type Sweeper struct {
	db      *gorm.DB
	observe func(error)
	// startAfter is how long after the start the first sweep runs, a field for the tests
	startAfter time.Duration
	// batch deletes one batch and returns how many rows went, a field so that a test can run the loop without a database
	batch func(ctx context.Context) (int64, error)
}

func NewSweeper(db *gorm.DB) *Sweeper {
	s := &Sweeper{db: db, startAfter: startDelay}
	s.batch = s.deleteBatch

	return s
}

// Observe sets the function that gets the result of every run, nil for a run that went well, and returns the sweeper
func (s *Sweeper) Observe(observe func(error)) *Sweeper {
	s.observe = observe

	return s
}

// Sweep deletes the old notifications in batches and returns how many it removed. A row that another statement has locked is
// skipped and left for the next run, so the sweep never waits for a lock.
func (s *Sweeper) Sweep(ctx context.Context) (int, error) {
	total := 0

	for {
		removed, err := s.batch(ctx)
		if err != nil {
			return total, err
		}

		total += int(removed)

		if removed < sweepBatch {
			return total, nil
		}

		if ctx.Err() != nil {
			return total, ctx.Err()
		}
	}
}

func (s *Sweeper) deleteBatch(ctx context.Context) (int64, error) {
	res := s.db.WithContext(ctx).Exec(
		"DELETE FROM notifications WHERE id IN ("+
			"SELECT id FROM notifications WHERE created_at < now() - make_interval(days => ?) LIMIT ? FOR UPDATE SKIP LOCKED)",
		RetentionDays, sweepBatch,
	)

	return res.RowsAffected, res.Error
}

// run is one sweep with its report to the observer and the log
func (s *Sweeper) run(ctx context.Context) {
	removed, err := s.Sweep(ctx)
	if s.observe != nil && ctx.Err() == nil {
		s.observe(err)
	}

	if err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("notification sweep failed %v", err)
	} else if removed > 0 {
		log.Printf("notification sweep removed %d notifications older than %d days", removed, RetentionDays)
	}
}

// Start sweeps once per interval until the returned stop function is called, which waits for a sweep that is running.
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
