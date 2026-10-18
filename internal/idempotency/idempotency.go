// Package idempotency lets a client repeat a request without repeating what it did. An app that queues an action while it
// is offline sends one Idempotency-Key for the action, and every try of it carries the same key. A try after the first
// one that worked gets the first response again, and nothing is written twice.
//
// The keys live in PostgreSQL and not in Redis, because Redis is an optional accelerator here, and a Redis outage would
// turn every repeat back into a duplicate.
package idempotency

import (
	"context"
	"crypto/sha256"
	"errors"
	"log"
	"regexp"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/middleware"
)

const (
	// Header names the key that the client sends
	Header = "Idempotency-Key"
	// ReplayedHeader is set to true on a response that is the stored first response of an earlier try
	ReplayedHeader = "Idempotent-Replayed"

	// Interval is how often keys that outlived their retention are deleted
	Interval = time.Hour

	// maxStoredBytes is the largest response that is kept. A bigger one is not stored and its key is freed, so the work
	// can be repeated, which is the safe side for a response that no route is expected to produce.
	maxStoredBytes = 1 << 20

	// sweepBatch is how many rows one delete removes, so a long backlog never holds one big lock
	sweepBatch = 1000

	// claimTries is how often a claim is retried when the row it found was freed again in between
	claimTries = 3

	// abandonMargin is added to the request timeout before a key that never got a response counts as abandoned
	abandonMargin = 30 * time.Second

	commandTimeout = 5 * time.Second
)

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// Store keeps the keys and answers repeats
type Store struct {
	db           *gorm.DB
	ttl          time.Duration
	abandonAfter time.Duration
}

// New builds a store that remembers keys for ttl. A first try that has not finished after requestTimeout and a margin
// is taken for dead, so a process that crashed mid request does not block the key until it expires.
func New(db *gorm.DB, ttl time.Duration, requestTimeout time.Duration) *Store {
	return &Store{db: db, ttl: ttl, abandonAfter: requestTimeout + abandonMargin}
}

// Handler runs a request once per key. It has to run after Authentication, because the key belongs to the signed in user,
// so two users can never meet each other's keys. A request without the header is passed on untouched. A response with a
// 2xx status is stored, and any other outcome frees the key so that the client can try again.
func (s *Store) Handler() fiber.Handler {
	return func(c fiber.Ctx) error {
		key := c.Get(Header)
		if key == "" {
			return c.Next()
		}

		if !keyPattern.MatchString(key) {
			return apperror.Validation(map[string]string{Header: "must hold 8 to 64 letters, digits, dashes or underscores"})
		}

		userID := middleware.UserIDFrom(c)
		fingerprint := fingerprintOf(c)

		replayed, claimID, err := s.Claim(c.Context(), userID, key, fingerprint)
		if err != nil {
			return err
		}

		if replayed != nil {
			c.Set(ReplayedHeader, "true")
			c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)

			return c.Status(replayed.Status).Send(replayed.Body)
		}

		// keep is true once the work is done and its response is to be remembered. Until then a failure frees the key, and
		// after it a failed write of the response leaves the key claimed, so a repeat is refused until the claim is
		// abandoned and cannot do the work twice.
		keep := false

		defer func() {
			if !keep {
				s.Release(c.Context(), userID, key, claimID)
			}
		}()

		err = c.Next()
		if err != nil {
			return err
		}

		status := c.Response().StatusCode()
		body := c.Response().Body()

		if status < 200 || status > 299 || len(body) > maxStoredBytes {
			return nil
		}

		keep = true
		s.Complete(c.Context(), userID, key, claimID, status, body)

		return nil
	}
}

// Replay is the stored answer of a try that worked
type Replay struct {
	Status int
	Body   []byte
}

// Claim takes the key for this request. It returns a replay when an earlier try already worked, and an error when the key
// belongs to another request or to a try that is still running. No replay and no error means the work may start, and the
// returned id names this claim, so that Complete and Release can only touch the row while this request still owns it. A
// request that was taken for dead and finishes after another one took the key over then changes nothing.
func (s *Store) Claim(ctx context.Context, userID uuid.UUID, key string, fingerprint []byte) (*Replay, uuid.UUID, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), commandTimeout)
	defer cancel()

	claimID := uuid.New()

	for range claimTries {
		// a row that outlived its retention or that no running request owns any more is taken over
		res := s.db.WithContext(ctx).Exec(
			"INSERT INTO idempotency_keys (user_id, idempotency_key, fingerprint, claim_id) VALUES (?, ?, ?, ?) "+
				"ON CONFLICT (user_id, idempotency_key) DO UPDATE "+
				"SET fingerprint = EXCLUDED.fingerprint, claim_id = EXCLUDED.claim_id, status_code = NULL, response = NULL, created_at = now() "+
				"WHERE idempotency_keys.created_at < now() - ? * interval '1 second' "+
				"OR (idempotency_keys.status_code IS NULL AND idempotency_keys.created_at < now() - ? * interval '1 second')",
			userID, key, fingerprint, claimID, s.ttl.Seconds(), s.abandonAfter.Seconds(),
		)
		if res.Error != nil {
			return nil, uuid.Nil, apperror.Internal(res.Error)
		}

		if res.RowsAffected == 1 {
			return nil, claimID, nil
		}

		var row struct {
			Fingerprint []byte
			StatusCode  *int
			Response    []byte
		}

		res = s.db.WithContext(ctx).
			Raw("SELECT fingerprint, status_code, response FROM idempotency_keys WHERE user_id = ? AND idempotency_key = ?", userID, key).
			Scan(&row)
		if res.Error != nil {
			return nil, uuid.Nil, apperror.Internal(res.Error)
		}

		// the row was freed between the two statements, so claim it again
		if res.RowsAffected == 0 {
			continue
		}

		if string(row.Fingerprint) != string(fingerprint) {
			return nil, uuid.Nil, apperror.ErrIdempotencyReused
		}

		if row.StatusCode == nil {
			return nil, uuid.Nil, apperror.ErrIdempotencyBusy
		}

		return &Replay{Status: *row.StatusCode, Body: row.Response}, uuid.Nil, nil
	}

	return nil, uuid.Nil, apperror.ErrIdempotencyBusy
}

// Complete keeps the response of a try that worked, when the claim is still the one of the request. A failure is only
// logged, because the work is done and the client has to get its answer, and the key stays claimed until it counts as
// abandoned.
func (s *Store) Complete(ctx context.Context, userID uuid.UUID, key string, claimID uuid.UUID, status int, body []byte) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), commandTimeout)
	defer cancel()

	err := s.db.WithContext(ctx).Exec(
		"UPDATE idempotency_keys SET status_code = ?, response = ? "+
			"WHERE user_id = ? AND idempotency_key = ? AND claim_id = ? AND status_code IS NULL",
		status, append([]byte(nil), body...), userID, key, claimID,
	).Error
	if err != nil {
		log.Printf("storing the response of idempotency key %s failed %v", key, err)
	}
}

// Release frees a key whose request did not work, so the client can send it again. It leaves the row alone when another
// request has taken the key over since.
func (s *Store) Release(ctx context.Context, userID uuid.UUID, key string, claimID uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), commandTimeout)
	defer cancel()

	err := s.db.WithContext(ctx).Exec(
		"DELETE FROM idempotency_keys WHERE user_id = ? AND idempotency_key = ? AND claim_id = ? AND status_code IS NULL",
		userID, key, claimID,
	).Error
	if err != nil {
		log.Printf("freeing idempotency key %s failed %v", key, err)
	}
}

// fingerprintOf identifies a request by its method, its address with the query and its body, so a key can only repeat
// the request it was first used for
func fingerprintOf(c fiber.Ctx) []byte {
	sum := sha256.New()
	sum.Write([]byte(c.Method()))
	sum.Write([]byte{0})
	sum.Write([]byte(c.OriginalURL()))
	sum.Write([]byte{0})
	sum.Write(c.Body())

	return sum.Sum(nil)
}

// Sweep deletes the keys that outlived their retention and returns how many it removed
func (s *Store) Sweep(ctx context.Context) (int, error) {
	total := 0

	for {
		res := s.db.WithContext(ctx).Exec(
			"DELETE FROM idempotency_keys WHERE ctid IN ("+
				"SELECT ctid FROM idempotency_keys WHERE created_at < now() - ? * interval '1 second' LIMIT ?)",
			s.ttl.Seconds(), sweepBatch,
		)
		if res.Error != nil {
			return total, res.Error
		}

		total += int(res.RowsAffected)

		if res.RowsAffected < sweepBatch {
			return total, nil
		}

		if ctx.Err() != nil {
			return total, ctx.Err()
		}
	}
}

// Start sweeps once per interval until the returned stop function is called, which waits for a sweep that is running.
// A zero or negative interval disables it.
func (s *Store) Start(interval time.Duration) (stop func()) {
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
				if err != nil && !errors.Is(err, context.Canceled) {
					log.Printf("idempotency key sweep failed %v", err)
				} else if removed > 0 {
					log.Printf("idempotency key sweep removed %d keys that outlived their retention", removed)
				}
			}
		}
	})

	return func() {
		cancel()
		wg.Wait()
	}
}
