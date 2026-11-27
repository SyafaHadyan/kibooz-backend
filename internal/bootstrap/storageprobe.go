package bootstrap

import (
	"context"
	"sync"
	"time"

	"github.com/kiboozapp/kibooz-backend/internal/infra/s3"
)

const (
	// storageProbeKey is written and removed again by every probe
	storageProbeKey = "healthcheck/probe"
	// storageProbeTTL is how long an answer is reused, so health checks do not turn into a stream of writes to the bucket
	storageProbeTTL = time.Minute
	// storageProbeTimeout is how long one probe of all the buckets may take. A write and a delete in each of two buckets took about
	// 2.5 to 2.9 seconds from the production host, which the 2 seconds of the health check never allowed. The limit stays under
	// the 5 seconds after which the container health check and the rolling update give up on a copy.
	storageProbeTimeout = 4 * time.Second
)

// storageProbe checks that every bucket accepts a write, which configured settings alone do not prove. It stores a tiny
// object in each and deletes it, and reuses the answer for storageProbeTTL so only one request in that time pays for the round trip.
type storageProbe struct {
	storages []s3.StorageItf
	ttl      time.Duration
	limit    time.Duration
	now      func() time.Time

	mu      sync.Mutex
	checked time.Time
	result  error
}

// newStorageProbe probes each of the buckets it is given, so the caller leaves out the ones that are not configured
func newStorageProbe(storages ...s3.StorageItf) *storageProbe {
	return &storageProbe{storages: storages, ttl: storageProbeTTL, limit: storageProbeTimeout, now: time.Now}
}

// Ping returns the cached outcome of the last probe, or runs a new one when it is older than the ttl. The probe runs under its
// own limit and not under the context of the caller, whose short limit and whose disconnect say nothing about the bucket. So the
// outcome is always about the bucket, and it is kept for the ttl whatever the caller did.
func (p *storageProbe) Ping(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.checked.IsZero() && p.now().Sub(p.checked) < p.ttl {
		return p.result
	}

	probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.limit)
	defer cancel()

	var err error

	for _, storage := range p.storages {
		_, err = storage.Upload(probeCtx, storageProbeKey, "text/plain", []byte("ok"))
		if err == nil {
			err = storage.Delete(probeCtx, storageProbeKey)
		}

		if err != nil {
			break
		}
	}

	p.checked = p.now()
	p.result = err

	return err
}
