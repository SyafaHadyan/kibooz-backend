// Package filecleanup retries the removal of files that a deleted account left in the bucket. A file is queued when the
// first attempt failed, because the rows that pointed at it are gone and nothing else would find it again.
package filecleanup

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/s3"
)

const (
	// Interval is how often the queue is swept
	Interval = 10 * time.Minute
	// batch is how many files one sweep tries, so a long outage is worked off over several sweeps
	batch = 100
	// attemptTimeout bounds the removal of one file
	attemptTimeout = 5 * time.Second
	// lease is how long a file that one copy took for a try is kept from the other copy, a little more than the batch needs at its
	// worst, so a copy that dies in the middle only delays its files
	lease = batch * attemptTimeout
	// maxSteps caps the wait between tries, which grows by 10 minutes with each failed attempt, at 24 hours
	maxSteps = 144
)

// Sweeper removes the queued files from the bucket
type Sweeper struct {
	db      *gorm.DB
	storage s3.StorageItf
	// private holds the chat media, whose keys all start with PrivatePrefix, and is nil until WithPrivate sets it
	private s3.StorageItf
	observe func(error)
}

// PrivatePrefix starts every key of the private bucket, which is how the sweeper tells which bucket a queued key is in
const PrivatePrefix = "chat/"

func New(db *gorm.DB, storage s3.StorageItf) *Sweeper {
	return &Sweeper{db: db, storage: storage}
}

// WithPrivate sets the storage of the private bucket, for the keys that start with PrivatePrefix. Without it those keys are
// tried against the public bucket, which does not have them, so a deleted message would leave its file behind unseen.
func (s *Sweeper) WithPrivate(private s3.StorageItf) *Sweeper {
	s.private = private

	return s
}

// storageOf returns the bucket that holds the key
func (s *Sweeper) storageOf(key string) s3.StorageItf {
	if strings.HasPrefix(key, PrivatePrefix) && s.private != nil {
		return s.private
	}

	return s.storage
}

// Observe makes the sweeper call report with the error of every run that the shutdown did not cut short, nil for a success
func (s *Sweeper) Observe(report func(error)) *Sweeper {
	s.observe = report

	return s
}

// Sweep tries the queued files that are due, the longest waiting first, up to one batch. A file that is gone leaves the
// queue, and one that still fails waits longer before the next try, so files that keep failing cannot keep newer ones
// from being tried. It returns how many files were removed.
func (s *Sweeper) Sweep(ctx context.Context) (int, error) {
	var keys []string

	// the files are taken with a lease and not locked while the bucket is called, so the two copies of the API that sweep at the
	// same time do not try the same file and no transaction stays open for the length of 100 calls. A try that fails sets its own
	// later time, and a try that succeeds deletes the row.
	err := s.db.WithContext(ctx).
		Raw("UPDATE file_cleanups SET next_try_at = now() + make_interval(secs => ?) WHERE object_key IN ("+
			"SELECT object_key FROM file_cleanups WHERE next_try_at <= now() ORDER BY next_try_at, object_key LIMIT ? FOR UPDATE SKIP LOCKED"+
			") RETURNING object_key", lease.Seconds(), batch).
		Scan(&keys).Error
	if err != nil {
		return 0, err
	}

	removed := 0

	for _, key := range keys {
		if s.attempt(ctx, key) {
			removed++
		}

		if ctx.Err() != nil {
			return removed, ctx.Err()
		}
	}

	return removed, nil
}

// attempt deletes one file and takes it off the queue, and tells whether that worked
func (s *Sweeper) attempt(ctx context.Context, key string) bool {
	attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
	defer cancel()

	err := s.storageOf(key).Delete(attemptCtx, key)
	if err != nil {
		log.Printf("storage cleanup of %s failed again %v", key, err)

		if err := s.db.WithContext(ctx).Exec(
			"UPDATE file_cleanups SET attempts = attempts + 1, next_try_at = now() + LEAST(attempts + 1, ?) * interval '10 minutes' "+
				"WHERE object_key = ?",
			maxSteps, key,
		).Error; err != nil {
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
				if s.observe != nil && ctx.Err() == nil {
					s.observe(err)
				}

				if err != nil && ctx.Err() == nil {
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

// KeyFunc turns the public address of a file into its object key, and says false for an address that is not ours
type KeyFunc func(url string) (key string, ok bool)

// OwnFiles picks the files of this storage that lie under one of the directories out of the addresses rows hold, so an
// address that was set some other way is never deleted
func OwnFiles(storage s3.StorageItf, directories ...constants.StorageDirectory) KeyFunc {
	return func(url string) (string, bool) {
		key, ok := storage.KeyFromURL(url)
		if !ok {
			return "", false
		}

		for _, directory := range directories {
			if strings.HasPrefix(key, string(directory)+"/") {
				return key, true
			}
		}

		return "", false
	}
}

// TakeAddresses adds the file addresses that rows hold in column to files and clears them, so no row points at a file
// that is about to be deleted
func TakeAddresses(files *[]string, rows *gorm.DB, column string) error {
	var urls []string

	err := rows.Session(&gorm.Session{}).Where(column+" IS NOT NULL AND "+column+" <> ''").Pluck(column, &urls).Error
	if err != nil || len(urls) == 0 {
		return err
	}

	*files = append(*files, urls...)

	return rows.Session(&gorm.Session{}).Update(column, nil).Error
}

// KeysOf maps the addresses to object keys and leaves out the ones that are not files of this storage
func KeysOf(urls []string, fileKey KeyFunc) []string {
	keys := make([]string, 0, len(urls))

	for _, url := range urls {
		if key, ok := fileKey(url); ok {
			keys = append(keys, key)
		}
	}

	return keys
}

// queued is a row of the queue of files that still have to be removed from the bucket
type queued struct {
	ObjectKey string `gorm:"primaryKey"`
}

func (queued) TableName() string { return "file_cleanups" }

// Queue adds the keys to the cleanup queue in the transaction of the change that dropped their addresses, and keeps the
// ones that are already there as they are. The sweep removes them from the bucket.
func Queue(tx *gorm.DB, keys []string) error {
	if len(keys) == 0 {
		return nil
	}

	rows := make([]queued, 0, len(keys))
	for _, key := range keys {
		rows = append(rows, queued{ObjectKey: key})
	}

	// attempts and queued_at are left to the defaults of the table
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}
