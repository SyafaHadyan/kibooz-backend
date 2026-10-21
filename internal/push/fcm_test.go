package push

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeGoogle answers the token endpoint and the FCM endpoint of one test, and records what the FCM endpoint received
type fakeGoogle struct {
	server *httptest.Server

	mu        sync.Mutex
	status    int
	body      string
	received  []map[string]any
	headers   []http.Header
	tokenCall int
}

func newFakeGoogle(t *testing.T) *fakeGoogle {
	t.Helper()

	f := &fakeGoogle{status: http.StatusOK, body: `{"name":"projects/p/messages/1"}`}

	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		f.tokenCall++
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access-123","token_type":"Bearer","expires_in":3600}`))
	})
	mux.HandleFunc("/v1/projects/kibooz-test/messages:send", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)

		var parsed map[string]any

		_ = json.Unmarshal(raw, &parsed)

		f.mu.Lock()
		f.received = append(f.received, parsed)
		f.headers = append(f.headers, r.Header.Clone())
		status, body := f.status, f.body
		f.mu.Unlock()

		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})

	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)

	return f
}

// keyFile writes a service account key that points at the fake token endpoint. The private key is made for the test, so no
// secret is stored in the repository.
func keyFile(t *testing.T, tokenURI string, mutate func(map[string]string)) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	pemKey := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: mustPKCS8(t, key)})

	fields := map[string]string{
		"type": "service_account", "project_id": "kibooz-test", "client_email": "push@kibooz-test.iam.example.test",
		"private_key": string(pemKey), "token_uri": tokenURI,
	}

	if mutate != nil {
		mutate(fields)
	}

	data, err := json.Marshal(fields)
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "firebase.json")
	require.NoError(t, os.WriteFile(path, data, 0o600))

	return path
}

func mustPKCS8(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()

	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)

	return der
}

func newClient(t *testing.T, google *fakeGoogle) *FCM {
	t.Helper()

	client, err := NewFCM(keyFile(t, google.server.URL+"/token", nil))
	require.NoError(t, err)

	client.endpoint = google.server.URL + "/v1/projects/kibooz-test/messages:send"

	return client
}

func TestAWakeUpCarriesOnlyIdsAndAsksForHighPriority(t *testing.T) {
	google := newFakeGoogle(t)
	client := newClient(t, google)

	require.NoError(t, client.Send(context.Background(), Message{Token: "device-token", ConversationID: "conv-1", MessageID: 42}))

	require.Len(t, google.received, 1)
	require.Equal(t, "Bearer access-123", google.headers[0].Get("Authorization"))

	message := google.received[0]["message"].(map[string]any)

	require.Equal(t, "device-token", message["token"])
	require.Equal(t, map[string]any{"conversationId": "conv-1", "messageId": "42"}, message["data"])
	require.NotContains(t, message, "notification", "no text is sent, the app builds the notification")

	android := message["android"].(map[string]any)

	require.Equal(t, "high", android["priority"])
	require.Equal(t, "conversation-conv-1", android["collapse_key"])
	require.Equal(t, "86400s", android["ttl"])
}

func TestTheAccessTokenIsReusedBetweenWakeUps(t *testing.T) {
	google := newFakeGoogle(t)
	client := newClient(t, google)

	for range 3 {
		require.NoError(t, client.Send(context.Background(), Message{Token: "t", ConversationID: "c", MessageID: 1}))
	}

	require.Equal(t, 1, google.tokenCall, "the token lives for an hour")
}

func TestATokenThatFirebaseDoesNotKnowIsReportedAsUnregistered(t *testing.T) {
	google := newFakeGoogle(t)
	client := newClient(t, google)

	google.status, google.body = http.StatusNotFound, `{"error":{"code":404,"status":"NOT_FOUND","message":"Requested entity was not found.","details":[{"errorCode":"UNREGISTERED"}]}}`
	require.ErrorIs(t, client.Send(context.Background(), Message{Token: "gone"}), ErrUnregistered)

	google.status, google.body = http.StatusBadRequest, `{"error":{"status":"INVALID_ARGUMENT","details":[{"errorCode":"UNREGISTERED"}]}}`
	require.ErrorIs(t, client.Send(context.Background(), Message{Token: "gone"}), ErrUnregistered)

	// a 404 without the code can be a wrong project or endpoint, which must never delete a good token
	for _, body := range []string{``, `{"error":{"code":404,"status":"NOT_FOUND","message":"Requested entity was not found."}}`} {
		google.status, google.body = http.StatusNotFound, body

		err := client.Send(context.Background(), Message{Token: "fine"})
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrUnregistered)
	}
}

func TestAnotherFailureIsAnErrorAndKeepsTheToken(t *testing.T) {
	google := newFakeGoogle(t)
	client := newClient(t, google)

	google.status, google.body = http.StatusServiceUnavailable, `{"error":{"status":"UNAVAILABLE","message":"try later"}}`

	err := client.Send(context.Background(), Message{Token: "t"})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrUnregistered)
	require.Contains(t, err.Error(), "503")
	require.Contains(t, err.Error(), "UNAVAILABLE")
}

func TestASendThatCannotGetAnAccessTokenFails(t *testing.T) {
	google := newFakeGoogle(t)
	client, err := NewFCM(keyFile(t, google.server.URL+"/missing", nil))
	require.NoError(t, err)

	client.endpoint = google.server.URL + "/v1/projects/kibooz-test/messages:send"

	require.Error(t, client.Send(context.Background(), Message{Token: "t"}))
	require.Empty(t, google.received)
}

func TestAKeyFileThatIsNotAServiceAccountKeyIsRefused(t *testing.T) {
	_, err := NewFCM(filepath.Join(t.TempDir(), "missing.json"))
	require.ErrorContains(t, err, "read the Firebase credentials file")

	notJSON := filepath.Join(t.TempDir(), "bad.json")
	require.NoError(t, os.WriteFile(notJSON, []byte("not json"), 0o600))

	_, err = NewFCM(notJSON)
	require.ErrorContains(t, err, "not valid JSON")

	for _, field := range []string{"project_id", "client_email", "private_key"} {
		path := keyFile(t, "https://token.example.test", func(fields map[string]string) { delete(fields, field) })

		_, err = NewFCM(path)
		require.ErrorContains(t, err, "not a service account key", field)
	}

	// without a token address the one of Google is used
	path := keyFile(t, "", func(fields map[string]string) { delete(fields, "token_uri") })

	client, err := NewFCM(path)
	require.NoError(t, err)
	require.Contains(t, client.endpoint, "kibooz-test")
}
