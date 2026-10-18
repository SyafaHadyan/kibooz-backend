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
