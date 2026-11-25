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
