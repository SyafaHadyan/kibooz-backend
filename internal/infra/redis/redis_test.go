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

func TestLimiterStorageFallsBackToMemory(t *testing.T) {
	storage := redis.NewLimiterStorage(unreachable(), "limiter:")

	require.NoError(t, storage.Set("ip", []byte("3"), time.Minute))

	value, err := storage.Get("ip")
	require.NoError(t, err)
	require.Equal(t, []byte("3"), value)

	require.NoError(t, storage.Delete("ip"))

	value, err = storage.Get("ip")
	require.NoError(t, err)
	require.Nil(t, value)
}

func TestLimiterStorageFallbackExpires(t *testing.T) {
	storage := redis.NewLimiterStorage(unreachable(), "limiter:")

	require.NoError(t, storage.Set("ip", []byte("1"), 20*time.Millisecond))
	time.Sleep(40 * time.Millisecond)

	value, err := storage.Get("ip")
	require.NoError(t, err)
	require.Nil(t, value)
}
