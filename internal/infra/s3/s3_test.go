package s3_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

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

func TestPublicURL(t *testing.T) {
	storage, _ := storageFor(t, http.StatusNoContent)

	require.Equal(t, "https://cdn.example.com/videos/a/b.mp4", storage.PublicURL("videos/a/b.mp4"))
	require.Empty(t, s3.Disabled{}.PublicURL("videos/a/b.mp4"))
}

func TestPresignUploadSignsTheTypeAndTheLength(t *testing.T) {
	storage, requests := storageFor(t, http.StatusNoContent)

	signed, err := storage.PresignUpload(context.Background(), "videos/a/b.mp4", "video/mp4", 1234, 15*time.Minute)
	require.NoError(t, err)

	parsed, err := url.Parse(signed)
	require.NoError(t, err)

	query := parsed.Query()

	require.Equal(t, "/bucket/videos/a/b.mp4", parsed.Path)
	require.Equal(t, "900", query.Get("X-Amz-Expires"))
	require.NotEmpty(t, query.Get("X-Amz-Signature"))
	require.Contains(t, query.Get("X-Amz-SignedHeaders"), "content-type")
	require.Contains(t, query.Get("X-Amz-SignedHeaders"), "content-length")
	require.Empty(t, requests(), "signing never calls the bucket")
}

func TestDisabledStorageSignsNothing(t *testing.T) {
	_, err := s3.Disabled{}.PresignUpload(context.Background(), "videos/a/b.mp4", "video/mp4", 1, time.Minute)
	require.Error(t, err)

	_, err = s3.Disabled{}.Stat(context.Background(), "videos/a/b.mp4")
	require.Error(t, err)
}

func TestStat(t *testing.T) {
	tests := map[string]struct {
		status  int
		want    *s3.Object
		wantErr bool
	}{
		"an object":         {http.StatusOK, &s3.Object{Size: 2048, ContentType: "video/mp4"}, false},
		"a missing object":  {http.StatusNotFound, nil, false},
		"a refused request": {http.StatusForbidden, nil, true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tt.status == http.StatusOK {
					w.Header().Set("Content-Type", "video/mp4")
					w.Header().Set("Content-Length", "2048")
				}

				w.WriteHeader(tt.status)
			}))
			t.Cleanup(server.Close)

			storage, err := s3.New(&env.Env{
				S3Endpoint: server.URL, S3Region: "us-east-1", S3BucketName: "bucket",
				S3AccessKeyID: "key", S3AccessKeySecret: "secret", S3PublicURL: "https://cdn.example.com",
			})
			require.NoError(t, err)

			got, err := storage.Stat(context.Background(), "videos/a/b.mp4")

			require.Equal(t, tt.wantErr, err != nil)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestCopy(t *testing.T) {
	tests := map[string]struct {
		status  int
		wantErr bool
	}{
		"a copied object":   {http.StatusOK, false},
		"a refused request": {http.StatusForbidden, true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var (
				mu   sync.Mutex
				seen *http.Request
			)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				seen = r
				mu.Unlock()

				w.WriteHeader(tt.status)

				if tt.status == http.StatusOK {
					_, _ = w.Write([]byte(`<CopyObjectResult><ETag>"copied"</ETag></CopyObjectResult>`))
				}
			}))
			t.Cleanup(server.Close)

			storage, err := s3.New(&env.Env{
				S3Endpoint: server.URL, S3Region: "us-east-1", S3BucketName: "bucket",
				S3AccessKeyID: "key", S3AccessKeySecret: "secret", S3PublicURL: "https://cdn.example.com",
			})
			require.NoError(t, err)

			err = storage.Copy(context.Background(), "pending/videos/a/b.mp4", "videos/a/b.mp4", "video/mp4")

			mu.Lock()
			defer mu.Unlock()

			require.Equal(t, tt.wantErr, err != nil)
			require.NotNil(t, seen)
			require.Equal(t, http.MethodPut, seen.Method)
			require.Equal(t, "/bucket/videos/a/b.mp4", seen.URL.Path)
			require.Equal(t, "bucket/pending/videos/a/b.mp4", seen.Header.Get("X-Amz-Copy-Source"))
			require.Equal(t, "REPLACE", seen.Header.Get("X-Amz-Metadata-Directive"))
			require.Equal(t, "video/mp4", seen.Header.Get("Content-Type"))
			require.Contains(t, seen.Header.Get("Cache-Control"), "immutable")
		})
	}
}

func TestDisabledStorageCopiesNothing(t *testing.T) {
	require.Error(t, s3.Disabled{}.Copy(context.Background(), "pending/a.mp4", "videos/a.mp4", "video/mp4"))
}
