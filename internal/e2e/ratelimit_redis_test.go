package e2e

import (
	"context"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/redis"
)

// instance connects to the Redis of the suite the way another API instance would, under its own key prefix
func instance(t *testing.T, prefix string) *redis.Redis {
	t.Helper()

	app(t) // skips the test when the end to end suite is off

	port, err := strconv.ParseUint(os.Getenv("REDIS_PORT"), 10, 16)
	require.NoError(t, err)

	database, _ := strconv.Atoi(os.Getenv("REDIS_DATABASE"))

	cache := redis.New(&env.Env{
		RedisAddress: os.Getenv("REDIS_ADDRESS"), RedisPort: uint(port), RedisDatabase: database, RedisKeyPrefix: prefix,
	})

	t.Cleanup(func() { _ = cache.Close() })

	return cache
}

// fixed is a clock that stands still, so the test never crosses the end of a window by accident
func fixed(at time.Time) func() time.Time { return func() time.Time { return at } }

func TestInstancesThatShareARedisShareOneLimit(t *testing.T) {
	prefix := "e2e-" + suffix() + ":"
	now := time.Unix(0, 0).Add(10 * time.Minute).Add(7 * time.Second)

	first := redis.NewRateStoreWithClock(instance(t, prefix), fixed(now))
	second := redis.NewRateStoreWithClock(instance(t, prefix), fixed(now))
	ctx := context.Background()

	var group sync.WaitGroup

	for i := range 300 {
		group.Add(1)

		go func() {
			defer group.Done()

			store := first
			if i%2 == 1 {
				store = second
			}

			_, err := store.Hit(ctx, "email:burst", time.Hour)
			require.NoError(t, err)
		}()
	}

	group.Wait()

	// no request was lost, however the two instances interleaved
	hits, err := first.Hit(ctx, "email:burst", time.Hour)
	require.NoError(t, err)
	require.EqualValues(t, 301, hits.Current)
	require.Zero(t, hits.Previous)
	require.Equal(t, 10*time.Minute+7*time.Second, hits.Elapsed, "ten minutes and seven seconds into the window of the hour")

	t.Run("the count of the window before still counts in the next one", func(t *testing.T) {
		later := redis.NewRateStoreWithClock(instance(t, prefix), fixed(now.Add(time.Hour)))

		next, err := later.Hit(ctx, "email:burst", time.Hour)
		require.NoError(t, err)
		require.EqualValues(t, 1, next.Current)
		require.EqualValues(t, 301, next.Previous)
	})

	t.Run("the counters expire by themselves", func(t *testing.T) {
		client := redisClient(t)

		keys, err := client.Keys(ctx, prefix+"limiter:email:burst:*").Result()
		require.NoError(t, err)
		require.Len(t, keys, 2, "the window and the next one")

		for _, key := range keys {
			ttl, err := client.TTL(ctx, key).Result()
			require.NoError(t, err)
			require.Positive(t, ttl)
			require.LessOrEqual(t, ttl, 2*time.Hour)
		}
	})
}

func TestApplicationsThatShareARedisKeepTheirOwnKeys(t *testing.T) {
	ctx := context.Background()
	client := redisClient(t)

	mine := prefixed(t, "mine-")
	other := prefixed(t, "other-")

	require.NoError(t, mine.cache.Set(ctx, "refresh:abc", "user-1", time.Minute))
	require.NoError(t, other.cache.Set(ctx, "refresh:abc", "user-2", time.Minute))

	t.Run("the same key holds a different value for each", func(t *testing.T) {
		value, found, err := mine.cache.Get(ctx, "refresh:abc")
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, "user-1", value)

		raw, err := client.Get(ctx, mine.prefix+"refresh:abc").Result()
		require.NoError(t, err)
		require.Equal(t, "user-1", raw)

		_, err = client.Get(ctx, "refresh:abc").Result()
		require.Error(t, err, "nothing is stored without the prefix")
	})

	t.Run("marking a key as used and deleting it only reach the own key", func(t *testing.T) {
		used, err := mine.cache.MarkUsed(ctx, "refresh:abc", time.Minute)
		require.NoError(t, err)
		require.False(t, used)

		used, err = mine.cache.MarkUsed(ctx, "refresh:abc", time.Minute)
		require.NoError(t, err)
		require.True(t, used)

		other.requireValue(t, "refresh:abc", "user-2")

		require.NoError(t, mine.cache.Del(ctx, "refresh:abc"))

		_, found, err := mine.cache.Get(ctx, "refresh:abc")
		require.NoError(t, err)
		require.False(t, found)

		other.requireValue(t, "refresh:abc", "user-2")
	})

	t.Run("the counts of the limiter are apart too", func(t *testing.T) {
		now := fixed(time.Unix(0, 0).Add(time.Second))
		mineStore := redis.NewRateStoreWithClock(mine.cache, now)
		otherStore := redis.NewRateStoreWithClock(other.cache, now)

		for range 3 {
			_, err := mineStore.Hit(ctx, "user:1", time.Minute)
			require.NoError(t, err)
		}

		hits, err := otherStore.Hit(ctx, "user:1", time.Minute)
		require.NoError(t, err)
		require.EqualValues(t, 1, hits.Current)
	})
}

type prefixedRedis struct {
	cache  *redis.Redis
	prefix string
}

func prefixed(t *testing.T, name string) prefixedRedis {
	t.Helper()

	prefix := name + suffix() + ":"

	return prefixedRedis{cache: instance(t, prefix), prefix: prefix}
}

func (p prefixedRedis) requireValue(t *testing.T, key string, want string) {
	t.Helper()

	value, found, err := p.cache.Get(context.Background(), key)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, want, value)
}
