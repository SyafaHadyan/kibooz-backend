package e2e

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	pushrepository "github.com/SyafaHadyan/kibooz-backend/internal/app/push/repository"
	"github.com/SyafaHadyan/kibooz-backend/internal/bootstrap"
)

// The sender reads the tokens of the people who should be woken and forgets the ones that Firebase refused
func TestPushTokensAreReadAndForgottenByTheSender(t *testing.T) {
	guru := registerGuru(t, "Pushsender")
	_, code := classOf(t, guru)
	parent, _ := registerWali(t, code, "Sender Child")

	guruToken, parentToken := pushToken("sg"), pushToken("sp")

	require.Equal(t, http.StatusOK, call(t, http.MethodPost, pushBase, guru.Token, map[string]any{"token": guruToken}).Status)
	require.Equal(t, http.StatusOK, call(t, http.MethodPost, pushBase, parent.Token, map[string]any{"token": parentToken}).Status)

	var guruID, parentID string

	require.NoError(t, testDB.Raw("SELECT id::text FROM users WHERE email = ?", guru.Email).Scan(&guruID).Error)
	require.NoError(t, testDB.Raw("SELECT id::text FROM users WHERE email = ?", parent.Email).Scan(&parentID).Error)

	repo := pushrepository.NewPushDB(testDB)
	ctx := context.Background()

	none, err := repo.TokensOf(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, none, "nobody to wake, nothing to read")

	rows, err := repo.TokensOf(ctx, []uuid.UUID{uuid.MustParse(guruID), uuid.MustParse(parentID)})
	require.NoError(t, err)
	require.Len(t, rows, 2)

	found := map[string]string{}
	for _, row := range rows {
		found[row.Token] = row.UserID.String()
	}

	require.Equal(t, map[string]string{guruToken: guruID, parentToken: parentID}, found)

	only, err := repo.TokensOf(ctx, []uuid.UUID{uuid.MustParse(parentID)})
	require.NoError(t, err)
	require.Len(t, only, 1, "only the people who were asked for")

	require.NoError(t, repo.DeleteTokenValues(ctx, nil), "forgetting nothing is not an error")
	require.EqualValues(t, 1, tokenCount(t, "token = ?", guruToken))

	require.NoError(t, repo.DeleteTokenValues(ctx, []string{guruToken, parentToken}))
	require.Zero(t, tokenCount(t, "token IN ?", []string{guruToken, parentToken}))
}

// firebaseKey writes a service account key that was made for the test, so no secret is stored in the repository
func firebaseKey(t *testing.T) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)

	data, err := json.Marshal(map[string]string{
		"type": "service_account", "project_id": "kibooz-test", "client_email": "push@kibooz-test.iam.example.test",
		"private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"token_uri":   "https://oauth2.example.test/token",
	})
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "firebase.json")
	require.NoError(t, os.WriteFile(path, data, 0o600))

	return path
}

// A key that cannot be read leaves the pushes off and the API up, and a good key starts the sender and stops it with the API
func TestTheAPIStartsWithOrWithoutAUsableFirebaseKey(t *testing.T) {
	app(t) // makes sure the shared environment defaults are set

	t.Run("a key that cannot be read", func(t *testing.T) {
		t.Setenv("FIREBASE_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "missing.json"))

		started, err := bootstrap.Start("e2e-push-off")
		require.NoError(t, err)

		t.Cleanup(started.Close)

		health := callApp(t, started.App.Fiber, http.MethodGet, "/healthz", "", nil)
		require.Equal(t, http.StatusOK, health.Status, "body %v", health.Body)
	})

	t.Run("a usable key", func(t *testing.T) {
		t.Setenv("FIREBASE_CREDENTIALS_FILE", firebaseKey(t))

		started, err := bootstrap.Start("e2e-push-on")
		require.NoError(t, err)

		t.Cleanup(started.Close)

		health := callApp(t, started.App.Fiber, http.MethodGet, "/healthz", "", nil)
		require.Equal(t, http.StatusOK, health.Status, "body %v", health.Body)
	})
}
