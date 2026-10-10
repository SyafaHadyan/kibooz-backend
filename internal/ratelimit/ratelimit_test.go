package ratelimit_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/ratelimit"
)

const window = time.Minute

func TestWindowsAreTheStretchesOfTheWindowLengthSinceTheEpoch(t *testing.T) {
	index, elapsed := ratelimit.Window(time.Unix(0, 0).Add(3*window+20*time.Second), window)

	require.EqualValues(t, 3, index)
	require.Equal(t, 20*time.Second, elapsed)
}

func TestEstimateCountsThePreviousWindowForThePartThatIsStillCovered(t *testing.T) {
	for name, test := range map[string]struct {
		hits ratelimit.Hits
		want int64
	}{
		"start of the window":  {ratelimit.Hits{Current: 1, Previous: 10, Elapsed: 0}, 11},
		"middle of the window": {ratelimit.Hits{Current: 1, Previous: 10, Elapsed: 30 * time.Second}, 6},
		"a part is rounded up": {ratelimit.Hits{Current: 1, Previous: 10, Elapsed: 2 * time.Second}, 11},
		"one request before":   {ratelimit.Hits{Current: 1, Previous: 1, Elapsed: 59 * time.Second}, 2},
		"nothing left":         {ratelimit.Hits{Current: 2, Previous: 5, Elapsed: window}, 2},
		"end of the window":    {ratelimit.Hits{Current: 4, Previous: 10, Elapsed: window}, 4},
		"nothing before":       {ratelimit.Hits{Current: 3, Elapsed: 10 * time.Second}, 3},
	} {
		require.Equal(t, test.want, test.hits.Estimate(window), name)
	}
}

// at returns a memory store whose clock is the one that the test moves
func at(clock *time.Time) *ratelimit.Memory {
	return ratelimit.NewMemoryWithClock(func() time.Time { return *clock })
}

func TestMemoryCountsPerKeyAndPerWindow(t *testing.T) {
	ctx := context.Background()
	clock := time.Unix(0, 0).Add(10 * time.Second)
	store := at(&clock)

	for want := int64(1); want <= 3; want++ {
		hits, err := store.Hit(ctx, "a", window)
		require.NoError(t, err)
		require.Equal(t, want, hits.Current)
		require.Zero(t, hits.Previous)
	}

	other, err := store.Hit(ctx, "b", window)
	require.NoError(t, err)
	require.EqualValues(t, 1, other.Current, "a key has its own count")

	clock = clock.Add(window)

	next, err := store.Hit(ctx, "a", window)
	require.NoError(t, err)
	require.EqualValues(t, 1, next.Current)
	require.EqualValues(t, 3, next.Previous, "the window that just ended")
	require.Equal(t, 10*time.Second, next.Elapsed)
}

func TestMemoryForgetsWindowsThatAreTooOld(t *testing.T) {
	ctx := context.Background()
	clock := time.Unix(0, 0)
	store := at(&clock)

	_, err := store.Hit(ctx, "a", window)
	require.NoError(t, err)

	// two windows later, the window before the current one is empty
	clock = clock.Add(2 * window)

	hits, err := store.Hit(ctx, "a", window)
	require.NoError(t, err)
	require.EqualValues(t, 1, hits.Current)
	require.Zero(t, hits.Previous)
}

func TestMemoryDropsExpiredKeysWhenItGrows(t *testing.T) {
	ctx := context.Background()
	clock := time.Unix(0, 0)
	store := at(&clock)

	for i := range 10001 {
		_, err := store.Hit(ctx, string(rune(i+1000)), window)
		require.NoError(t, err)
	}

	clock = clock.Add(5 * window)

	hits, err := store.Hit(ctx, "fresh", window)
	require.NoError(t, err)
	require.EqualValues(t, 1, hits.Current)
	require.Equal(t, 1, store.Len(), "everything that expired was dropped")
}

func TestMemoryDoesNotLoseHitsUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	clock := time.Unix(0, 0).Add(time.Second)
	store := at(&clock)

	var group sync.WaitGroup

	for range 200 {
		group.Add(1)

		go func() {
			defer group.Done()

			_, err := store.Hit(ctx, "busy", window)
			require.NoError(t, err)
		}()
	}

	group.Wait()

	hits, err := store.Hit(ctx, "busy", window)
	require.NoError(t, err)
	require.EqualValues(t, 201, hits.Current)
}

func TestMemorySweepsOnlyOncePerWindow(t *testing.T) {
	ctx := context.Background()
	clock := time.Unix(0, 0)
	store := at(&clock)

	for i := range 10001 {
		_, err := store.Hit(ctx, string(rune(i+1000)), window)
		require.NoError(t, err)
	}

	// the first sweep finds everything alive and sets the time of the next one
	_, err := store.Hit(ctx, "a", window)
	require.NoError(t, err)

	// these keys expire before the next sweep is allowed, so they must still be there
	clock = clock.Add(2*window + time.Second)
	_, err = store.Hit(ctx, "b", window)
	require.NoError(t, err)
	require.Equal(t, 1, store.Len(), "the sweep of a new window removed the expired keys")

	for i := range 10001 {
		_, err := store.Hit(ctx, string(rune(i+1000)), window)
		require.NoError(t, err)
	}

	clock = clock.Add(3 * window)
	_, err = store.Hit(ctx, "c", window)
	require.NoError(t, err)

	// a sweep ran, and another one is not allowed until a window has passed
	size := store.Len()
	_, err = store.Hit(ctx, "d", window)
	require.NoError(t, err)
	require.Equal(t, size+1, store.Len(), "no second sweep within the same window")
}
