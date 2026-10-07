// Package devicetoken issues and checks the token that marks a device as one that has signed in to an email before.
// The token is not a credential and never replaces the password. It only lets the rate limiter give a returning
// device its own budget, so that someone who has no token cannot use up the budget of the account owner.
package devicetoken

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"time"

	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
)

const (
	version = "v1"

	// label keeps the signing key apart from the JWT secret it is derived from
	label = "kibooz device token v1"

	emailHashBytes = 16
	deviceIDBytes  = 16
	expiryBytes    = 8
	payloadBytes   = emailHashBytes + deviceIDBytes + expiryBytes

	day = 24 * time.Hour
)

// Tokens signs and verifies device tokens. Every instance built from the same config accepts the tokens of the others.
type Tokens struct {
	key []byte
	ttl time.Duration
	now func() time.Time
}

// New derives the signing key from the JWT secret and takes the lifetime from DEVICE_TOKEN_TTL_DAYS
func New(cfg *env.Env) *Tokens {
	mac := hmac.New(sha256.New, []byte(cfg.JWTSecretKey))
	mac.Write([]byte(label))

	return &Tokens{
		key: mac.Sum(nil),
		ttl: time.Duration(cfg.DeviceTokenTTLDays) * day,
		now: time.Now,
	}
}

// Issue returns a token for the email. A device that already holds a valid token for it keeps the same device id,
// so its rate limit bucket does not change each time it signs in.
func (t *Tokens) Issue(email string, presented string) (string, error) {
	deviceID, ok := t.Verify(presented, email)
	if !ok {
		deviceID = make([]byte, deviceIDBytes)

		if _, err := rand.Read(deviceID); err != nil {
			return "", err
		}
	}

	payload := make([]byte, 0, payloadBytes)
	payload = append(payload, emailHash(email)...)
	payload = append(payload, deviceID...)
	payload = binary.BigEndian.AppendUint64(payload, uint64(t.now().Add(t.ttl).Unix())) //nolint:gosec // the expiry is a positive time

	enc := base64.RawURLEncoding

	return version + "." + enc.EncodeToString(payload) + "." + enc.EncodeToString(t.sign(payload)), nil
}

// Verify returns the device id when the token is signed by this server, has not expired and was issued for the email
func (t *Tokens) Verify(token string, email string) ([]byte, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != version {
		return nil, false
	}

	enc := base64.RawURLEncoding

	payload, err := enc.DecodeString(parts[1])
	if err != nil || len(payload) != payloadBytes {
		return nil, false
	}

	signature, err := enc.DecodeString(parts[2])
	if err != nil || !hmac.Equal(signature, t.sign(payload)) {
		return nil, false
	}

	if !hmac.Equal(payload[:emailHashBytes], emailHash(email)) {
		return nil, false
	}

	expiry := int64(binary.BigEndian.Uint64(payload[emailHashBytes+deviceIDBytes:])) //nolint:gosec // an expiry that does not fit is rejected below

	if expiry <= 0 || !t.now().Before(time.Unix(expiry, 0)) {
		return nil, false
	}

	return payload[emailHashBytes : emailHashBytes+deviceIDBytes], true
}

func (t *Tokens) sign(payload []byte) []byte {
	mac := hmac.New(sha256.New, t.key)
	mac.Write(payload)

	return mac.Sum(nil)
}

// emailHash uses the same normalization as sign in, so the letter case of the email does not matter
func emailHash(email string) []byte {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))

	return sum[:emailHashBytes]
}
