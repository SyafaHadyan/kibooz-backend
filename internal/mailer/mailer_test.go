package mailer_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
	return mailer.New(&env.Env{ResendAPIURL: url, ResendAPIKey: key, EmailFrom: "no-reply@mail.example.org"})
}

func TestTheCodeGoesToResendInIndonesianWithTheKeyInTheHeaderOnly(t *testing.T) {
	server, seen := resendServer(t, http.StatusOK, `{"id":"abc"}`)

	err := sender(server.URL+"/", "re_testkey").SendVerificationCode(context.Background(), "parent@example.com", "012345", 15)
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
	require.Contains(t, text, "Halo,", "the mail names nobody, a name that a stranger typed would not reach someone else's inbox")

	for name, value := range map[string]string{"text": text, "html": page} {
		require.NotContains(t, value, "re_testkey", name)
	}
}

func TestTheSenderShowsTheNameOfTheServiceUnlessTheSettingHasOne(t *testing.T) {
	for from, shown := range map[string]string{
		"no-reply@mail.example.org":           "Kibooz <no-reply@mail.example.org>",
		"  no-reply@mail.example.org ":        "Kibooz <no-reply@mail.example.org>",
		"School Team <team@mail.example.org>": "School Team <team@mail.example.org>",
	} {
		server, seen := resendServer(t, http.StatusOK, `{}`)

		cfg := &env.Env{ResendAPIURL: server.URL, ResendAPIKey: "re_testkey", EmailFrom: from}
		require.NoError(t, mailer.New(cfg).SendVerificationCode(context.Background(), "a@example.com", "123456", 15))
		require.Equal(t, shown, seen.body["from"], from)
	}
}

func TestAnAnswerThatIsNotAnAcceptIsAnErrorWithoutTheKey(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusUnprocessableEntity, http.StatusInternalServerError} {
		server, _ := resendServer(t, status, `{"message":"daily quota reached"}`)

		err := sender(server.URL, "re_secretvalue").SendVerificationCode(context.Background(), "a@example.com", "123456", 15)
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

	err := sender(server.URL, "  ").SendVerificationCode(context.Background(), "a@example.com", "123456", 15)
	require.ErrorIs(t, err, mailer.ErrDisabled)
	require.False(t, called)
}

func TestAServerThatIsDownIsAnErrorWithoutTheKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL

	server.Close()

	err := sender(url, "re_secretvalue").SendVerificationCode(context.Background(), "a@example.com", "123456", 15)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "re_secretvalue")
}

func TestAQuotaAnswerIsToldApartFromOtherFailures(t *testing.T) {
	quota, _ := resendServer(t, http.StatusTooManyRequests, `{"message":"daily quota reached"}`)
	other, _ := resendServer(t, http.StatusInternalServerError, `{"message":"oops"}`)

	err := sender(quota.URL, "re_secretvalue").SendVerificationCode(context.Background(), "a@example.com", "123456", 15)
	require.ErrorIs(t, err, mailer.ErrQuota)

	err = sender(other.URL, "re_secretvalue").SendVerificationCode(context.Background(), "a@example.com", "123456", 15)
	require.Error(t, err)
	require.NotErrorIs(t, err, mailer.ErrQuota)
	rate, _ := resendServer(t, http.StatusTooManyRequests, `{"name":"rate_limit_exceeded","message":"Too many requests"}`)

	err = sender(rate.URL, "re_secretvalue").SendVerificationCode(context.Background(), "a@example.com", "123456", 15)
	require.ErrorIs(t, err, mailer.ErrRateLimit)
	require.NotErrorIs(t, err, mailer.ErrQuota)
}
