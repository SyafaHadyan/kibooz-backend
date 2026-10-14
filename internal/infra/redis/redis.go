// Package redis wraps go-redis as an optional accelerator. Every failure is contained so the API keeps working without Redis.
package redis

import (
	"context"
	"crypto/tls"
	"errors"
	"log"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
)

const (
	// downFor is how long calls are skipped after a failure, so an outage costs no network time
	downFor = 5 * time.Second

	// probeFor is how long the one call that tests a Redis that was down may take. A call is over in about two seconds
	// even when the network drops every packet, and a call that never reports does not keep the others out for longer.
	probeFor = 3 * time.Second

	// rejectionLogEvery is how often a command that Redis answers with an error is logged
	rejectionLogEvery = time.Minute

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
	Client *redis.Client
	// downUntil is zero while Redis is in use. After a failure it is the moment when one call may test Redis again, and
	// while that call is under way it is the moment when another may.
	downUntil atomic.Int64
	down      atomic.Bool

	lastRejection atomic.Int64

	// prefix comes before every key, so a Redis that other applications use too never mixes their keys with these
	prefix string
}

// New never fails. A Redis that cannot be reached at startup only produces a warning.
func New(cfg *env.Env) *Redis {
	r := &Redis{Client: redis.NewClient(newOptions(cfg)), prefix: cfg.RedisKeyPrefix}

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

// allow returns ErrUnavailable while the breaker is open. Once the pause is over it lets exactly one call through to find
// out whether Redis is back and keeps the others out until that call has reported, so a Redis that is still down costs one
// slow call and not one for every request that arrives in the meantime.
func (r *Redis) allow() error {
	until := r.downUntil.Load()
	if until == 0 {
		return nil
	}

	now := time.Now().UnixNano()
	if now < until {
		return ErrUnavailable
	}

	if r.downUntil.CompareAndSwap(until, now+int64(probeFor)) {
		return nil
	}

	return ErrUnavailable
}

// record updates the breaker from the result of a call and logs only when the state changes
func (r *Redis) record(ctx context.Context, err error) error {
	switch kind := classify(ctx, err); kind {
	case healthy, rejected:
		// Redis answered, so it is reachable even when it refused the command
		r.downUntil.Store(0)

		if r.down.CompareAndSwap(true, false) {
			log.Println("redis is available again")
		}

		if kind == rejected {
			r.logRejection(err)
		}
	case abandoned:
		// the caller gave up, which says nothing about the health of Redis
	case outage:
		r.downUntil.Store(time.Now().Add(downFor).UnixNano())

		if r.down.CompareAndSwap(false, true) {
			log.Printf("redis became unavailable, continuing without it %v", err)
		}
	}

	return err
}

// logRejection logs an error that Redis answered with, at most once a minute because it comes with every call
func (r *Redis) logRejection(err error) {
	now := time.Now().UnixNano()

	last := r.lastRejection.Load()
	if now-last < int64(rejectionLogEvery) || !r.lastRejection.CompareAndSwap(last, now) {
		return
	}

	log.Printf("redis refused a command, the call goes on without it %v", err)
}

type outcome int

const (
	healthy outcome = iota
	// rejected is an error that Redis answered with, such as a key of the wrong type or a user without the permission
	rejected
	// abandoned is a call that the caller gave up on, by cancelling it or by running out of time
	abandoned
	// outage is anything that says Redis cannot be used right now
	outage
)

// unavailableReplies are the errors that Redis answers with while it cannot serve the call, and not because of the call
var unavailableReplies = []string{"LOADING", "READONLY", "MASTERDOWN", "CLUSTERDOWN", "BUSY", "TRYAGAIN", "OOM"}

func classify(ctx context.Context, err error) outcome {
	if err == nil || errors.Is(err, redis.Nil) {
		return healthy
	}

	// only a caller whose own context has ended gave up. A timeout that the client raises on its own, while the context is
	// still alive, says that Redis did not answer in time.
	if ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return abandoned
	}

	var reply redis.Error
	if !errors.As(err, &reply) {
		return outage
	}

	for _, prefix := range unavailableReplies {
		if strings.HasPrefix(reply.Error(), prefix) {
			return outage
		}
	}

	return rejected
}

// key returns the name that a key has in Redis
func (r *Redis) key(key string) string {
	return r.prefix + key
}

// Available reports whether calls reach Redis. It only looks, and never takes the one call that tests a Redis that was down.
func (r *Redis) Available() bool {
	until := r.downUntil.Load()

	return until == 0 || time.Now().UnixNano() >= until
}

func (r *Redis) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	if err := r.allow(); err != nil {
		return err
	}

	return r.record(ctx, r.Client.Set(ctx, r.key(key), value, ttl).Err())
}

func (r *Redis) Get(ctx context.Context, key string) (string, bool, error) {
	if err := r.allow(); err != nil {
		return "", false, err
	}

	value, err := r.Client.Get(ctx, r.key(key)).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, r.record(ctx, nil)
	}

	if err != nil {
		return "", false, r.record(ctx, err)
	}

	return value, true, r.record(ctx, nil)
}

func (r *Redis) Del(ctx context.Context, keys ...string) error {
	if err := r.allow(); err != nil {
		return err
	}

	prefixed := make([]string, len(keys))
	for i, key := range keys {
		prefixed[i] = r.key(key)
	}

	return r.record(ctx, r.Client.Del(ctx, prefixed...).Err())
}

func (r *Redis) MarkUsed(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if err := r.allow(); err != nil {
		return false, err
	}

	previous, err := r.Client.SetArgs(ctx, r.key(key), usedMarker, redis.SetArgs{Mode: "XX", Get: true, TTL: ttl}).Result()
	if errors.Is(err, redis.Nil) {
		return false, r.record(ctx, nil)
	}

	if err != nil {
		return false, r.record(ctx, err)
	}

	return previous == usedMarker, r.record(ctx, nil)
}

func (r *Redis) Ping(ctx context.Context) error {
	if err := r.allow(); err != nil {
		return err
	}

	return r.record(ctx, r.Client.Ping(ctx).Err())
}

func (r *Redis) Close() error {
	return r.Client.Close()
}
