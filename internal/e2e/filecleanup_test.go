package e2e

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	userrepository "github.com/SyafaHadyan/kibooz-backend/internal/app/user/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/filecleanup"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/s3"
)

// bucketStub deletes what it is asked to, except the keys it is told to refuse
type bucketStub struct {
	s3.Disabled

	mu      sync.Mutex
	refused map[string]bool
	removed []string
}

func (b *bucketStub) Delete(_ context.Context, key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.refused[key] {
		return errors.New("bucket refused the delete")
	}

	b.removed = append(b.removed, key)

	return nil
}

func queuedAttempts(t *testing.T, key string) (rows int, attempts int) {
	t.Helper()

	var found []int
	require.NoError(t, testDB.Raw("SELECT attempts FROM file_cleanups WHERE object_key = ?", key).Scan(&found).Error)

	if len(found) == 0 {
		return 0, 0
	}

	return len(found), found[0]
}

func TestFileCleanupSweep(t *testing.T) {
	prefix := "avatars/" + uuid.NewString()
	fine, stuck := prefix+"/fine.jpg", prefix+"/stuck.jpg"

	repo := userrepository.NewUserDB(testDB)
	require.NoError(t, repo.QueueFileCleanups(context.Background(), []string{fine, stuck}))

	t.Run("queueing a key twice keeps one row", func(t *testing.T) {
		require.NoError(t, repo.QueueFileCleanups(context.Background(), []string{fine}))
		require.NoError(t, repo.QueueFileCleanups(context.Background(), nil))

		rows, attempts := queuedAttempts(t, fine)
		require.Equal(t, 1, rows)
		require.Zero(t, attempts)
	})

	bucket := &bucketStub{refused: map[string]bool{stuck: true}}
	sweeper := filecleanup.New(testDB, bucket)

	t.Run("a sweep removes what the bucket deletes and counts the failures", func(t *testing.T) {
		removed, err := sweeper.Sweep(context.Background())
		require.NoError(t, err)
		require.Equal(t, 1, removed)
		require.Contains(t, bucket.removed, fine)

		rows, _ := queuedAttempts(t, fine)
		require.Zero(t, rows, "the deleted file left the queue")

		rows, attempts := queuedAttempts(t, stuck)
		require.Equal(t, 1, rows)
		require.Equal(t, 1, attempts, fmt.Sprintf("%s stays with its attempt counted", stuck))
	})

	t.Run("the file is removed once the bucket works again", func(t *testing.T) {
		bucket.refused = nil

		removed, err := sweeper.Sweep(context.Background())
		require.NoError(t, err)
		require.Equal(t, 1, removed)

		rows, _ := queuedAttempts(t, stuck)
		require.Zero(t, rows)
	})
}
