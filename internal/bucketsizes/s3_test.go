package bucketsizes

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeStore answers ListObjectsV2 for one bucket in pages of two objects, the way an S3 compatible store does, and refuses other buckets
type fakeStore struct {
	mu     sync.Mutex
	paths  []string
	tokens []string
	deny   bool
}

func (f *fakeStore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.paths = append(f.paths, r.URL.Path)
	f.tokens = append(f.tokens, r.URL.Query().Get("continuation-token"))

	if f.deny || r.URL.Path != "/media" {
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>AccessDenied</Code><Message>no</Message></Error>`)

		return
	}

	pages := map[string]string{
		"": `<Contents><Key>a</Key><Size>100</Size></Contents><Contents><Key>b</Key><Size>200</Size></Contents>
			<IsTruncated>true</IsTruncated><NextContinuationToken>second</NextContinuationToken>`,
		"second": `<Contents><Key>c</Key><Size>300</Size></Contents><Contents><Key>d</Key><Size>0</Size></Contents>
			<IsTruncated>true</IsTruncated><NextContinuationToken>third</NextContinuationToken>`,
		"third": `<Contents><Key>e</Key><Size>4000</Size></Contents><IsTruncated>false</IsTruncated>`,
	}

	page, known := pages[r.URL.Query().Get("continuation-token")]
	if !known {
		w.WriteHeader(http.StatusBadRequest)

		return
	}

	w.Header().Set("Content-Type", "application/xml")
	_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
		<Name>media</Name><MaxKeys>1000</MaxKeys>%s</ListBucketResult>`, page)
}

func listerFor(t *testing.T, store http.Handler) *S3Lister {
	t.Helper()

	server := httptest.NewServer(store)
	t.Cleanup(server.Close)

	lister, err := NewS3Lister(t.Context(), S3Settings{
		Endpoint: server.URL, Region: "auto", AccessKeyID: "id", AccessKeySecret: "secret", PathStyle: true,
	})
	require.NoError(t, err)

	return lister
}

func TestTotalsAddsUpEveryPageOfTheListing(t *testing.T) {
	store := &fakeStore{}

	totals, err := listerFor(t, store).Totals(t.Context(), "media")

	require.NoError(t, err)
	require.Equal(t, Totals{Bytes: 4600, Objects: 5}, totals)
	require.Equal(t, []string{"", "second", "third"}, store.tokens, "each page is asked for with the token of the one before")

	for _, path := range store.paths {
		require.Equal(t, "/media", path, "the bucket is in the path")
	}
}

func TestTotalsStopsAtTheFirstErrorAndDoesNotAnswerAHalfCount(t *testing.T) {
	lister := listerFor(t, &fakeStore{})

	totals, err := lister.Totals(t.Context(), "other")

	require.Error(t, err)
	require.Contains(t, err.Error(), "other")
	require.Equal(t, Totals{}, totals)

	_, err = listerFor(t, &fakeStore{deny: true}).Totals(t.Context(), "media")
	require.Error(t, err)
}

func TestNewS3ListerNeedsTheEndpointAndTheKey(t *testing.T) {
	for name, settings := range map[string]S3Settings{
		"no endpoint": {AccessKeyID: "id", AccessKeySecret: "secret"},
		"no key id":   {Endpoint: "https://store.example.test", AccessKeySecret: "secret"},
		"no secret":   {Endpoint: "https://store.example.test", AccessKeyID: "id"},
	} {
		_, err := NewS3Lister(t.Context(), settings)
		require.Error(t, err, name)
	}

	lister, err := NewS3Lister(t.Context(), S3Settings{Endpoint: "https://store.example.test", Region: "eu-west-1", AccessKeyID: "id", AccessKeySecret: "secret"})
	require.NoError(t, err)
	require.NotNil(t, lister)
}
