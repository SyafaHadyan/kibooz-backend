package s3_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/s3"
)

type request struct{ method, path string }

func storageFor(t *testing.T, status int) (s3.StorageItf, func() []request) {
	t.Helper()

	var (
		mu   sync.Mutex
		seen []request
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, request{r.Method, r.URL.Path})
		mu.Unlock()

		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)

	storage, err := s3.New(&env.Env{
		S3Endpoint: server.URL, S3Region: "us-east-1", S3BucketName: "bucket",
		S3AccessKeyID: "key", S3AccessKeySecret: "secret", S3PublicURL: "https://cdn.example.com",
	})
	require.NoError(t, err)

	return storage, func() []request {
		mu.Lock()
		defer mu.Unlock()

		return append([]request(nil), seen...)
	}
}

func TestDeleteRemovesTheObject(t *testing.T) {
	storage, requests := storageFor(t, http.StatusNoContent)

	require.NoError(t, storage.Delete(context.Background(), "trash-scans/abc/def.png"))
	require.Equal(t, []request{{http.MethodDelete, "/bucket/trash-scans/abc/def.png"}}, requests())
}

func TestDeleteReportsAStorageFailure(t *testing.T) {
	// a 403 is not retried by the SDK, so the test stays fast
	storage, _ := storageFor(t, http.StatusForbidden)

	err := storage.Delete(context.Background(), "avatars/abc/def.png")

	require.Error(t, err)
}

func TestDisabledStorageHasNothingToDelete(t *testing.T) {
	require.Error(t, s3.Disabled{}.Delete(context.Background(), "anything"))
}

type brokenStorage struct{ s3.Disabled }

func (brokenStorage) Delete(context.Context, string) error { return context.DeadlineExceeded }

func TestDiscardNeverPanicsOrBlocksOnFailure(t *testing.T) {
	s3.Discard(context.Background(), brokenStorage{}, "avatars/abc/def.png")
	s3.Discard(context.Background(), s3.Disabled{}, "avatars/abc/def.png")
}

func TestKeyFromURL(t *testing.T) {
	storage, _ := storageFor(t, http.StatusNoContent)

	tests := map[string]struct {
		url string
		key string
		ok  bool
	}{
		"own object":                     {"https://cdn.example.com/avatars/a/b.png", "avatars/a/b.png", true},
		"another host":                   {"https://other.example.com/avatars/a/b.png", "", false},
		"host that only starts the same": {"https://cdn.example.com.evil.test/avatars/a/b.png", "", false},
		"base url only":                  {"https://cdn.example.com/", "", false},
		"empty":                          {"", "", false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			key, ok := storage.KeyFromURL(tt.url)

			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.key, key)
		})
	}

	_, ok := s3.Disabled{}.KeyFromURL("https://cdn.example.com/avatars/a/b.png")
	require.False(t, ok)
}
