// Package passwords hashes and checks passwords, and lets only a few of them run at the same time. A bcrypt hash costs
// tens of milliseconds of a whole core, and the sign in and sign up routes are open to anyone, so without a limit a
// flood of requests could keep every core busy and slow down the whole API. A request that cannot get a slot in a short
// time is refused with a answer that tells the app to wait, so the server stays responsive for everyone else.
package passwords

import (
	"context"
	"log"
	"runtime"
	"sync"
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

	// dummy is a hash of the configured cost for a sign in with an email that nobody registered, so that it takes as long as one
	// with a known email. It is made on first use, so a process that never signs in does not pay for it.
	dummyOnce sync.Once
	dummy     string
}

// New sizes the pool from PASSWORD_HASH_CONCURRENCY. When that is 0 it leaves one CPU free for everything else, so a flood of
// hashing cannot starve the requests that hash nothing, and a machine with a single CPU still gets one slot.
func New(cfg *env.Env) *Hasher {
	slots := cfg.PasswordHashConcurrency
	if slots <= 0 {
		slots = max(runtime.GOMAXPROCS(0)-1, 1)
	}

	// a cost that bcrypt would not accept is the default, which only happens for a config that did not come from the environment
	cost := cfg.PasswordHashCost
	if cost < bcrypt.MinCost {
		cost = bcrypt.DefaultCost
	}

	if cost < bcrypt.DefaultCost {
		log.Printf("PASSWORD_HASH_COST is %d, below the default of %d, so hashes are cheap to guess, use it for tests only", cost, bcrypt.DefaultCost)
	}

	return &Hasher{
		slots: make(chan struct{}, slots),
		wait:  time.Duration(cfg.PasswordHashWaitMilliseconds) * time.Millisecond,
		cost:  cost,
	}
}

// DummyHash returns a hash that no password matches and that costs as much to check as a real one.
func (h *Hasher) DummyHash() string {
	h.dummyOnce.Do(func() {
		hash, err := bcrypt.GenerateFromPassword([]byte("kibooz-dummy-password"), h.cost)
		if err == nil {
			h.dummy = string(hash)
		}
	})

	return h.dummy
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
