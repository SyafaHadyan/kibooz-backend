package bootstrap

import (
	"context"
	"sync"
	"time"

	"github.com/SyafaHadyan/kibooz-backend/internal/infra/s3"
)

const (
	// storageProbeKey is written and removed again by every probe
	storageProbeKey = "healthcheck/probe"
	// storageProbeTTL is how long an answer is reused, so health checks do not turn into a stream of writes to the bucket
	storageProbeTTL = time.Minute
)

// storageProbe checks that every bucket accepts a write, which configured settings alone do not prove. It stores a tiny
// object in each and deletes it, and reuses the answer for storageProbeTTL so only one request in that time pays for the round trip.
type storageProbe struct {
	storages []s3.StorageItf
	ttl      time.Duration
	now      func() time.Time

	mu      sync.Mutex
	checked time.Time
	result  error
}

// newStorageProbe probes each of the buckets it is given, so the caller leaves out the ones that are not configured
func newStorageProbe(storages ...s3.StorageItf) *storageProbe {
	return &storageProbe{storages: storages, ttl: storageProbeTTL, now: time.Now}
}

// Ping returns the cached outcome of the last probe, or runs a new one when it is older than the ttl
func (p *storageProbe) Ping(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.checked.IsZero() && p.now().Sub(p.checked) < p.ttl {
		return p.result
	}

	var err error

	for _, storage := range p.storages {
		_, err = storage.Upload(ctx, storageProbeKey, "text/plain", []byte("ok"))
		if err == nil {
			err = storage.Delete(ctx, storageProbeKey)
		}

		if err != nil {
			break
		}
	}

	// a caller that gave up says nothing about the bucket, so its error must not be reused for the next minute
	if err == nil || ctx.Err() == nil {
		p.checked = p.now()
		p.result = err
	}

	return err
}
