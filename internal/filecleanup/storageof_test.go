package filecleanup

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/infra/s3"
)

// bucket tells the buckets apart in a test
type bucket struct {
	s3.Disabled

	name string
}

func TestTheKeysOfTheChatMediaAndItsPendingUploadsAreInThePrivateBucket(t *testing.T) {
	sweeper := New(nil, &bucket{name: "public"}).WithPrivate(&bucket{name: "private"})

	for key, want := range map[string]string{
		"chat/room/user/file.jpg":         "private",
		"pending/chat/room/user/file.jpg": "private",
		"avatars/user/file.jpg":           "public",
		"pending/videos/class/file.mp4":   "public",
	} {
		require.Equal(t, want, sweeper.storageOf(key).(*bucket).name, key)
	}
}

func TestPlainGivesBackTheStorageThatWasWrapped(t *testing.T) {
	inner := &bucket{name: "inner"}

	require.Same(t, inner, Plain(RetryFailedDeletes(inner, nil)))
	require.Same(t, inner, Plain(inner), "a storage that was not wrapped stays as it is")
}
