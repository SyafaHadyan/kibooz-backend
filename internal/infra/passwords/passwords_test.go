package passwords

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
)

func fast(slots int, wait time.Duration) *Hasher {
	hasher := New(&env.Env{PasswordHashConcurrency: slots, PasswordHashWaitMilliseconds: int(wait.Milliseconds())})
	hasher.cost = bcrypt.MinCost

	return hasher
}

func TestOnlyTheConfiguredNumberOfHashesRunAtOnce(t *testing.T) {
	hasher := fast(2, 20*time.Millisecond)

	first, err := hasher.Acquire(t.Context())
	require.NoError(t, err)

	second, err := hasher.Acquire(t.Context())
	require.NoError(t, err)

	started := time.Now()
	_, err = hasher.Acquire(t.Context())
	require.ErrorIs(t, err, apperror.ErrBusy)
	require.GreaterOrEqual(t, time.Since(started), 15*time.Millisecond, "a caller waits before it is refused")

	t.Run("a slot that is given back is used by the next caller", func(t *testing.T) {
		first()

		third, err := hasher.Acquire(t.Context())
		require.NoError(t, err)

		third()
		second()
	})
}

func TestACallerWaitsForASlotThatIsFreedInTime(t *testing.T) {
	hasher := fast(1, time.Second)

	release, err := hasher.Acquire(t.Context())
	require.NoError(t, err)

	go func() {
		time.Sleep(30 * time.Millisecond)
		release()
	}()

	next, err := hasher.Acquire(t.Context())
	require.NoError(t, err, "a short spike is absorbed and nobody is refused")

	next()
}

func TestAContextThatEndsStopsTheWait(t *testing.T) {
	hasher := fast(1, time.Minute)

	release, err := hasher.Acquire(t.Context())
	require.NoError(t, err)

	defer release()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = hasher.Acquire(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestAContextThatEndedTakesNoFreeSlot(t *testing.T) {
	hasher := fast(1, time.Minute)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := hasher.Acquire(ctx)
	require.ErrorIs(t, err, context.Canceled)

	_, err = hasher.Hash(ctx, "a password")
	require.ErrorIs(t, err, context.Canceled)

	release, err := hasher.Acquire(t.Context())
	require.NoError(t, err, "the slot must still be free")

	release()
}

func TestHashAndMatchesWorkTogether(t *testing.T) {
	hasher := fast(1, time.Second)

	hash, err := hasher.Hash(t.Context(), "correct horse")
	require.NoError(t, err)

	ok, err := hasher.Matches(t.Context(), hash, "correct horse")
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = hasher.Matches(t.Context(), hash, "wrong horse")
	require.NoError(t, err, "a wrong password is an answer and not an error")
	require.False(t, ok)
}

func TestHashAndMatchesGiveTheSlotBack(t *testing.T) {
	hasher := fast(1, 20*time.Millisecond)

	for range 5 {
		hash, err := hasher.Hash(t.Context(), "password")
		require.NoError(t, err)

		_, err = hasher.Matches(t.Context(), hash, "password")
		require.NoError(t, err)
	}
}

func TestBothAreRefusedWhileEverySlotIsTaken(t *testing.T) {
	hasher := fast(1, 10*time.Millisecond)

	release, err := hasher.Acquire(t.Context())
	require.NoError(t, err)

	defer release()

	_, err = hasher.Hash(t.Context(), "password")
	require.ErrorIs(t, err, apperror.ErrBusy)

	_, err = hasher.Matches(t.Context(), "hash", "password")
	require.ErrorIs(t, err, apperror.ErrBusy)
}

func TestZeroSlotsLeavesOneCPUFree(t *testing.T) {
	hasher := New(&env.Env{PasswordHashWaitMilliseconds: 10})

	require.Equal(t, max(runtime.GOMAXPROCS(0)-1, 1), cap(hasher.slots))
}

func TestAConfiguredNumberOfSlotsIsKept(t *testing.T) {
	hasher := New(&env.Env{PasswordHashConcurrency: 3, PasswordHashWaitMilliseconds: 10})

	require.Equal(t, 3, cap(hasher.slots))
}

func TestNewHashesWithTheConfiguredCost(t *testing.T) {
	tests := map[string]struct {
		configured int
		want       int
	}{
		"the default":                {configured: 10, want: 10},
		"a lower cost for tests":     {configured: bcrypt.MinCost, want: bcrypt.MinCost},
		"a config that has no cost":  {configured: 0, want: bcrypt.DefaultCost},
		"a cost that bcrypt refuses": {configured: 2, want: bcrypt.DefaultCost},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			hasher := New(&env.Env{PasswordHashConcurrency: 1, PasswordHashWaitMilliseconds: 10, PasswordHashCost: tc.configured})

			hash, err := hasher.Hash(t.Context(), "a-password-for-the-test")
			require.NoError(t, err)

			cost, err := bcrypt.Cost([]byte(hash))
			require.NoError(t, err)
			require.Equal(t, tc.want, cost)
		})
	}
}

func TestTheDummyHashHasTheCostOfTheConfiguredHashes(t *testing.T) {
	hasher := New(&env.Env{PasswordHashConcurrency: 1, PasswordHashWaitMilliseconds: 10, PasswordHashCost: bcrypt.MinCost})

	cost, err := bcrypt.Cost([]byte(hasher.DummyHash()))
	require.NoError(t, err)
	require.Equal(t, bcrypt.MinCost, cost)

	matches, err := hasher.Matches(t.Context(), hasher.DummyHash(), "not-the-dummy-password")
	require.NoError(t, err)
	require.False(t, matches, "no real password matches the dummy hash")

	require.Equal(t, hasher.DummyHash(), hasher.DummyHash(), "the hash is made once")
}

func TestACostAboveTheLimitOfBcryptFallsBackToTheDefault(t *testing.T) {
	hasher := New(&env.Env{PasswordHashConcurrency: 1, PasswordHashWaitMilliseconds: 10, PasswordHashCost: bcrypt.MaxCost + 1})

	cost, err := bcrypt.Cost([]byte(hasher.DummyHash()))
	require.NoError(t, err)
	require.Equal(t, bcrypt.DefaultCost, cost)
}
