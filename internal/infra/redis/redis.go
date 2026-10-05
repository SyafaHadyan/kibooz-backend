// Package redis wraps go-redis as an optional accelerator. Every failure is contained so the API keeps working without Redis.
package redis

import (
	"context"
	"crypto/tls"
	"errors"
	"log"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
)

const (
	// downFor is how long calls are skipped after a failure, so an outage costs no network time
	downFor = 5 * time.Second

	usedMarker = "used"
)

// ErrUnavailable is returned without touching the network while Redis is considered down
var ErrUnavailable = errors.New("redis unavailable")

type CacheItf interface {
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	// Get returns found=false when the key does not exist
	Get(ctx context.Context, key string) (value string, found bool, err error)
	Del(ctx context.Context, keys ...string) error
	// MarkUsed atomically flags an existing key as used for ttl and reports whether it was already flagged.
	// A missing key is left missing and reports false.
	MarkUsed(ctx context.Context, key string, ttl time.Duration) (alreadyUsed bool, err error)
	// Available reports whether Redis is currently reachable
	Available() bool
	Ping(ctx context.Context) error
}

type Redis struct {
	Client    *redis.Client
	downUntil atomic.Int64
	down      atomic.Bool
}

// New never fails. A Redis that cannot be reached at startup only produces a warning.
func New(cfg *env.Env) *Redis {
	r := &Redis{Client: redis.NewClient(newOptions(cfg))}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// a failed ping opens the breaker, which already logs that Redis is unavailable
	_ = r.Ping(ctx)

	return r
}

func newOptions(cfg *env.Env) *redis.Options {
	opts := &redis.Options{
		Addr:         cfg.RedisAddress + ":" + strconv.FormatUint(uint64(cfg.RedisPort), 10),
		Username:     cfg.RedisUsername,
		Password:     cfg.RedisPassword,
		DB:           cfg.RedisDatabase,
		DialTimeout:  500 * time.Millisecond,
		ReadTimeout:  500 * time.Millisecond,
		WriteTimeout: 500 * time.Millisecond,
		MaxRetries:   1,
	}

	if cfg.RedisTLS {
		// hosted Redis only accepts TLS, and the certificate is checked against the configured host
		opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: cfg.RedisAddress}
	}

	return opts
}

// allow returns ErrUnavailable while the breaker is open
func (r *Redis) allow() error {
	if time.Now().UnixNano() < r.downUntil.Load() {
		return ErrUnavailable
	}

	return nil
}

// record updates the breaker from the result of a call and logs only when the state changes
func (r *Redis) record(err error) error {
	if err == nil || errors.Is(err, redis.Nil) {
		if r.down.CompareAndSwap(true, false) {
			log.Println("redis is available again")
		}

		return err
	}

	// a client that gave up on its own request says nothing about Redis health
	if errors.Is(err, context.Canceled) {
		return err
	}

	r.downUntil.Store(time.Now().Add(downFor).UnixNano())

	if r.down.CompareAndSwap(false, true) {
		log.Printf("redis became unavailable, continuing without it %v", err)
	}

	return err
}

func (r *Redis) Available() bool {
	return r.allow() == nil
}

func (r *Redis) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	if err := r.allow(); err != nil {
		return err
	}

	return r.record(r.Client.Set(ctx, key, value, ttl).Err())
}

func (r *Redis) Get(ctx context.Context, key string) (string, bool, error) {
	if err := r.allow(); err != nil {
		return "", false, err
	}

	value, err := r.Client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, r.record(nil)
	}

	if err != nil {
		return "", false, r.record(err)
	}

	return value, true, r.record(nil)
}

func (r *Redis) Del(ctx context.Context, keys ...string) error {
	if err := r.allow(); err != nil {
		return err
	}

	return r.record(r.Client.Del(ctx, keys...).Err())
}

func (r *Redis) MarkUsed(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if err := r.allow(); err != nil {
		return false, err
	}

	previous, err := r.Client.SetArgs(ctx, key, usedMarker, redis.SetArgs{Mode: "XX", Get: true, TTL: ttl}).Result()
	if errors.Is(err, redis.Nil) {
		return false, r.record(nil)
	}

	if err != nil {
		return false, r.record(err)
	}

	return previous == usedMarker, r.record(nil)
}

func (r *Redis) Ping(ctx context.Context) error {
	if err := r.allow(); err != nil {
		return err
	}

	return r.record(r.Client.Ping(ctx).Err())
}

func (r *Redis) Close() error {
	return r.Client.Close()
}
