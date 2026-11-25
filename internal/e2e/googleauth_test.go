package e2e

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/bootstrap"
	"github.com/SyafaHadyan/kibooz-backend/internal/googleauth"
)

const testGoogleClientID = "e2e-kibooz-web-client.example.test"

var (
	mockGoogleOnce   sync.Once
	mockGoogleKey    *rsa.PrivateKey
	mockGoogleServer *httptest.Server
)

func initMockGoogleVerifier() googleauth.Verifier {
	mockGoogleOnce.Do(func() {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}

		mockGoogleKey = key
		jwk := map[string]any{
			"kty": "RSA",
			"alg": "RS256",
			"use": "sig",
			"kid": "e2e-kid-1",
			"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}

		mockGoogleServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{jwk}})
		}))
	})

	return googleauth.NewVerifier([]string{testGoogleClientID}, googleauth.WithJWKSURL(mockGoogleServer.URL))
}

func signMockGoogleIDToken(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	app(t)

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "e2e-kid-1"

	signed, err := token.SignedString(mockGoogleKey)
	require.NoError(t, err)

	return signed
}

func TestGoogleAuthFlow(t *testing.T) {
	now := time.Now()
	guru := registerGuru(t, "Google Class Teacher")
	_, joinCode := classOf(t, guru)

	t.Run("unconfigured server rejects google sign-in cleanly", func(t *testing.T) {
		app(t)
		t.Setenv("GOOGLE_CLIENT_IDS", "")

		started, err := bootstrap.Start("e2e-google-off")
		require.NoError(t, err)
		t.Cleanup(started.Close)

		res := callApp(t, started.App.Fiber, http.MethodPost, "/api/v1/auth/google", "", map[string]any{
			"idToken": "unconfigured-" + suffix(),
			"role":    "WALI",
		})
		res.requireError(t, http.StatusServiceUnavailable, "AUTH_GOOGLE_UNAVAILABLE")
	})

	t.Run("invalid google token is rejected with 401", func(t *testing.T) {
		badAud := signMockGoogleIDToken(t, jwt.MapClaims{
			"iss": "https://accounts.google.com", "aud": "wrong-client", "sub": "bad-sub",
			"email": "bad@gmail.com", "email_verified": true, "exp": now.Add(10 * time.Minute).Unix(),
		})

		call(t, http.MethodPost, "/api/v1/auth/google", "", map[string]any{
			"idToken": badAud, "role": "WALI",
		}).requireError(t, http.StatusUnauthorized, "AUTH_GOOGLE_INVALID")

		call(t, http.MethodPost, "/api/v1/auth/google/register", "", map[string]any{
			"idToken": badAud, "fullName": "Orang Tua", "role": "WALI",
			"classCode": joinCode, "student": map[string]any{"fullName": "Anak", "nisn": "9988776655"},
		}).requireError(t, http.StatusUnauthorized, "AUTH_GOOGLE_INVALID")
	})

	t.Run("new google wali goes through onboarding then signs in directly", func(t *testing.T) {
		email := "google.wali." + suffix() + "@gmail.com"
		sub := "google-sub-wali-" + suffix()
		idToken := signMockGoogleIDToken(t, jwt.MapClaims{
			"iss":            "https://accounts.google.com",
			"aud":            testGoogleClientID,
			"sub":            sub,
			"email":          email,
			"email_verified": true,
			"name":           "Ibu Google",
			"picture":        "https://lh3.googleusercontent.com/a/wali",
			"exp":            now.Add(10 * time.Minute).Unix(),
		})

		check := call(t, http.MethodPost, "/api/v1/auth/google", "", map[string]any{
			"idToken": idToken, "role": "WALI",
		})
		require.Equal(t, http.StatusOK, check.Status, "body %v", check.Body)
		require.Equal(t, true, check.data("needsRegistration"))
		require.Equal(t, email, check.data("googleProfile", "email"))
		require.Equal(t, "Ibu Google", check.data("googleProfile", "fullName"))

		reg := call(t, http.MethodPost, "/api/v1/auth/google/register", "", map[string]any{
			"idToken":   idToken,
			"fullName":  "Ibu Google",
			"role":      "WALI",
			"classCode": joinCode,
			"student":   map[string]any{"fullName": "Anak Google", "nisn": nisn()},
		})
		require.Equal(t, http.StatusCreated, reg.Status, "body %v", reg.Body)

		regToken, _ := reg.data("token").(string)
		require.NotEmpty(t, regToken)
		require.Equal(t, http.StatusOK, call(t, http.MethodGet, "/api/v1/wali/dashboard", regToken, nil).Status)

		// Repeating login with wrong role returns AUTH_ROLE_MISMATCH
		call(t, http.MethodPost, "/api/v1/auth/google", "", map[string]any{
			"idToken": idToken, "role": "GURU",
		}).requireError(t, http.StatusConflict, "AUTH_ROLE_MISMATCH")

		// Logging in again with WALI issues a session directly
		login := call(t, http.MethodPost, "/api/v1/auth/google", "", map[string]any{
			"idToken": idToken, "role": "WALI",
		})
		require.Equal(t, http.StatusOK, login.Status, "body %v", login.Body)
		require.Equal(t, false, login.data("needsRegistration"))
		loginToken, _ := login.data("token").(string)
		require.NotEmpty(t, loginToken)
		require.Equal(t, http.StatusOK, call(t, http.MethodGet, "/api/v1/wali/dashboard", loginToken, nil).Status)

		// Password-only endpoints reject cleanly on passwordless Google accounts
		call(t, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
			"email": email, "password": testPassword, "role": "WALI",
		}).requireError(t, http.StatusUnauthorized, "AUTH_INVALID_CREDENTIALS")

		call(t, http.MethodPut, changePasswordPath, loginToken, map[string]any{
			"currentPassword": testPassword, "newPassword": "Next-" + suffix() + "!",
		}).requireError(t, http.StatusConflict, "AUTH_PASSWORD_NOT_SET")
	})

	t.Run("new google guru completes onboarding and reaches dashboard", func(t *testing.T) {
		sub := "google-sub-guru-" + suffix()
		idToken := signMockGoogleIDToken(t, jwt.MapClaims{
			"iss":            "accounts.google.com",
			"aud":            testGoogleClientID,
			"sub":            sub,
			"email":          "google.guru." + suffix() + "@gmail.com",
			"email_verified": true,
			"name":           "Pak Google",
			"exp":            now.Add(10 * time.Minute).Unix(),
		})

		reg := call(t, http.MethodPost, "/api/v1/auth/google/register", "", map[string]any{
			"idToken":  idToken,
			"fullName": "Pak Google",
			"role":     "GURU",
			"class":    map[string]any{"name": "Anggrek", "gradeLevel": "Class B"},
		})
		require.Equal(t, http.StatusCreated, reg.Status, "body %v", reg.Body)
		guruToken, _ := reg.data("token").(string)
		require.Equal(t, http.StatusOK, call(t, http.MethodGet, "/api/v1/guru/dashboard", guruToken, nil).Status)
	})

	t.Run("an email that a password account holds is never linked by email", func(t *testing.T) {
		token := signMockGoogleIDToken(t, jwt.MapClaims{
			"iss": "https://accounts.google.com", "aud": testGoogleClientID, "sub": "pre-hijack-sub-" + suffix(),
			"email": guru.Email, "email_verified": true, "exp": now.Add(10 * time.Minute).Unix(),
		})

		call(t, http.MethodPost, "/api/v1/auth/google", "", map[string]any{
			"idToken": token, "role": "GURU",
		}).requireError(t, http.StatusConflict, "AUTH_ACCOUNT_EXISTS")

		// the password of the account still works and the account has no link
		require.Equal(t, http.StatusOK, loginAs(t, guru.Email, guru.Password, "GURU").Status)
	})

	t.Run("a signed in account links Google with its password and a fresh token, then unlinks it", func(t *testing.T) {
		session := loginAs(t, guru.Email, guru.Password, "GURU")
		require.Equal(t, http.StatusOK, session.Status)

		access := session.data("token").(string)
		sub := "linked-sub-" + suffix()
		fresh := func() string {
			return signMockGoogleIDToken(t, jwt.MapClaims{
				"iss": "https://accounts.google.com", "aud": testGoogleClientID, "sub": sub,
				"email": guru.Email, "email_verified": true, "iat": time.Now().Unix(), "exp": now.Add(10 * time.Minute).Unix(),
			})
		}
		stale := signMockGoogleIDToken(t, jwt.MapClaims{
			"iss": "https://accounts.google.com", "aud": testGoogleClientID, "sub": sub,
			"email": guru.Email, "email_verified": true, "iat": time.Now().Add(-time.Hour).Unix(), "exp": now.Add(10 * time.Minute).Unix(),
		})

		call(t, http.MethodPost, "/api/v1/users/me/google", access, map[string]any{
			"idToken": stale, "password": guru.Password,
		}).requireError(t, http.StatusUnauthorized, "AUTH_GOOGLE_STALE")

		call(t, http.MethodPost, "/api/v1/users/me/google", access, map[string]any{
			"idToken": fresh(), "password": strings.ToUpper(guru.Password),
		}).requireError(t, http.StatusForbidden, "AUTH_PASSWORD_INCORRECT")

		linked := call(t, http.MethodPost, "/api/v1/users/me/google", access, map[string]any{
			"idToken": fresh(), "password": guru.Password,
		})
		require.Equal(t, http.StatusOK, linked.Status, "body %v", linked.Body)

		call(t, http.MethodPost, "/api/v1/users/me/google", access, map[string]any{
			"idToken": fresh(), "password": guru.Password,
		}).requireError(t, http.StatusConflict, "GOOGLE_ALREADY_LINKED")

		// signing in with Google now finds the account by its subject
		signedIn := call(t, http.MethodPost, "/api/v1/auth/google", "", map[string]any{"idToken": fresh(), "role": "GURU"})
		require.Equal(t, http.StatusOK, signedIn.Status, "body %v", signedIn.Body)
		require.Equal(t, false, signedIn.data("needsRegistration"))

		// unlinking needs the password and ends every session
		call(t, http.MethodDelete, "/api/v1/users/me/google", access, map[string]any{
			"password": strings.ToUpper(guru.Password),
		}).requireError(t, http.StatusForbidden, "AUTH_PASSWORD_INCORRECT")

		unlinked := call(t, http.MethodDelete, "/api/v1/users/me/google", access, map[string]any{"password": guru.Password})
		require.Equal(t, http.StatusOK, unlinked.Status, "body %v", unlinked.Body)

		call(t, http.MethodDelete, "/api/v1/users/me/google", loginAs(t, guru.Email, guru.Password, "GURU").data("token").(string), map[string]any{
			"password": guru.Password,
		}).requireError(t, http.StatusConflict, "GOOGLE_NOT_LINKED")
	})

	t.Run("a Google only account sets a password and deletes itself with a fresh token", func(t *testing.T) {
		sub := "only-sub-" + suffix()
		email := "only-" + suffix() + "@example.test"
		token := func() string {
			return signMockGoogleIDToken(t, jwt.MapClaims{
				"iss": "https://accounts.google.com", "aud": testGoogleClientID, "sub": sub, "name": "Only Google",
				"email": email, "email_verified": true, "iat": time.Now().Unix(), "exp": now.Add(10 * time.Minute).Unix(),
			})
		}

		registered := call(t, http.MethodPost, "/api/v1/auth/google/register", "", map[string]any{
			"idToken": token(), "fullName": "Only Google", "role": "GURU",
			"class": map[string]any{"name": "Anggrek", "gradeLevel": "Class B"},
		})
		require.Equal(t, http.StatusCreated, registered.Status, "body %v", registered.Body)

		access := registered.data("token").(string)
		call(t, http.MethodPut, "/api/v1/users/me/password", access, map[string]any{
			"currentPassword": strings.ToUpper(testPassword), "newPassword": testPassword + "0",
		}).requireError(t, http.StatusConflict, "AUTH_PASSWORD_NOT_SET")

		set := call(t, http.MethodPost, "/api/v1/users/me/password", access, map[string]any{
			"idToken": token(), "newPassword": testPassword + "0",
		})
		require.Equal(t, http.StatusOK, set.Status, "body %v", set.Body)

		// a second go answers that the account has a password now
		call(t, http.MethodPost, "/api/v1/users/me/password", access, map[string]any{
			"idToken": token(), "newPassword": testPassword + "0",
		}).requireError(t, http.StatusConflict, "AUTH_PASSWORD_ALREADY_SET")
	})

	t.Run("a Google only account confirms its deletion with a fresh token", func(t *testing.T) {
		sub := "gone-sub-" + suffix()
		token := func(issued time.Time) string {
			return signMockGoogleIDToken(t, jwt.MapClaims{
				"iss": "https://accounts.google.com", "aud": testGoogleClientID, "sub": sub, "name": "Gone Google",
				"email": "gone-" + suffix() + "@example.test", "email_verified": true, "iat": issued.Unix(), "exp": now.Add(10 * time.Minute).Unix(),
			})
		}

		registered := call(t, http.MethodPost, "/api/v1/auth/google/register", "", map[string]any{
			"idToken": token(time.Now()), "fullName": "Gone Google", "role": "GURU",
			"class": map[string]any{"name": "Anggrek", "gradeLevel": "Class B"},
		})
		require.Equal(t, http.StatusCreated, registered.Status, "body %v", registered.Body)

		access := registered.data("token").(string)

		call(t, http.MethodDelete, "/api/v1/users/me", access, map[string]any{
			"idToken": token(time.Now().Add(-time.Hour)),
		}).requireError(t, http.StatusUnauthorized, "AUTH_GOOGLE_STALE")

		deleted := call(t, http.MethodDelete, "/api/v1/users/me", access, map[string]any{"idToken": token(time.Now())})
		require.Equal(t, http.StatusOK, deleted.Status, "body %v", deleted.Body)
	})
}
