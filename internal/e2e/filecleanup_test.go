package e2e

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	userrepository "github.com/SyafaHadyan/kibooz-backend/internal/app/user/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
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

// noFiles is the file key function of a storage that holds nothing of ours
func noFiles(string) (string, bool) { return "", false }

func TestAccountDeletionQueuesItsFilesInTheSameTransaction(t *testing.T) {
	guru := registerGuru(t, "Seruni")
	prefix := "avatars/" + uuid.NewString()

	require.NoError(t, testDB.Exec("UPDATE users SET avatar_url = ? WHERE email = ?", "https://media.test/"+prefix+"/me.jpg", guru.Email).Error)

	var userID string
	require.NoError(t, testDB.Raw("SELECT id::text FROM users WHERE email = ?", guru.Email).Scan(&userID).Error)

	repo := userrepository.NewUserDB(testDB)
	fileKey := func(url string) (string, bool) { return strings.CutPrefix(url, "https://media.test/") }

	deleted, err := repo.SoftDeleteAccount(context.Background(), uuid.MustParse(userID), constants.RoleGuru, fileKey)
	require.NoError(t, err)
	require.Equal(t, []string{prefix + "/me.jpg"}, deleted.FileKeys)

	rows, attempts := queuedAttempts(t, prefix+"/me.jpg")
	require.Equal(t, 1, rows, "the key was queued together with the deletion")
	require.Zero(t, attempts)

	var avatar *string
	require.NoError(t, testDB.Raw("SELECT avatar_url FROM users WHERE id = ?::uuid", userID).Scan(&avatar).Error)
	require.Nil(t, avatar, "the address that pointed at the file is cleared")

	t.Run("a key leaves the queue once its file is gone", func(t *testing.T) {
		require.NoError(t, repo.UnqueueFileCleanups(context.Background(), deleted.FileKeys))
		require.NoError(t, repo.UnqueueFileCleanups(context.Background(), nil))

		rows, _ := queuedAttempts(t, prefix+"/me.jpg")
		require.Zero(t, rows)
	})
}

func TestFileCleanupSweep(t *testing.T) {
	prefix := "avatars/" + uuid.NewString()
	fine, stuck := prefix+"/fine.jpg", prefix+"/stuck.jpg"

	require.NoError(t, testDB.Exec("INSERT INTO file_cleanups (object_key) VALUES (?), (?)", fine, stuck).Error)

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
