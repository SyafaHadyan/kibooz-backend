package bootstrap

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/infra/s3"
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

func TestStorageProbeReportsADeleteThatFails(t *testing.T) {
	refused := errors.New("delete refused")
	storage := &recordingStorage{deleteErr: refused}
	now := time.Now()

	require.ErrorIs(t, probeWithClock(storage, &now).Ping(context.Background()), refused)
}
