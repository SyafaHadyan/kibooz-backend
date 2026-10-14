// Package filecleanup retries the removal of files that a deleted account left in the bucket. A file is queued when the
// first attempt failed, because the rows that pointed at it are gone and nothing else would find it again.
package filecleanup

import (
	"context"
	"log"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/SyafaHadyan/kibooz-backend/internal/infra/s3"
)

const (
	// Interval is how often the queue is swept
	Interval = 10 * time.Minute
	// batch is how many files one sweep tries, so a long outage is worked off over several sweeps
	batch = 100
	// attemptTimeout bounds the removal of one file
	attemptTimeout = 5 * time.Second
)

// Sweeper removes the queued files from the bucket
type Sweeper struct {
	db      *gorm.DB
	storage s3.StorageItf
}

func New(db *gorm.DB, storage s3.StorageItf) *Sweeper {
	return &Sweeper{db: db, storage: storage}
}

// Sweep tries every queued file of one batch, the ones with the fewest failed attempts first and the oldest among them,
// so files that keep failing cannot keep newer ones from being tried. A file that is gone leaves the queue, and one that still
// fails stays with one more attempt counted. It returns how many files were removed.
func (s *Sweeper) Sweep(ctx context.Context) (int, error) {
	var keys []string

	err := s.db.WithContext(ctx).Raw("SELECT object_key FROM file_cleanups ORDER BY attempts, queued_at, object_key LIMIT ?", batch).
		Scan(&keys).Error
	if err != nil {
		return 0, err
	}

	removed := 0

	for _, key := range keys {
		if ctx.Err() != nil {
			break
		}

		if s.attempt(ctx, key) {
			removed++
		}
	}

	return removed, nil
}

// attempt deletes one file and takes it off the queue, and tells whether that worked
func (s *Sweeper) attempt(ctx context.Context, key string) bool {
	attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
	defer cancel()

	err := s.storage.Delete(attemptCtx, key)
	if err != nil {
		log.Printf("storage cleanup of %s failed again %v", key, err)

		if err := s.db.WithContext(ctx).Exec("UPDATE file_cleanups SET attempts = attempts + 1 WHERE object_key = ?", key).Error; err != nil {
			log.Printf("count the cleanup attempt of %s failed %v", key, err)
		}

		return false
	}

	if err := s.db.WithContext(ctx).Exec("DELETE FROM file_cleanups WHERE object_key = ?", key).Error; err != nil {
		log.Printf("take %s off the cleanup queue failed %v", key, err)

		return false
	}

	return true
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
				if err != nil {
					log.Printf("storage cleanup sweep failed %v", err)
				} else if removed > 0 {
					log.Printf("storage cleanup removed %d files that an earlier try missed", removed)
				}
			}
		}
	})

	return func() {
		cancel()
		wg.Wait()
	}
}
