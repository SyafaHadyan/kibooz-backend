// Package mailer sends the mail of the API through Resend
package mailer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kiboozapp/kibooz-backend/internal/infra/env"
)

// sendTimeout is how long one call to Resend may take. The request that sends the code waits for it, so it stays short.
const sendTimeout = 5 * time.Second

// defaultURL is the Resend API, and the setting only differs in a test
const defaultURL = "https://api.resend.com"

// ErrDisabled is returned when no key is set, so nothing can be sent
var ErrDisabled = errors.New("mail is not configured, RESEND_API_KEY is empty")

// ErrQuota is returned when Resend answers 429 and names a quota of the plan. A retry job stops for a while then.
var ErrQuota = errors.New("resend refused the mail for its quota")

// ErrRateLimit is returned when Resend answers any other 429, which is a limit of requests in a moment
var ErrRateLimit = errors.New("resend refused the mail for its rate")

// Sender sends the mail of the API
type Sender interface {
	// SendVerificationCode mails the code of an email verification to the address, and says how many minutes it works. The mail
	// names nobody, because a name that a stranger typed would reach the inbox of someone else in a mail that looks like the service's.
	SendVerificationCode(ctx context.Context, to string, code string, validMinutes int) error
}

// Resend sends mail through the Resend API
type Resend struct {
	client *http.Client
	url    string
	key    string
	from   string
}

// New builds the sender from the settings. Without a key every send answers ErrDisabled.
func New(cfg *env.Env) *Resend {
	base := cfg.ResendAPIURL
	if base == "" {
		base = defaultURL
	}

	return &Resend{
		client: &http.Client{Timeout: sendTimeout},
		url:    strings.TrimRight(base, "/") + "/emails",
		key:    strings.TrimSpace(cfg.ResendAPIKey),
		from:   sender(cfg.EmailFrom),
	}
}

// sender gives a bare address the name of the service, and leaves an address that has a name as it is
func sender(from string) string {
	from = strings.TrimSpace(from)
	if from == "" || strings.Contains(from, "<") {
		return from
	}

	return "Kibooz <" + from + ">"
}

type resendMessage struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
	Text    string   `json:"text"`
}

// SendVerificationCode mails the code in Indonesian, as a plain text part and an html part
func (r *Resend) SendVerificationCode(ctx context.Context, to string, code string, validMinutes int) error {
	if r.key == "" {
		return ErrDisabled
	}

	text := fmt.Sprintf(
		"Halo,\n\nKode verifikasi email Kibooz Anda adalah %s.\n\nKode ini berlaku %d menit. Jika Anda tidak mendaftar di Kibooz, abaikan email ini.\n",
		code, validMinutes,
	)

	page := fmt.Sprintf(
		`<p>Halo,</p><p>Kode verifikasi email Kibooz Anda adalah</p><p style="font-size:28px;font-weight:bold;letter-spacing:6px">%s</p>`+
			`<p>Kode ini berlaku %d menit. Jika Anda tidak mendaftar di Kibooz, abaikan email ini.</p>`,
		html.EscapeString(code), validMinutes,
	)

	body, err := json.Marshal(resendMessage{
		From: r.from, To: []string{to}, Subject: "Kode verifikasi Kibooz Anda", HTML: page, Text: text,
	})
	if err != nil {
		return fmt.Errorf("encode mail: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build mail request: %w", err)
	}

	request.Header.Set("Authorization", "Bearer "+r.key)
	request.Header.Set("Content-Type", "application/json")

	response, err := r.client.Do(request)
	if err != nil {
		// the error of the client holds the address and never the key, which only travels in the header
		return fmt.Errorf("send mail: %w", err)
	}

	defer func() { _ = response.Body.Close() }()

	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))

		return nil
	}

	// the body names the reason, such as a daily quota, and holds no secret
	detail, _ := io.ReadAll(io.LimitReader(response.Body, 512))

	if response.StatusCode == http.StatusTooManyRequests {
		refusal := ErrRateLimit
		// the names of the refusals of the plan, daily_quota_exceeded and monthly_quota_exceeded, both hold the word
		if strings.Contains(string(detail), "quota") {
			refusal = ErrQuota
		}

		return fmt.Errorf("%w, resend answered 429 %s", refusal, strings.TrimSpace(string(detail)))
	}

	return fmt.Errorf("resend answered %d %s", response.StatusCode, strings.TrimSpace(string(detail)))
}
