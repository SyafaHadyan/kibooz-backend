// Package passwords hashes and checks passwords, and lets only a few of them run at the same time. A bcrypt hash costs
// tens of milliseconds of a whole core, and the sign in and sign up routes are open to anyone, so without a limit a
// flood of requests could keep every core busy and slow down the whole API. A request that cannot get a slot in a short
// time is refused with a answer that tells the app to wait, so the server stays responsive for everyone else.
package passwords

import (
	"context"
	"runtime"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
)

// Hasher is shared by every use case that hashes or checks a password, because the slots are the budget of the whole process.
type Hasher struct {
	slots chan struct{}
	wait  time.Duration
	cost  int
}

// New sizes the pool from PASSWORD_HASH_CONCURRENCY, and one slot for every CPU when that is 0.
func New(cfg *env.Env) *Hasher {
	slots := cfg.PasswordHashConcurrency
	if slots <= 0 {
		slots = runtime.GOMAXPROCS(0)
	}

	return &Hasher{
		slots: make(chan struct{}, slots),
		wait:  time.Duration(cfg.PasswordHashWaitMilliseconds) * time.Millisecond,
		cost:  bcrypt.DefaultCost,
	}
}

// Acquire takes a slot and returns the function that gives it back. A caller that finds every slot taken waits for the
// configured time and then gets apperror.ErrBusy. A context that ends first gives its own error.
func (h *Hasher) Acquire(ctx context.Context) (release func(), err error) {
	release = func() { <-h.slots }

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	select {
	case h.slots <- struct{}{}:
		return release, nil
	default:
	}

	timer := time.NewTimer(h.wait)
	defer timer.Stop()

	select {
	case h.slots <- struct{}{}:
		return release, nil
	case <-timer.C:
		return nil, apperror.ErrBusy
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Hash returns the bcrypt hash of the password. The error is apperror.ErrBusy when no slot was free in time.
func (h *Hasher) Hash(ctx context.Context, password string) (string, error) {
	release, err := h.Acquire(ctx)
	if err != nil {
		return "", err
	}
	defer release()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), h.cost)
	if err != nil {
		return "", err
	}

	return string(hash), nil
}

// Matches tells whether the password belongs to the hash. A password that does not match is false with no error. The
// error is for a check that could not run, which is apperror.ErrBusy when no slot was free in time.
func (h *Hasher) Matches(ctx context.Context, hash string, password string) (bool, error) {
	release, err := h.Acquire(ctx)
	if err != nil {
		return false, err
	}
	defer release()

	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil, nil
}
