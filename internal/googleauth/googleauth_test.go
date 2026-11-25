package googleauth_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/googleauth"
)

const testClientID = "test-web-client-id.example.test"

type rsaSigner struct {
	kid     string
	private *rsa.PrivateKey
}

func newSigner(t *testing.T, kid string) rsaSigner {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	return rsaSigner{kid: kid, private: key}
}

func (s rsaSigner) jwk() map[string]any {
	pub := &s.private.PublicKey

	return map[string]any{
		"kty": "RSA",
		"alg": "RS256",
		"use": "sig",
		"kid": s.kid,
		"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}
}

func (s rsaSigner) sign(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = s.kid

	signed, err := token.SignedString(s.private)
	require.NoError(t, err)

	return signed
}

func TestJWKSClientVerifiesValidGoogleTokensAndCachesKeys(t *testing.T) {
	signer := newSigner(t, "kid-1")

	var hits atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Cache-Control", "public, max-age=120")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{signer.jwk()}})
	}))
	t.Cleanup(server.Close)

	now := time.Unix(1_760_000_000, 0)
	verifier := googleauth.NewVerifier(
		[]string{testClientID},
		googleauth.WithJWKSURL(server.URL),
		googleauth.WithNow(func() time.Time { return now }),
	)

	for _, issuer := range []string{"https://accounts.google.com", "accounts.google.com"} {
		token := signer.sign(t, jwt.MapClaims{
			"iss":            issuer,
			"aud":            testClientID,
			"sub":            "110248495921234",
			"email":          "  Guru.Sari@Gmail.com ",
			"email_verified": true,
			"name":           "  Ibu Sari ",
			"picture":        "https://lh3.googleusercontent.com/a/avatar",
			"exp":            now.Add(10 * time.Minute).Unix(),
			"iat":            now.Unix(),
		})

		claims, err := verifier.Verify(context.Background(), token)
		require.NoError(t, err)
		require.Equal(t, "110248495921234", claims.Subject)
		require.Equal(t, "guru.sari@gmail.com", claims.Email)
		require.True(t, claims.EmailVerified)
		require.Equal(t, "Ibu Sari", claims.FullName)
		require.Equal(t, "https://lh3.googleusercontent.com/a/avatar", claims.AvatarURL)
	}

	require.Equal(t, int32(1), hits.Load(), "second verify should reuse cached JWKS")
}

func TestJWKSClientRefreshesRotatedKeys(t *testing.T) {
	first := newSigner(t, "kid-old")
	second := newSigner(t, "kid-new")

	var current atomic.Value
	current.Store(first)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		active := current.Load().(rsaSigner)
		w.Header().Set("Cache-Control", "max-age=3600")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{active.jwk()}})
	}))
	t.Cleanup(server.Close)

	now := time.Unix(1_760_000_000, 0)
	verifier := googleauth.NewVerifier(
		[]string{testClientID},
		googleauth.WithJWKSURL(server.URL),
		googleauth.WithNow(func() time.Time { return now }),
	)

	token1 := first.sign(t, jwt.MapClaims{
		"iss":            "https://accounts.google.com",
		"aud":            testClientID,
		"sub":            "sub-1",
		"email":          "a@example.com",
		"email_verified": true,
		"exp":            now.Add(10 * time.Minute).Unix(),
	})

	_, err := verifier.Verify(context.Background(), token1)
	require.NoError(t, err)

	current.Store(second)
	now = now.Add(10 * time.Second)

	token2 := second.sign(t, jwt.MapClaims{
		"iss":            "https://accounts.google.com",
		"aud":            testClientID,
		"sub":            "sub-1",
		"email":          "a@example.com",
		"email_verified": true,
		"exp":            now.Add(10 * time.Minute).Unix(),
	})

	claims, err := verifier.Verify(context.Background(), token2)
	require.NoError(t, err)
	require.Equal(t, "sub-1", claims.Subject)
}

func TestJWKSClientRejectsInvalidOrUnconfiguredTokens(t *testing.T) {
	unconfigured := googleauth.NewVerifier(nil)
	_, err := unconfigured.Verify(context.Background(), "anything")
	require.ErrorIs(t, err, googleauth.ErrNotConfigured)

	signer := newSigner(t, "kid-1")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{signer.jwk()}})
	}))
	t.Cleanup(server.Close)

	now := time.Unix(1_760_000_000, 0)
	verifier := googleauth.NewVerifier(
		[]string{testClientID},
		googleauth.WithJWKSURL(server.URL),
		googleauth.WithNow(func() time.Time { return now }),
	)

	hsToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": "https://accounts.google.com", "aud": testClientID, "sub": "sub-1", "email": "a@example.com", "email_verified": true,
		"exp": now.Add(5 * time.Minute).Unix(),
	})
	hsSigned, err := hsToken.SignedString(bytes.Repeat([]byte("k"), 32))
	require.NoError(t, err)

	cases := map[string]string{
		"empty":         "",
		"malformed":     "not.a.jwt",
		"hs256":         hsSigned,
		"wrong_aud":     signer.sign(t, jwt.MapClaims{"iss": "https://accounts.google.com", "aud": "other-app", "sub": "1", "email": "a@example.com", "email_verified": true, "exp": now.Add(5 * time.Minute).Unix()}),
		"wrong_iss":     signer.sign(t, jwt.MapClaims{"iss": "https://evil.example", "aud": testClientID, "sub": "1", "email": "a@example.com", "email_verified": true, "exp": now.Add(5 * time.Minute).Unix()}),
		"unverified":    signer.sign(t, jwt.MapClaims{"iss": "https://accounts.google.com", "aud": testClientID, "sub": "1", "email": "a@example.com", "email_verified": false, "exp": now.Add(5 * time.Minute).Unix()}),
		"missing_email": signer.sign(t, jwt.MapClaims{"iss": "https://accounts.google.com", "aud": testClientID, "sub": "1", "email": "", "email_verified": true, "exp": now.Add(5 * time.Minute).Unix()}),
		"missing_sub":   signer.sign(t, jwt.MapClaims{"iss": "https://accounts.google.com", "aud": testClientID, "sub": "", "email": "a@example.com", "email_verified": true, "exp": now.Add(5 * time.Minute).Unix()}),
		"expired":       signer.sign(t, jwt.MapClaims{"iss": "https://accounts.google.com", "aud": testClientID, "sub": "1", "email": "a@example.com", "email_verified": true, "exp": now.Add(-2 * time.Minute).Unix()}),
	}

	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			_, verifyErr := verifier.Verify(context.Background(), token)
			require.ErrorIs(t, verifyErr, googleauth.ErrInvalidToken)
		})
	}
}

func TestJWKSClientReturnsFetchErrorOnUpstreamFailure(t *testing.T) {
	signer := newSigner(t, "kid-1")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	now := time.Unix(1_760_000_000, 0)
	verifier := googleauth.NewVerifier([]string{testClientID}, googleauth.WithJWKSURL(server.URL))
	token := signer.sign(t, jwt.MapClaims{
		"iss": "https://accounts.google.com", "aud": testClientID, "sub": "1", "email": "a@example.com", "email_verified": true,
		"exp": now.Add(5 * time.Minute).Unix(),
	})

	_, err := verifier.Verify(context.Background(), token)
	require.Error(t, err)
	require.NotErrorIs(t, err, googleauth.ErrInvalidToken)
	require.NotErrorIs(t, err, googleauth.ErrNotConfigured)
}

func TestSwitchableVerifierDelegatesSafely(t *testing.T) {
	sw := googleauth.NewSwitchableVerifier(nil)
	_, err := sw.Verify(context.Background(), "assertion-sample")
	require.ErrorIs(t, err, googleauth.ErrNotConfigured)

	expected := googleauth.Claims{Subject: "sub-99", Email: "user@example.com", EmailVerified: true}
	sw.Set(verifierFunc(func(context.Context, string) (googleauth.Claims, error) {
		return expected, nil
	}))

	got, err := sw.Verify(context.Background(), "assertion-sample")
	require.NoError(t, err)
	require.Equal(t, expected, got)
}

type verifierFunc func(context.Context, string) (googleauth.Claims, error)

func (f verifierFunc) Verify(ctx context.Context, idToken string) (googleauth.Claims, error) {
	return f(ctx, idToken)
}

func TestClaimsFreshWithin(t *testing.T) {
	now := time.Unix(1_760_000_000, 0)
	window := 5 * time.Minute

	cases := map[string]struct {
		issued time.Time
		fresh  bool
	}{
		"issued a minute ago":                {now.Add(-time.Minute), true},
		"issued exactly at the edge":         {now.Add(-window), true},
		"issued a second past the window":    {now.Add(-window - time.Second), false},
		"issued a little ahead of the clock": {now.Add(10 * time.Second), true},
		"issued far ahead of the clock":      {now.Add(10 * time.Minute), false},
		"no issue time":                      {time.Time{}, false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.fresh, googleauth.Claims{IssuedAt: tc.issued}.FreshWithin(now, window))
		})
	}
}

func TestJWKSClientReadsTheIssueTime(t *testing.T) {
	signer := newSigner(t, "kid-iat")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{signer.jwk()}})
	}))
	t.Cleanup(server.Close)

	now := time.Unix(1_760_000_000, 0)
	verifier := googleauth.NewVerifier(
		[]string{testClientID}, googleauth.WithJWKSURL(server.URL), googleauth.WithNow(func() time.Time { return now }),
	)

	token := func(extra jwt.MapClaims) string {
		claims := jwt.MapClaims{
			"iss": "https://accounts.google.com", "aud": testClientID, "sub": "110248495921234",
			"email": "a@example.test", "email_verified": true, "exp": now.Add(10 * time.Minute).Unix(),
		}
		for key, value := range extra {
			claims[key] = value
		}

		return signer.sign(t, claims)
	}

	claims, err := verifier.Verify(context.Background(), token(jwt.MapClaims{"iat": now.Add(-2 * time.Minute).Unix()}))
	require.NoError(t, err)
	require.True(t, claims.FreshWithin(now, 5*time.Minute))

	claims, err = verifier.Verify(context.Background(), token(nil))
	require.NoError(t, err)
	require.True(t, claims.IssuedAt.IsZero())
	require.False(t, claims.FreshWithin(now, 5*time.Minute))
}

func TestJWKSClientKeepsKnownKeysWhileTheFetchIsDown(t *testing.T) {
	signer := newSigner(t, "kid-stale")

	var down atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if down.Load() {
			http.Error(w, "down", http.StatusBadGateway)

			return
		}

		w.Header().Set("Cache-Control", "public, max-age=60")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{signer.jwk()}})
	}))
	t.Cleanup(server.Close)

	now := time.Unix(1_760_000_000, 0)
	verifier := googleauth.NewVerifier(
		[]string{testClientID}, googleauth.WithJWKSURL(server.URL), googleauth.WithNow(func() time.Time { return now }),
	)

	token := signer.sign(t, jwt.MapClaims{
		"iss": "https://accounts.google.com", "aud": testClientID, "sub": "110248495921234",
		"email": "a@example.test", "email_verified": true, "exp": now.Add(time.Hour).Unix(),
	})

	_, err := verifier.Verify(context.Background(), token)
	require.NoError(t, err)

	// the cache has expired and Google does not answer, so the key that is known still checks the token
	now = now.Add(2 * time.Minute)
	down.Store(true)

	_, err = verifier.Verify(context.Background(), token)
	require.NoError(t, err)

	// a token with a key that was never known still fails with the fetch error
	other := newSigner(t, "kid-unknown")
	unknown := other.sign(t, jwt.MapClaims{
		"iss": "https://accounts.google.com", "aud": testClientID, "sub": "1", "email": "a@example.test", "email_verified": true,
		"exp": now.Add(time.Hour).Unix(),
	})
	now = now.Add(time.Minute)

	_, err = verifier.Verify(context.Background(), unknown)
	require.Error(t, err)
	require.NotErrorIs(t, err, googleauth.ErrInvalidToken)
}

func TestJWKSClientRejectsATokenWithoutExpiry(t *testing.T) {
	signer := newSigner(t, "kid-noexp")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{signer.jwk()}})
	}))
	t.Cleanup(server.Close)

	now := time.Unix(1_760_000_000, 0)
	verifier := googleauth.NewVerifier(
		[]string{testClientID}, googleauth.WithJWKSURL(server.URL), googleauth.WithNow(func() time.Time { return now }),
	)

	token := signer.sign(t, jwt.MapClaims{
		"iss": "https://accounts.google.com", "aud": testClientID, "sub": "110248495921234",
		"email": "a@example.test", "email_verified": true, "iat": now.Unix(),
	})

	_, err := verifier.Verify(context.Background(), token)
	require.ErrorIs(t, err, googleauth.ErrInvalidToken)
}

func TestJWKSClientServesCachedKeysWhileADownloadIsSlow(t *testing.T) {
	signer := newSigner(t, "kid-cached")
	release := make(chan struct{})
	started := make(chan struct{}, 1)

	var slow atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if slow.Load() {
			started <- struct{}{}
			<-release
		}

		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{signer.jwk()}})
	}))
	t.Cleanup(server.Close)

	var tick atomic.Int64

	tick.Store(1_760_000_000)

	now := func() time.Time { return time.Unix(tick.Load(), 0) }
	verifier := googleauth.NewVerifier([]string{testClientID}, googleauth.WithJWKSURL(server.URL), googleauth.WithNow(now))

	token := func(kid string) string {
		other := signer
		other.kid = kid

		return other.sign(t, jwt.MapClaims{
			"iss": "https://accounts.google.com", "aud": testClientID, "sub": "1", "email": "a@example.test",
			"email_verified": true, "exp": now().Add(time.Hour).Unix(),
		})
	}

	_, err := verifier.Verify(context.Background(), token("kid-cached"))
	require.NoError(t, err)

	// the cache is fresh and its last download is older than the wait, so a token with an unknown key downloads again
	tick.Add(30)
	slow.Store(true)

	done := make(chan struct{})

	go func() {
		defer close(done)

		_, _ = verifier.Verify(context.Background(), token("kid-unknown"))
	}()

	<-started

	answered := make(chan error, 1)

	go func() {
		_, err := verifier.Verify(context.Background(), token("kid-cached"))
		answered <- err
	}()

	select {
	case err := <-answered:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("a cached key waited for the download of another")
	}

	close(release)
	<-done
}

func tokenFor(t *testing.T, signer rsaSigner, now time.Time) string {
	t.Helper()

	return signer.sign(t, jwt.MapClaims{
		"iss": "https://accounts.google.com", "aud": testClientID, "sub": "110248495921234",
		"email": "a@example.test", "email_verified": true, "exp": now.Add(time.Hour).Unix(),
	})
}

func TestJWKSClientUsesOnlyUsableSigningKeys(t *testing.T) {
	signer := newSigner(t, "kid-good")
	now := time.Unix(1_760_000_000, 0)

	entry := func(mutate func(map[string]any)) map[string]any {
		jwk := signer.jwk()
		mutate(jwk)

		return jwk
	}

	cases := map[string]struct {
		keys    []any
		cache   string
		wantErr bool
	}{
		"skips what is not a signing key and uses the good one": {
			keys: []any{
				entry(func(j map[string]any) { j["kty"] = "EC"; j["kid"] = "ec" }),
				entry(func(j map[string]any) { j["kid"] = "" }),
				entry(func(j map[string]any) { j["use"] = "enc"; j["kid"] = "enc" }),
				entry(func(j map[string]any) { j["alg"] = "RS384"; j["kid"] = "rs384" }),
				entry(func(j map[string]any) { j["n"] = "!!"; j["kid"] = "badn" }),
				entry(func(j map[string]any) { j["e"] = "!!"; j["kid"] = "bade" }),
				entry(func(j map[string]any) { j["e"] = "AQ"; j["kid"] = "smalle" }),
				signer.jwk(),
			},
			cache: "public, max-age=1",
		},
		"a long max-age is capped": {keys: []any{signer.jwk()}, cache: "max-age=99999999"},
		"a bad max-age falls back": {keys: []any{signer.jwk()}, cache: "max-age=abc, public"},
		"no usable key is a fetch error": {
			keys:    []any{entry(func(j map[string]any) { j["kty"] = "EC" })},
			wantErr: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.cache != "" {
					w.Header().Set("Cache-Control", tc.cache)
				}

				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"keys": tc.keys})
			}))
			t.Cleanup(server.Close)

			verifier := googleauth.NewVerifier(
				[]string{testClientID}, googleauth.WithJWKSURL(server.URL), googleauth.WithNow(func() time.Time { return now }),
				googleauth.WithHTTPClient(server.Client()),
			)

			_, err := verifier.Verify(context.Background(), tokenFor(t, signer, now))
			if tc.wantErr {
				require.Error(t, err)
				require.NotErrorIs(t, err, googleauth.ErrInvalidToken)

				return
			}

			require.NoError(t, err)
		})
	}
}

func TestJWKSClientFetchFailuresAreRetriedAfterTheWait(t *testing.T) {
	signer := newSigner(t, "kid-retry")
	now := time.Unix(1_760_000_000, 0)

	var hits atomic.Int32

	var mode atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)

		switch mode.Load() {
		case 0:
			http.Error(w, "down", http.StatusBadGateway)
		case 1:
			_, _ = w.Write([]byte("not json"))
		default:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{signer.jwk()}})
		}
	}))
	t.Cleanup(server.Close)

	verifier := googleauth.NewVerifier(
		[]string{testClientID}, googleauth.WithJWKSURL(server.URL), googleauth.WithNow(func() time.Time { return now }),
	)
	token := tokenFor(t, signer, now)

	// the first failure is a bad status, and the callers in the next seconds get the same answer without a new download
	_, err := verifier.Verify(context.Background(), token)
	require.Error(t, err)

	_, err = verifier.Verify(context.Background(), token)
	require.Error(t, err)
	require.Equal(t, int32(1), hits.Load())

	// after the wait a download starts again, and a body that is no JSON fails it again
	now = now.Add(6 * time.Second)
	mode.Store(1)

	_, err = verifier.Verify(context.Background(), token)
	require.Error(t, err)
	require.Equal(t, int32(2), hits.Load())

	// then Google answers again
	now = now.Add(6 * time.Second)
	mode.Store(2)

	_, err = verifier.Verify(context.Background(), token)
	require.NoError(t, err)
}

func TestJWKSClientAnswersUnavailableWhenGoogleCannotBeReached(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()

	signer := newSigner(t, "kid-gone")
	now := time.Unix(1_760_000_000, 0)
	verifier := googleauth.NewVerifier(
		[]string{testClientID}, googleauth.WithJWKSURL(url), googleauth.WithNow(func() time.Time { return now }),
	)

	_, err := verifier.Verify(context.Background(), tokenFor(t, signer, now))
	require.Error(t, err)
	require.NotErrorIs(t, err, googleauth.ErrInvalidToken)
}
