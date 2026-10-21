// Package push wakes a closed app when a chat message arrives, through Firebase Cloud Messaging. The message carries ids and
// nothing a person can read, so the text never passes through Google, see docs/adr/0013-chat-push-notifications.md.
package push

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/jwt"
)

const (
	// scope is the permission of the access token that the service account asks for
	scope = "https://www.googleapis.com/auth/firebase.messaging"
	// defaultEndpoint is the FCM HTTP v1 address, with the project id filled in
	defaultEndpoint = "https://fcm.googleapis.com/v1/projects/%s/messages:send"
	// requestTimeout bounds one call to FCM
	requestTimeout = 10 * time.Second
	// ttl is how long FCM keeps a message for a phone that is off, a day, and the collapse key keeps only the newest one
	ttl = "86400s"
	// maxResponse is how much of an answer of FCM is read, which is only an error that fits in a few lines
	maxResponse = 16 * 1024
)

// ErrUnregistered says that FCM no longer knows the token, because the app was removed or the token was replaced, so it can be dropped
var ErrUnregistered = errors.New("the push token is not registered any more")

// Message is a wake-up. It holds the two ids that the app needs to fetch the message from the API, and no text.
type Message struct {
	Token          string
	ConversationID string
	MessageID      int64
}

// Sender sends one message to one device
type Sender interface {
	Send(ctx context.Context, message Message) error
}

// serviceAccount is the part of the key file that the sender uses
type serviceAccount struct {
	ProjectID   string `json:"project_id"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

// FCM calls the HTTP v1 API of Firebase Cloud Messaging with the token of a service account
type FCM struct {
	endpoint string
	tokens   oauth2.TokenSource
	client   *http.Client
}

// NewFCM reads the key file of the service account. The file is read once at start, and the access tokens that Google gives for it
// are cached and renewed by the library.
func NewFCM(credentialsFile string) (*FCM, error) {
	data, err := os.ReadFile(credentialsFile) //nolint:gosec // the path is a setting of the operator
	if err != nil {
		return nil, fmt.Errorf("read the Firebase credentials file: %w", err)
	}

	var account serviceAccount

	err = json.Unmarshal(data, &account)
	if err != nil {
		return nil, errors.New("the Firebase credentials file is not valid JSON")
	}

	if account.ProjectID == "" || account.ClientEmail == "" || account.PrivateKey == "" {
		return nil, errors.New("the Firebase credentials file has no project_id, client_email or private_key, it is not a service account key")
	}

	if account.TokenURI == "" {
		account.TokenURI = "https://oauth2.googleapis.com/token"
	}

	config := &jwt.Config{
		Email: account.ClientEmail, PrivateKey: []byte(account.PrivateKey), Scopes: []string{scope}, TokenURL: account.TokenURI,
	}

	client := &http.Client{Timeout: requestTimeout}

	return &FCM{
		endpoint: fmt.Sprintf(defaultEndpoint, account.ProjectID),
		tokens:   oauth2.ReuseTokenSource(nil, config.TokenSource(context.WithValue(context.Background(), oauth2.HTTPClient, client))),
		client:   client,
	}, nil
}

// request is the body of the call. The data is the only thing that a person's phone gets, and the app builds the notification.
type request struct {
	Message struct {
		Token   string            `json:"token"`
		Data    map[string]string `json:"data"`
		Android struct {
			Priority    string `json:"priority"`
			CollapseKey string `json:"collapse_key"`
			TTL         string `json:"ttl"`
		} `json:"android"`
	} `json:"message"`
}

// errorBody is the part of an error answer of FCM that says why
type errorBody struct {
	Error struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Details []struct {
			ErrorCode string `json:"errorCode"`
		} `json:"details"`
	} `json:"error"`
}

func (f *FCM) Send(ctx context.Context, message Message) error {
	var body request

	body.Message.Token = message.Token
	body.Message.Data = map[string]string{"conversationId": message.ConversationID, "messageId": strconv.FormatInt(message.MessageID, 10)}
	body.Message.Android.Priority = "high"
	// a burst of messages of one conversation is one wake-up
	body.Message.Android.CollapseKey = "conversation-" + message.ConversationID
	body.Message.Android.TTL = ttl

	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	token, err := f.tokens.Token()
	if err != nil {
		return fmt.Errorf("get the Firebase access token: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

	res, err := f.client.Do(req)
	if err != nil {
		return fmt.Errorf("call Firebase: %w", err)
	}

	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= http.StatusOK && res.StatusCode < http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, maxResponse))

		return nil
	}

	raw, _ := io.ReadAll(io.LimitReader(res.Body, maxResponse))

	var failure errorBody

	_ = json.Unmarshal(raw, &failure)

	// Only the UNREGISTERED code proves that a token is dead, and the dispatcher deletes it on that word. A bare 404 can come
	// from a wrong project in the key file, a wrong endpoint or a proxy, and then every token is still good. Neither does a
	// malformed token or a payload that Firebase refuses delete anything.
	for _, detail := range failure.Error.Details {
		if detail.ErrorCode == "UNREGISTERED" {
			return ErrUnregistered
		}
	}

	return fmt.Errorf("firebase answered %d %s %s", res.StatusCode, failure.Error.Status, failure.Error.Message)
}

func isUnregistered(err error) bool {
	return errors.Is(err, ErrUnregistered)
}
