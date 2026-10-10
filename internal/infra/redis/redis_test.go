package redis_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/redis"
)

// port 1 never has a listener, which is how an unreachable Redis looks
func unreachable() *redis.Redis {
	return redis.New(&env.Env{RedisAddress: "127.0.0.1", RedisPort: 1})
}

func TestUnreachableRedisFailsFastAfterTheFirstError(t *testing.T) {
	cache := unreachable()
	ctx := context.Background()

	require.Error(t, cache.Set(ctx, "key", "value", time.Minute))
	require.False(t, cache.Available())

	started := time.Now()

	_, _, err := cache.Get(ctx, "key")
	require.ErrorIs(t, err, redis.ErrUnavailable)

	_, err = cache.MarkUsed(ctx, "key", time.Minute)
	require.ErrorIs(t, err, redis.ErrUnavailable)

	require.Less(t, time.Since(started), 100*time.Millisecond)
}

func TestRateStoreCountsInMemoryWhileRedisIsDown(t *testing.T) {
	store := redis.NewRateStore(unreachable())
	ctx := context.Background()

	for want := int64(1); want <= 3; want++ {
		hits, err := store.Hit(ctx, "user:1", time.Minute)
		require.NoError(t, err, "a request is never refused because Redis is down")
		require.Equal(t, want, hits.Current)
	}

	other, err := store.Hit(ctx, "user:2", time.Minute)
	require.NoError(t, err)
	require.EqualValues(t, 1, other.Current, "every key has its own count")
}

func TestRateStoreFallbackMovesOnToTheNextWindow(t *testing.T) {
	clock := time.Unix(0, 0).Add(5 * time.Second)
	store := redis.NewRateStoreWithClock(unreachable(), func() time.Time { return clock })
	ctx := context.Background()

	_, err := store.Hit(ctx, "user:1", time.Minute)
	require.NoError(t, err)

	clock = clock.Add(time.Minute)

	hits, err := store.Hit(ctx, "user:1", time.Minute)
	require.NoError(t, err)
	require.EqualValues(t, 1, hits.Current)
	require.EqualValues(t, 1, hits.Previous)
	require.Equal(t, 5*time.Second, hits.Elapsed)
}
