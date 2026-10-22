// Package passwords hashes and checks passwords, and lets only a few of them run at the same time. A bcrypt hash costs
// tens of milliseconds of a whole core, and the sign in and sign up routes are open to anyone, so without a limit a
// flood of requests could keep every core busy and slow down the whole API. A request that cannot get a slot in a short
// time is refused with a answer that tells the app to wait, so the server stays responsive for everyone else.
package passwords

import (
	"context"
	"log"
	"runtime"
	"sync/atomic"
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
	// with a known email
	dummy string

	// waiters counts the callers that wait for a slot, and typical is the recent time of one hash in nanoseconds, which together
	// tell a caller that arrives whether it could get a slot before its wait is over
	waiters atomic.Int64
	typical atomic.Int64
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
	if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
		cost = bcrypt.DefaultCost
	}

	if cost < bcrypt.DefaultCost {
		log.Printf("PASSWORD_HASH_COST is %d, below the default of %d, so hashes are cheap to guess, use it for tests only", cost, bcrypt.DefaultCost)
	}

	// made here and not on the first sign in, so no sign in is slower than another and the dummy never goes missing. A fixed
	// short password and a cost in the range of bcrypt cannot fail, so a failure is a bug and the server must not start
	started := time.Now()

	dummy, err := bcrypt.GenerateFromPassword([]byte("kibooz-dummy-password"), cost)
	if err != nil {
		panic("passwords: the dummy hash could not be made: " + err.Error())
	}

	hasher := &Hasher{
		slots: make(chan struct{}, slots),
		wait:  time.Duration(cfg.PasswordHashWaitMilliseconds) * time.Millisecond,
		cost:  cost,
		dummy: string(dummy),
	}

	// the first hash of the process is the first estimate of how long one takes
	hasher.typical.Store(int64(time.Since(started)))

	return hasher
}

// DummyHash returns a hash that no password matches and that costs as much to check as a real one.
func (h *Hasher) DummyHash() string {
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

	// a caller that cannot get a slot before its wait is over is refused now and not after the wait, so a flood does not keep a
	// goroutine, a connection and a request for the whole wait each
	queued := h.waiters.Add(1)
	defer h.waiters.Add(-1)

	if h.cannotWaitThatLong(queued) {
		return nil, apperror.ErrBusy
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

// cannotWaitThatLong tells whether the callers that wait, this one included, need more time than the wait allows. Each round of
// the slots takes about the typical time of one hash, and the caller at the back of the line needs one round for every
// slots-sized group before it and its own. Before the first estimate exists nobody is refused early.
func (h *Hasher) cannotWaitThatLong(queued int64) bool {
	typical := h.typical.Load()
	if typical <= 0 {
		return false
	}

	rounds := (queued + int64(cap(h.slots)) - 1) / int64(cap(h.slots))

	return time.Duration(rounds*typical) > h.wait
}

// took folds the time of a hash into the typical time, so the estimate follows a loaded machine and a changed cost
func (h *Hasher) took(started time.Time) {
	sample := int64(time.Since(started))
	if sample <= 0 {
		return
	}

	// a lost update under a race only makes the estimate a little late, so there is no loop
	h.typical.Store((h.typical.Load()*7 + sample) / 8)
}

// Hash returns the bcrypt hash of the password. The error is apperror.ErrBusy when no slot was free in time.
func (h *Hasher) Hash(ctx context.Context, password string) (string, error) {
	release, err := h.Acquire(ctx)
	if err != nil {
		return "", err
	}
	defer release()

	defer h.took(time.Now())

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

	defer h.took(time.Now())

	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil, nil
}
