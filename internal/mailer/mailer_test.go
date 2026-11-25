package mailer_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/mailer"
)

type captured struct {
	method string
	path   string
	auth   string
	body   map[string]any
}

func resendServer(t *testing.T, status int, answer string) (*httptest.Server, *captured) {
	t.Helper()

	seen := &captured{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, &seen.body))

		seen.method, seen.path, seen.auth = r.Method, r.URL.Path, r.Header.Get("Authorization")

		w.WriteHeader(status)
		_, _ = w.Write([]byte(answer))
	}))

	t.Cleanup(server.Close)

	return server, seen
}

func sender(url string, key string) *mailer.Resend {
	return mailer.New(&env.Env{ResendAPIURL: url, ResendAPIKey: key, EmailFrom: "Kibooz <no-reply@mail.example.org>"})
}

func TestTheCodeGoesToResendInIndonesianWithTheKeyInTheHeaderOnly(t *testing.T) {
	server, seen := resendServer(t, http.StatusOK, `{"id":"abc"}`)

	err := sender(server.URL+"/", "re_testkey").SendVerificationCode(context.Background(), "parent@example.com", "Ibu <b>Ani</b>", "012345", 15)
	require.NoError(t, err)

	require.Equal(t, http.MethodPost, seen.method)
	require.Equal(t, "/emails", seen.path, "a slash at the end of the address does not double it")
	require.Equal(t, "Bearer re_testkey", seen.auth)
	require.Equal(t, "Kibooz <no-reply@mail.example.org>", seen.body["from"])
	require.Equal(t, []any{"parent@example.com"}, seen.body["to"])
	require.Equal(t, "Kode verifikasi Kibooz Anda", seen.body["subject"])

	text, _ := seen.body["text"].(string)
	page, _ := seen.body["html"].(string)

	require.Contains(t, text, "012345", "leading zeros are kept")
	require.Contains(t, text, "15 menit")
	require.Contains(t, page, "012345")
	require.Contains(t, page, "Ibu &lt;b&gt;Ani&lt;/b&gt;", "a name cannot add markup to the mail")
	require.NotContains(t, page, "<b>Ani</b>")

	for name, value := range map[string]string{"text": text, "html": page} {
		require.NotContains(t, value, "re_testkey", name)
	}
}

func TestAnAnswerThatIsNotAnAcceptIsAnErrorWithoutTheKey(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusUnprocessableEntity, http.StatusInternalServerError} {
		server, _ := resendServer(t, status, `{"message":"daily quota reached"}`)

		err := sender(server.URL, "re_secretvalue").SendVerificationCode(context.Background(), "a@example.com", "A", "123456", 15)
		require.Error(t, err)
		require.Contains(t, err.Error(), "daily quota reached")
		require.NotContains(t, err.Error(), "re_secretvalue")
		require.NotContains(t, err.Error(), "123456", "the code is not in an error that is logged")
	}
}

func TestWithoutAKeyNothingIsSent(t *testing.T) {
	called := false

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	t.Cleanup(server.Close)

	err := sender(server.URL, "  ").SendVerificationCode(context.Background(), "a@example.com", "A", "123456", 15)
	require.ErrorIs(t, err, mailer.ErrDisabled)
	require.False(t, called)
}

func TestAServerThatIsDownIsAnErrorWithoutTheKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL

	server.Close()

	err := sender(url, "re_secretvalue").SendVerificationCode(context.Background(), "a@example.com", "A", "123456", 15)
	require.Error(t, err)
	require.False(t, strings.Contains(err.Error(), "re_secretvalue"))
}

func TestAnEmptyNameFallsBackToAGreeting(t *testing.T) {
	server, seen := resendServer(t, http.StatusOK, `{}`)

	require.NoError(t, sender(server.URL, "re_testkey").SendVerificationCode(context.Background(), "a@example.com", "   ", "123456", 15))
	require.Contains(t, seen.body["text"], "Halo Pengguna")
}
