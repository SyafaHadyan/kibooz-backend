// Package mailretry sends the verification code again to the accounts whose mail failed
package mailretry

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/mailer"
)

const (
	// Interval is how often the job looks for a mail that is due, which is short because the first retries come after a minute
	Interval = time.Minute

	// startDelay is how long after the start the first run happens
	startDelay = 20 * time.Second

	// batch is how many accounts one run takes. One mail may take the 5 seconds of its timeout, so a run stays well inside the lease.
	batch = 10

	// lease is how long a claimed account is left alone, so a copy that dies in the middle of a run does not lose the retry and a
	// second copy does not mail the same account at the same time
	lease = 5 * time.Minute

	// retryTimeout bounds one account. It is shorter than the cooldown of a resend, so a retry that stalls cannot mail a code
	// after the person got another one
	retryTimeout = 30 * time.Second

	// quotaPause is how long the job stops after the mail service refused for its quota
	quotaPause = time.Hour
)

// Source claims the accounts whose mail is due
type Source interface {
	ClaimMailRetries(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]uuid.UUID, error)
}

// Retrier mails a new code to one account
type Retrier func(ctx context.Context, userID uuid.UUID) error

// Job tries the mail of the accounts that wait for a code again, after 1, 5 and 15 minutes and then every hour, a few times in all.
// It must only run while the verification is required.
type Job struct {
	source  Source
	retry   Retrier
	observe func(error)
	now     func() time.Time

	// startAfter is how long after the start the first run happens, a field for the tests
	startAfter time.Duration

	mu          sync.Mutex
	pausedUntil time.Time
}

func NewJob(source Source, retry Retrier) *Job {
	return &Job{source: source, retry: retry, now: time.Now, startAfter: startDelay}
}

// Observe sets the function that gets the result of every run, nil for a run that went well, and returns the job
func (j *Job) Observe(observe func(error)) *Job {
	j.observe = observe

	return j
}

// Run takes the accounts that are due and mails each of them. It returns how many it tried. A quota answer stops the run and the job
// for an hour, and an answer about the rate stops the run. Any other failure of one account does not stop the others, and the run
// returns those errors.
func (j *Job) Run(ctx context.Context) (int, error) {
	j.mu.Lock()
	paused := j.now().Before(j.pausedUntil)
	j.mu.Unlock()

	if paused {
		return 0, nil
	}

	ids, err := j.source.ClaimMailRetries(ctx, j.now(), lease, batch)
	if err != nil {
		return 0, err
	}

	tried := 0

	var failures []error

	for _, id := range ids {
		if ctx.Err() != nil {
			return tried, ctx.Err()
		}

		tried++

		err = j.one(ctx, id)
		if errors.Is(err, mailer.ErrRateLimit) {
			log.Printf("the mail service refused for its rate, the rest of the verification mail retries wait for their lease")

			break
		}

		if errors.Is(err, mailer.ErrQuota) {
			j.mu.Lock()
			j.pausedUntil = j.now().Add(quotaPause)
			j.mu.Unlock()

			log.Printf("the mail service refused for its quota, the verification mail retries stop for %s", quotaPause)

			break
		}

		if err != nil {
			failures = append(failures, fmt.Errorf("account %s: %w", id, err))
		}
	}

	return tried, errors.Join(failures...)
}

// one tries one account. A send that is under way is not cut off by a stop of the job, which would count a mail that may have
// been delivered as failed.
func (j *Job) one(ctx context.Context, id uuid.UUID) error {
	retryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), retryTimeout)
	defer cancel()

	return j.retry(retryCtx, id)
}

// run is one run with its report to the observer and the log
func (j *Job) run(ctx context.Context) {
	tried, err := j.Run(ctx)
	if j.observe != nil && ctx.Err() == nil {
		j.observe(err)
	}

	if err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("verification mail retry run failed %v", err)
	}

	if tried > 0 {
		log.Printf("verification mail retry tried %d accounts", tried)
	}
}

// Start runs once per interval until the returned stop function is called, which waits for a run that is under way.
// A zero or negative interval disables it.
func (j *Job) Start(interval time.Duration) (stop func()) {
	if interval <= 0 {
		return func() {}
	}

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup

	wg.Go(func() {
		timer := time.NewTimer(min(interval, j.startAfter))
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				j.run(ctx)
				timer.Reset(interval)
			}
		}
	})

	return func() {
		cancel()
		wg.Wait()
	}
}
