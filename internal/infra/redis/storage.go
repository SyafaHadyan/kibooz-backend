package redis

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// LimiterStorage backs the rate limiter with Redis so limits are shared across instances,
// and quietly falls back to process memory while Redis is unavailable
type LimiterStorage struct {
	r        *Redis
	prefix   string
	fallback *memoryStore
}

func NewLimiterStorage(r *Redis, prefix string) *LimiterStorage {
	return &LimiterStorage{r: r, prefix: prefix, fallback: newMemoryStore()}
}

func (s *LimiterStorage) key(key string) string {
	return s.prefix + key
}

func (s *LimiterStorage) GetWithContext(ctx context.Context, key string) ([]byte, error) {
	if s.r.allow() == nil {
		value, err := s.r.Client.Get(ctx, s.key(key)).Bytes()
		if errors.Is(err, redis.Nil) {
			return nil, s.r.record(nil)
		}

		if s.r.record(err) == nil {
			return value, nil
		}
	}

	return s.fallback.get(key), nil
}

func (s *LimiterStorage) Get(key string) ([]byte, error) {
	return s.GetWithContext(context.Background(), key)
}

func (s *LimiterStorage) SetWithContext(ctx context.Context, key string, val []byte, exp time.Duration) error {
	if key == "" || len(val) == 0 {
		return nil
	}

	if s.r.allow() == nil && s.r.record(s.r.Client.Set(ctx, s.key(key), val, exp).Err()) == nil {
		return nil
	}

	s.fallback.set(key, val, exp)

	return nil
}

func (s *LimiterStorage) Set(key string, val []byte, exp time.Duration) error {
	return s.SetWithContext(context.Background(), key, val, exp)
}

func (s *LimiterStorage) DeleteWithContext(ctx context.Context, key string) error {
	s.fallback.delete(key)

	if s.r.allow() == nil {
		_ = s.r.record(s.r.Client.Del(ctx, s.key(key)).Err())
	}

	return nil
}

func (s *LimiterStorage) Delete(key string) error {
	return s.DeleteWithContext(context.Background(), key)
}

func (s *LimiterStorage) ResetWithContext(ctx context.Context) error {
	s.fallback.reset()

	// a failing Redis only means there is nothing to clear there
	if s.r.allow() == nil {
		s.resetRedis(ctx)
	}

	return nil
}

func (s *LimiterStorage) resetRedis(ctx context.Context) {
	var cursor uint64

	for {
		keys, next, err := s.r.Client.Scan(ctx, cursor, s.prefix+"*", 200).Result()
		if s.r.record(err) != nil {
			return
		}

		if len(keys) > 0 {
			_ = s.r.Client.Del(ctx, keys...).Err()
		}

		cursor = next
		if cursor == 0 {
			return
		}
	}
}

func (s *LimiterStorage) Reset() error {
	return s.ResetWithContext(context.Background())
}

// Close is a no-op because the shared client is closed by its owner
func (s *LimiterStorage) Close() error {
	return nil
}

type memoryEntry struct {
	value   []byte
	expires time.Time
}

type memoryStore struct {
	mu      sync.Mutex
	entries map[string]memoryEntry
}

func newMemoryStore() *memoryStore {
	return &memoryStore{entries: make(map[string]memoryEntry)}
}

func (m *memoryStore) get(key string) []byte {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.entries[key]
	if !ok {
		return nil
	}

	if !entry.expires.IsZero() && time.Now().After(entry.expires) {
		delete(m.entries, key)

		return nil
	}

	return entry.value
}

func (m *memoryStore) set(key string, value []byte, ttl time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// bound memory use by dropping expired entries once the map grows
	if len(m.entries) > 10000 {
		now := time.Now()

		for k, entry := range m.entries {
			if !entry.expires.IsZero() && now.After(entry.expires) {
				delete(m.entries, k)
			}
		}
	}

	entry := memoryEntry{value: append([]byte(nil), value...)}
	if ttl > 0 {
		entry.expires = time.Now().Add(ttl)
	}

	m.entries[key] = entry
}

func (m *memoryStore) delete(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.entries, key)
}

func (m *memoryStore) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	clear(m.entries)
}
