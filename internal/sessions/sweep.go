package sessions

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"gorm.io/gorm"
)

const (
	// Interval is how often the refresh tokens that no longer matter are deleted
	Interval = time.Hour

	// sweepBatch is how many tokens one statement deletes, so a long backlog never holds a lock for long
	sweepBatch = 1000
)

// Sweeper deletes the refresh tokens that no longer matter, for every account. A refresh only cleans up the tokens of the
// account that refreshes, so the tokens of a person who never comes back would stay for ever, and the table would grow by about
// one row for each refresh of each session.
//
// A token matters while it can still be refreshed, which is until it expires, and a used token matters while its session lives,
// because showing it again must still end that session. So a token that was not used goes when it expires, and a used token goes
// when its session has reached its longest lifetime, which is the same rule that a refresh applies to one account.
type Sweeper struct {
	db         *gorm.DB
	sessionMax time.Duration
	observe    func(error)
	// batch deletes one batch and returns how many rows went, a field so that a test can run the loop without a database
	batch func(ctx context.Context) (int64, error)
}

func NewSweeper(db *gorm.DB, sessionMax time.Duration) *Sweeper {
	s := &Sweeper{db: db, sessionMax: sessionMax}
	s.batch = s.deleteBatch

	return s
}

// Observe sets the function that gets the result of every run, nil for a run that went well, and returns the sweeper
func (s *Sweeper) Observe(observe func(error)) *Sweeper {
	s.observe = observe

	return s
}

// Sweep deletes the tokens that no longer matter in batches and returns how many it removed. A token that a refresh or a sign
// out has locked is skipped and left for the next run, so the sweep never waits for a lock and cannot close a circle of
// waits with them.
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
		"DELETE FROM refresh_tokens WHERE id IN ("+
			"SELECT id FROM refresh_tokens WHERE (used_at IS NULL AND expires_at <= now()) OR "+
			"(used_at IS NOT NULL AND session_started_at <= now() - ? * interval '1 second') LIMIT ? FOR UPDATE SKIP LOCKED)",
		s.sessionMax.Seconds(), sweepBatch,
	)

	return res.RowsAffected, res.Error
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
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				removed, err := s.Sweep(ctx)
				if s.observe != nil && ctx.Err() == nil {
					s.observe(err)
				}

				if err != nil && !errors.Is(err, context.Canceled) {
					log.Printf("refresh token sweep failed %v", err)
				} else if removed > 0 {
					log.Printf("refresh token sweep removed %d tokens that no longer matter", removed)
				}
			}
		}
	})

	return func() {
		cancel()
		wg.Wait()
	}
}
