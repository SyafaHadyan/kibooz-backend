// Package googleauth verifies Google OIDC ID tokens against Google's JWK public key set.
package googleauth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	defaultJWKSURL     = "https://www.googleapis.com/oauth2/v3/certs"
	defaultCacheTTL    = 5 * time.Minute
	minCacheTTL        = 30 * time.Second
	maxCacheTTL        = 24 * time.Hour
	minRefreshInterval = 5 * time.Second
	maxJWKSBodyBytes   = 1 << 20
	httpTimeout        = 5 * time.Second
)

var (
	// ErrNotConfigured is returned when no Google OAuth client IDs are configured on the server.
	ErrNotConfigured = errors.New("google sign-in is not configured")
	// ErrInvalidToken is returned when an ID token is malformed, expired, unsigned, untrusted, or lacks a verified email.
	ErrInvalidToken = errors.New("invalid google id token")
)

// Claims contains the verified Google identity fields extracted from an ID token.
type Claims struct {
	Subject       string
	Email         string
	EmailVerified bool
	FullName      string
	AvatarURL     string
	// IssuedAt is when Google issued the token, and a sensitive action only accepts a token that was issued a moment ago
	IssuedAt time.Time
}

// maxClockSkew is how far into the future the issue time of a token may be, for a clock that runs a little fast
const maxClockSkew = 30 * time.Second

// FreshWithin tells whether Google issued the token no longer than window ago. A token that has no issue time is never fresh.
func (c Claims) FreshWithin(now time.Time, window time.Duration) bool {
	if c.IssuedAt.IsZero() || c.IssuedAt.After(now.Add(maxClockSkew)) {
		return false
	}

	return now.Sub(c.IssuedAt) <= window
}

// Verifier verifies a raw Google OIDC ID token and returns the verified user claims.
type Verifier interface {
	Verify(ctx context.Context, idToken string) (Claims, error)
}

// SwitchableVerifier wraps a Verifier behind an RWMutex so tests can install a mock verifier safely.
type SwitchableVerifier struct {
	mu       sync.RWMutex
	delegate Verifier
}

// NewSwitchableVerifier wraps initial (nil behaves as unconfigured until Set is called).
func NewSwitchableVerifier(initial Verifier) *SwitchableVerifier {
	return &SwitchableVerifier{delegate: initial}
}

// Set replaces the underlying verifier.
func (s *SwitchableVerifier) Set(v Verifier) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.delegate = v
}

// Verify delegates to the installed verifier, or returns ErrNotConfigured if nil.
func (s *SwitchableVerifier) Verify(ctx context.Context, idToken string) (Claims, error) {
	s.mu.RLock()
	v := s.delegate
	s.mu.RUnlock()

	if v == nil {
		return Claims{}, ErrNotConfigured
	}

	return v.Verify(ctx, idToken)
}

// Option customizes a JWKSClient verifier.
type Option func(*JWKSClient)

// WithJWKSURL overrides the JWK endpoint URL (intended for tests).
func WithJWKSURL(rawURL string) Option {
	return func(c *JWKSClient) {
		if trimmed := strings.TrimSpace(rawURL); trimmed != "" {
			c.jwksURL = trimmed
		}
	}
}

// WithHTTPClient overrides the HTTP client used to download JWK certificates.
func WithHTTPClient(client *http.Client) Option {
	return func(c *JWKSClient) {
		if client != nil {
			c.httpClient = client
		}
	}
}

// WithNow overrides the clock function (intended for deterministic time tests).
func WithNow(now func() time.Time) Option {
	return func(c *JWKSClient) {
		if now != nil {
			c.now = now
		}
	}
}

// JWKSClient validates RS256 Google ID tokens using cached RSA public keys from Google's JWK endpoint.
type JWKSClient struct {
	clientIDs  []string
	jwksURL    string
	httpClient *http.Client
	now        func() time.Time

	// fetchMu lets one download run at a time. It is never held by a reader of the cache, so a slow download of Google
	// does not hold up the checks that a cached key can answer.
	fetchMu sync.Mutex

	mu          sync.RWMutex
	keys        map[string]*rsa.PublicKey
	expiresAt   time.Time
	lastRefresh time.Time
	// retryAt is when a download may start again after one failed, and failure is the error of that download
	retryAt time.Time
	failure error
}

// NewVerifier returns a JWKSClient configured for the given accepted Google OAuth client IDs.
func NewVerifier(clientIDs []string, opts ...Option) *JWKSClient {
	cleaned := make([]string, 0, len(clientIDs))
	for _, id := range clientIDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" && !slices.Contains(cleaned, trimmed) {
			cleaned = append(cleaned, trimmed)
		}
	}

	client := &JWKSClient{
		clientIDs:  cleaned,
		jwksURL:    defaultJWKSURL,
		httpClient: &http.Client{Timeout: httpTimeout},
		now:        time.Now,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(client)
		}
	}

	return client
}

type idTokenClaims struct {
	jwt.RegisteredClaims
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"` //nolint:tagliatelle // Google OIDC ID token claims use snake_case field names
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

// Verify checks signature, expiry, issuer, accepted audience, and verified email on idToken.
func (c *JWKSClient) Verify(ctx context.Context, idToken string) (Claims, error) {
	if len(c.clientIDs) == 0 {
		return Claims{}, ErrNotConfigured
	}

	raw := strings.TrimSpace(idToken)
	if raw == "" {
		return Claims{}, ErrInvalidToken
	}

	claims := new(idTokenClaims)

	parsed, err := jwt.ParseWithClaims(
		raw,
		claims,
		func(token *jwt.Token) (any, error) {
			kid, _ := token.Header["kid"].(string)
			if strings.TrimSpace(kid) == "" {
				return nil, ErrInvalidToken
			}

			return c.publicKey(ctx, kid)
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithTimeFunc(c.now),
		jwt.WithLeeway(30*time.Second),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		var fetchErr *jwksFetchError
		if errors.As(err, &fetchErr) {
			return Claims{}, fetchErr
		}

		return Claims{}, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}

	if !parsed.Valid {
		return Claims{}, ErrInvalidToken
	}

	if !isGoogleIssuer(claims.Issuer) || !hasAcceptedAudience(claims.Audience, c.clientIDs) {
		return Claims{}, ErrInvalidToken
	}

	subject := strings.TrimSpace(claims.Subject)
	email := strings.ToLower(strings.TrimSpace(claims.Email))
	if subject == "" || email == "" || !claims.EmailVerified {
		return Claims{}, ErrInvalidToken
	}

	return Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: true,
		FullName:      strings.TrimSpace(claims.Name),
		AvatarURL:     strings.TrimSpace(claims.Picture),
		IssuedAt:      issuedAt(claims.IssuedAt),
	}, nil
}

func issuedAt(at *jwt.NumericDate) time.Time {
	if at == nil {
		return time.Time{}
	}

	return at.Time
}

func isGoogleIssuer(issuer string) bool {
	return issuer == "https://accounts.google.com" || issuer == "accounts.google.com"
}

func hasAcceptedAudience(aud jwt.ClaimStrings, allowed []string) bool {
	for _, candidate := range aud {
		if slices.Contains(allowed, candidate) {
			return true
		}
	}

	return false
}

func (c *JWKSClient) publicKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	now := c.now()

	c.mu.RLock()
	key := c.keys[kid]
	fresh := key != nil && now.Before(c.expiresAt)
	c.mu.RUnlock()

	if fresh {
		return key, nil
	}

	keys, err := c.refreshKeys(ctx, kid)
	if err != nil {
		return nil, err
	}

	key, ok := keys[kid]
	if !ok {
		return nil, ErrInvalidToken
	}

	return key, nil
}

func (c *JWKSClient) refreshKeys(ctx context.Context, neededKID string) (map[string]*rsa.PublicKey, error) {
	c.fetchMu.Lock()
	defer c.fetchMu.Unlock()

	// the callers that waited for the download in front of them find its result here and download nothing
	if keys, done, err := c.cached(neededKID); done {
		return keys, err
	}

	fetched, ttl, err := c.fetchJWKS(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()

	// the clock is read after the download, so a slow answer does not use up the wait before the next one
	now := c.now()

	if err != nil {
		c.retryAt = now.Add(minRefreshInterval)
		c.failure = err

		// Google rotates its keys rarely, so a key that is already known still checks tokens while the download is down
		if c.keys[neededKID] != nil {
			return c.keys, nil
		}

		return nil, err
	}

	c.keys = fetched
	c.lastRefresh = now
	c.expiresAt = now.Add(ttl)
	c.retryAt = time.Time{}
	c.failure = nil

	return c.keys, nil
}

// cached answers a refresh without a download when the cache can. done is false when a download is due.
func (c *JWKSClient) cached(neededKID string) (map[string]*rsa.PublicKey, bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	now := c.now()
	if key := c.keys[neededKID]; key != nil && now.Before(c.expiresAt) {
		return c.keys, true, nil
	}

	if len(c.keys) > 0 && now.Sub(c.lastRefresh) < minRefreshInterval && now.Before(c.expiresAt) {
		return c.keys, true, nil
	}

	// after a failed download nobody downloads again until the wait is over
	if c.failure != nil && now.Before(c.retryAt) {
		if c.keys[neededKID] != nil {
			return c.keys, true, nil
		}

		return nil, true, c.failure
	}

	return nil, false, nil
}

type jwksFetchError struct {
	err error
}

func (e *jwksFetchError) Error() string {
	return fmt.Sprintf("fetch google jwks: %v", e.err)
}

func (e *jwksFetchError) Unwrap() error {
	return e.err
}

type jwkSet struct {
	Keys []jwkEntry `json:"keys"`
}

type jwkEntry struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func (c *JWKSClient) fetchJWKS(ctx context.Context) (map[string]*rsa.PublicKey, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.jwksURL, nil)
	if err != nil {
		return nil, 0, &jwksFetchError{err: err}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, &jwksFetchError{err: err}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, &jwksFetchError{err: fmt.Errorf("unexpected status %d", resp.StatusCode)}
	}

	var payload jwkSet

	err = json.NewDecoder(io.LimitReader(resp.Body, maxJWKSBodyBytes)).Decode(&payload)
	if err != nil {
		return nil, 0, &jwksFetchError{err: err}
	}

	keys := make(map[string]*rsa.PublicKey, len(payload.Keys))
	for _, entry := range payload.Keys {
		if entry.Kty != "RSA" || entry.Kid == "" {
			continue
		}

		if entry.Use != "" && entry.Use != "sig" {
			continue
		}

		if entry.Alg != "" && entry.Alg != jwt.SigningMethodRS256.Alg() {
			continue
		}

		pub, err := parseRSAPublicKey(entry.N, entry.E)
		if err != nil {
			continue
		}

		keys[entry.Kid] = pub
	}

	if len(keys) == 0 {
		return nil, 0, &jwksFetchError{err: errors.New("jwks returned no usable signing keys")}
	}

	return keys, parseCacheMaxAge(resp.Header.Get("Cache-Control")), nil
}

func parseRSAPublicKey(encodedN string, encodedE string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(encodedN)
	if err != nil || len(nBytes) == 0 {
		return nil, errors.New("invalid modulus")
	}

	eBytes, err := base64.RawURLEncoding.DecodeString(encodedE)
	if err != nil || len(eBytes) == 0 {
		return nil, errors.New("invalid exponent")
	}

	exp := new(big.Int).SetBytes(eBytes)
	if !exp.IsInt64() || exp.Int64() < 3 {
		return nil, errors.New("invalid rsa exponent")
	}

	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: int(exp.Int64()),
	}, nil
}

func parseCacheMaxAge(cacheControl string) time.Duration {
	for _, part := range strings.Split(cacheControl, ",") {
		part = strings.TrimSpace(strings.ToLower(part))
		if raw, ok := strings.CutPrefix(part, "max-age="); ok {
			seconds, err := strconv.Atoi(raw)
			if err != nil || seconds <= 0 {
				continue
			}

			ttl := time.Duration(seconds) * time.Second
			if ttl < minCacheTTL {
				return minCacheTTL
			}

			if ttl > maxCacheTTL {
				return maxCacheTTL
			}

			return ttl
		}
	}

	return defaultCacheTTL
}
