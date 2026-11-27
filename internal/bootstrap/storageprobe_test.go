package bootstrap

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kiboozapp/kibooz-backend/internal/infra/s3"
)

// recordingStorage counts the calls of the probe and fails the ones it is told to
type recordingStorage struct {
	s3.Disabled

	uploads, deletes int
	uploadErr        error
	deleteErr        error
	keys             []string
}

func (r *recordingStorage) Upload(_ context.Context, key string, _ string, _ []byte) (string, error) {
	r.uploads++
	r.keys = append(r.keys, key)

	return "", r.uploadErr
}

func (r *recordingStorage) Delete(_ context.Context, key string) error {
	r.deletes++
	r.keys = append(r.keys, key)

	return r.deleteErr
}

func probeWithClock(storage s3.StorageItf, clock *time.Time) *storageProbe {
	p := newStorageProbe(storage)
	p.now = func() time.Time { return *clock }

	return p
}

func TestStorageProbeWritesAndRemovesTheSameObject(t *testing.T) {
	storage := &recordingStorage{}
	now := time.Now()

	require.NoError(t, probeWithClock(storage, &now).Ping(context.Background()))
	require.Equal(t, []string{storageProbeKey, storageProbeKey}, storage.keys)
	require.Equal(t, 1, storage.uploads)
	require.Equal(t, 1, storage.deletes)
}

func TestStorageProbeReusesAnAnswerForTheTTL(t *testing.T) {
	storage := &recordingStorage{}
	now := time.Now()
	p := probeWithClock(storage, &now)

	for range 5 {
		require.NoError(t, p.Ping(context.Background()))
	}

	require.Equal(t, 1, storage.uploads, "only the first call reached the bucket")

	now = now.Add(storageProbeTTL + time.Second)

	require.NoError(t, p.Ping(context.Background()))
	require.Equal(t, 2, storage.uploads, "an old answer is probed again")
}

func TestStorageProbeReportsAWriteThatFailsAndKeepsItForTheTTL(t *testing.T) {
	denied := errors.New("AccessDenied")
	storage := &recordingStorage{uploadErr: denied}
	now := time.Now()
	p := probeWithClock(storage, &now)

	require.ErrorIs(t, p.Ping(context.Background()), denied)
	require.ErrorIs(t, p.Ping(context.Background()), denied)
	require.Equal(t, 1, storage.uploads)
	require.Zero(t, storage.deletes, "nothing was stored, so nothing is removed")

	storage.uploadErr = nil
	now = now.Add(storageProbeTTL + time.Second)

	require.NoError(t, p.Ping(context.Background()), "a bucket that works again is picked up")
}

func TestStorageProbeIgnoresACallerThatGaveUp(t *testing.T) {
	// the storage honors the context of its call, so a probe that used the context of the caller would fail
	storage := &slowStorage{delay: 50 * time.Millisecond}
	now := time.Now()
	p := newStorageProbe(storage)
	p.now = func() time.Time { return now }

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.NoError(t, p.Ping(ctx), "a caller that left says nothing about the bucket")
	require.Equal(t, 1, storage.uploads, "the probe still ran")

	require.NoError(t, p.Ping(context.Background()))
	require.Equal(t, 1, storage.uploads, "and its answer is kept")
}

// slowStorage answers after a delay, and gives up when the context of the call ends first, like the client of a real bucket
type slowStorage struct {
	s3.Disabled

	delay   time.Duration
	uploads int
}

func (s *slowStorage) Upload(ctx context.Context, _ string, _ string, _ []byte) (string, error) {
	s.uploads++

	select {
	case <-time.After(s.delay):
		return "", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (s *slowStorage) Delete(context.Context, string) error { return nil }

func TestStorageProbeIsNotCutByTheShortLimitOfTheHealthCheck(t *testing.T) {
	storage := &slowStorage{delay: 100 * time.Millisecond}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	require.NoError(t, newStorageProbe(storage).Ping(ctx), "a bucket that needs longer than the caller allows is still up")
}

func TestStorageProbeStopsAtItsOwnLimitAndKeepsTheFailure(t *testing.T) {
	storage := &slowStorage{delay: time.Minute}
	now := time.Now()
	p := newStorageProbe(storage)
	p.limit = 20 * time.Millisecond
	p.now = func() time.Time { return now }

	require.ErrorIs(t, p.Ping(context.Background()), context.DeadlineExceeded)
	require.ErrorIs(t, p.Ping(context.Background()), context.DeadlineExceeded)
	require.Equal(t, 1, storage.uploads, "a bucket that is too slow is not probed again before the ttl")

	now = now.Add(storageProbeTTL + time.Second)

	require.ErrorIs(t, p.Ping(context.Background()), context.DeadlineExceeded)
	require.Equal(t, 2, storage.uploads)
}

func TestStorageProbeReportsADeleteThatFails(t *testing.T) {
	refused := errors.New("delete refused")
	storage := &recordingStorage{deleteErr: refused}
	now := time.Now()

	require.ErrorIs(t, probeWithClock(storage, &now).Ping(context.Background()), refused)
}
