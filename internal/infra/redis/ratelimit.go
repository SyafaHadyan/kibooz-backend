package redis

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/SyafaHadyan/kibooz-backend/internal/ratelimit"
)

// RateStore counts requests in Redis, so every instance that shares the Redis shares the limit. A request is counted by
// one transaction of atomic commands and no instance reads a value to write it back, so two instances that count at the
// same moment never lose a request. While Redis is down every instance counts in its own memory, which only makes the
// limit per instance for a while.
type RateStore struct {
	r        *Redis
	fallback *ratelimit.Memory
	now      func() time.Time
}

func NewRateStore(r *Redis) *RateStore {
	return NewRateStoreWithClock(r, time.Now)
}

// NewRateStoreWithClock reads the time from now, so a test can move it
func NewRateStoreWithClock(r *Redis, now func() time.Time) *RateStore {
	return &RateStore{r: r, fallback: ratelimit.NewMemoryWithClock(now), now: now}
}

// limiterPrefix keeps the counters apart from the other keys of the application
const limiterPrefix = "limiter:"

func (s *RateStore) Hit(ctx context.Context, key string, window time.Duration) (ratelimit.Hits, error) {
	if s.r.allow() == nil {
		hits, err := s.hit(ctx, key, window)
		if s.r.record(err) == nil {
			return hits, nil
		}
	}

	return s.fallback.Hit(ctx, key, window)
}

func (s *RateStore) hit(ctx context.Context, key string, window time.Duration) (ratelimit.Hits, error) {
	index, elapsed := ratelimit.Window(s.now(), window)

	current := s.r.key(limiterPrefix + key + ":" + strconv.FormatInt(index, 10))
	previous := s.r.key(limiterPrefix + key + ":" + strconv.FormatInt(index-1, 10))

	pipe := s.r.Client.TxPipeline()
	count := pipe.Incr(ctx, current)
	// the window after this one still needs the count, and nothing after that
	pipe.PExpire(ctx, current, 2*window)
	before := pipe.Get(ctx, previous)

	// the exec reports a window without requests as a missing key, which is no failure
	_, err := pipe.Exec(ctx)
	if err != nil && !errors.Is(err, redis.Nil) {
		return ratelimit.Hits{}, err
	}

	hits := ratelimit.Hits{Elapsed: elapsed}
	hits.Current, err = count.Result()
	hits.Previous, _ = before.Int64()

	return hits, err
}
