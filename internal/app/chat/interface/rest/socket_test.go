package rest

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/gofiber/fiber/v3"
	golangjwt "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/chat/usecase"
	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/chatlive"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/jwt"
	"github.com/SyafaHadyan/kibooz-backend/internal/middleware"
)

// testSecret is made at run time, so no key-shaped value sits in the source
var testSecret = strings.Repeat("k", 32)

// typingCall is what the socket asked the use case to do
type typingCall struct {
	user         uuid.UUID
	conversation uuid.UUID
	typing       bool
}

// fakeUseCase only answers typing, which is the only thing a socket asks of the use case
type fakeUseCase struct {
	usecase.ChatUseCaseItf

	mu    sync.Mutex
	calls []typingCall
	err   error
}

func (f *fakeUseCase) Typing(_ context.Context, userID uuid.UUID, conversationID uuid.UUID, typing bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, typingCall{userID, conversationID, typing})

	return f.err
}

func (f *fakeUseCase) recorded() []typingCall {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]typingCall(nil), f.calls...)
}

type fixture struct {
	addr   string
	hub    *chatlive.Hub
	use    *fakeUseCase
	tokens *jwt.JWT
}

func newFixture(t *testing.T, maxSockets int) fixture {
	t.Helper()

	cfg := &env.Env{JWTSecretKey: testSecret, JWTAccessExpiredMinutes: 60, ChatPhotoMaxMB: 4}
	tokens := jwt.New(cfg)
	hub := chatlive.NewHub(chatlive.Config{MaxSockets: maxSockets, MaxPerUser: 2})
	use := &fakeUseCase{}

	app := fiber.New(fiber.Config{ErrorHandler: func(c fiber.Ctx, err error) error {
		var appErr *apperror.Error
		if errors.As(err, &appErr) {
			return c.Status(appErr.Status).JSON(fiber.Map{"errorCode": appErr.Code})
		}

		var fiberErr *fiber.Error
		if errors.As(err, &fiberErr) {
			return c.Status(fiberErr.Code).JSON(fiber.Map{"errorCode": "FIBER"})
		}

		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"errorCode": "INTERNAL"})
	}})

	NewChatHandler(app.Group("/api/v1"), middleware.NewMiddleware(tokens, nil, nil, nil), use, hub, cfg)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() { _ = app.Listener(listener, fiber.ListenConfig{DisableStartupMessage: true}) }()

	t.Cleanup(func() { _ = app.ShutdownWithContext(context.Background()) })

	return fixture{addr: listener.Addr().String(), hub: hub, use: use, tokens: tokens}
}

func (f fixture) token(t *testing.T, id uuid.UUID, role constants.Role) string {
	t.Helper()

	token, err := f.tokens.GenerateToken(id, role)
	require.NoError(t, err)

	return token
}

func (f fixture) dial(token string) (*websocket.Conn, *http.Response, error) {
	header := http.Header{}
	if token != "" {
		header.Set("Authorization", "Bearer "+token)
	}

	return websocket.DefaultDialer.Dial("ws://"+f.addr+"/api/v1/chat/ws", header)
}

func readFrame(t *testing.T, conn *websocket.Conn) string {
	t.Helper()

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))

	_, data, err := conn.ReadMessage()
	require.NoError(t, err)

	return string(data)
}

func closeCodeOf(t *testing.T, conn *websocket.Conn) int {
	t.Helper()

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))

	for {
		_, _, err := conn.ReadMessage()
		if err == nil {
			continue
		}

		var closed *websocket.CloseError

		require.ErrorAs(t, err, &closed)

		return closed.Code
	}
}

func TestASocketStartsWithReadyThenRelaysWhatTheHubDelivers(t *testing.T) {
	f := newFixture(t, 10)
	user, other := uuid.New(), uuid.New()

	conn, _, err := f.dial(f.token(t, user, constants.RoleWali))
	require.NoError(t, err)

	defer func() { _ = conn.Close() }()

	ready := readFrame(t, conn)
	require.Contains(t, ready, `"type":"ready"`)
	require.Contains(t, ready, `"expiresAt"`)
	require.Equal(t, 1, f.hub.Count())

	conversation := uuid.New()
	f.hub.Publish([]uuid.UUID{user}, chatlive.Event{Type: chatlive.EventRead, ConversationID: conversation, UserID: &other, LastReadMessageID: 5})

	frame := readFrame(t, conn)
	require.Contains(t, frame, `"type":"read"`)
	require.Contains(t, frame, conversation.String())
	require.Contains(t, frame, `"lastReadMessageId":5`)

	require.NoError(t, conn.Close())
	require.Eventually(t, func() bool { return f.hub.Count() == 0 }, 5*time.Second, 10*time.Millisecond, "a closed socket leaves the hub")
}

func TestATypingFrameReachesTheUseCaseOnceForQuickRepeats(t *testing.T) {
	f := newFixture(t, 10)
	user, conversation := uuid.New(), uuid.New()

	conn, _, err := f.dial(f.token(t, user, constants.RoleGuru))
	require.NoError(t, err)

	defer func() { _ = conn.Close() }()

	readFrame(t, conn)

	frame := `{"type":"typing","conversationId":"` + conversation.String() + `","typing":true}`

	for range 3 {
		require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(frame)))
	}

	require.Eventually(t, func() bool { return len(f.use.recorded()) == 1 }, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, []typingCall{{user, conversation, true}}, f.use.recorded(), "the repeats within a second were dropped")

	// the opposite hint is not a repeat, so the stop is not lost
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"typing","conversationId":"`+conversation.String()+`","typing":false}`)))
	require.Eventually(t, func() bool { return len(f.use.recorded()) == 2 }, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, typingCall{user, conversation, false}, f.use.recorded()[1])

	// a refusal of the use case is ignored and does not end the socket
	f.use.mu.Lock()
	f.use.err = apperror.ErrChatConversationNotFound
	f.use.mu.Unlock()

	other := uuid.New()

	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"typing","conversationId":"`+other.String()+`","typing":false}`)))
	require.Eventually(t, func() bool { return len(f.use.recorded()) == 3 }, 5*time.Second, 10*time.Millisecond)

	f.hub.Publish([]uuid.UUID{user}, chatlive.Event{Type: chatlive.EventTyping, ConversationID: other})
	require.Contains(t, readFrame(t, conn), `"type":"typing"`)
}

func TestFramesThatMakeNoSenseAreDroppedAndTooManyEndTheSocket(t *testing.T) {
	f := newFixture(t, 10)

	conn, _, err := f.dial(f.token(t, uuid.New(), constants.RoleWali))
	require.NoError(t, err)

	defer func() { _ = conn.Close() }()

	readFrame(t, conn)

	for range maxBadFrames + 1 {
		require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"unknown"}`)))
		time.Sleep(210 * time.Millisecond)
	}

	require.Equal(t, websocket.ClosePolicyViolation, closeCodeOf(t, conn))
	require.Empty(t, f.use.recorded())
}

func TestAFloodOfFramesEndsTheSocket(t *testing.T) {
	f := newFixture(t, 10)

	conn, _, err := f.dial(f.token(t, uuid.New(), constants.RoleWali))
	require.NoError(t, err)

	defer func() { _ = conn.Close() }()

	readFrame(t, conn)

	for range 3 * frameBurst {
		if conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"unknown"}`)) != nil {
			break
		}
	}

	require.Equal(t, websocket.ClosePolicyViolation, closeCodeOf(t, conn))
}

func TestASocketClosesWithItsOwnCodeWhenTheTokenEnds(t *testing.T) {
	f := newFixture(t, 10)
	user := uuid.New()

	claims := struct {
		Role constants.Role `json:"role"`
		golangjwt.RegisteredClaims
	}{
		Role: constants.RoleGuru,
		RegisteredClaims: golangjwt.RegisteredClaims{
			Subject: user.String(), Issuer: constants.AccessTokenIssuer,
			IssuedAt: golangjwt.NewNumericDate(time.Now()), ExpiresAt: golangjwt.NewNumericDate(time.Now().Add(time.Second)),
		},
	}

	signed, err := golangjwt.NewWithClaims(golangjwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	require.NoError(t, err)

	conn, _, err := f.dial(signed)
	require.NoError(t, err)

	defer func() { _ = conn.Close() }()

	readFrame(t, conn)

	require.Equal(t, closeTokenExpired, closeCodeOf(t, conn))
	require.Eventually(t, func() bool { return f.hub.Count() == 0 }, 5*time.Second, 10*time.Millisecond)
}

func TestAShutdownSendsGoingAwayToEverySocket(t *testing.T) {
	f := newFixture(t, 10)

	conn, _, err := f.dial(f.token(t, uuid.New(), constants.RoleGuru))
	require.NoError(t, err)

	defer func() { _ = conn.Close() }()

	readFrame(t, conn)
	f.hub.Shutdown()

	require.Equal(t, websocket.CloseGoingAway, closeCodeOf(t, conn))

	_, res, err := f.dial(f.token(t, uuid.New(), constants.RoleGuru))
	require.Error(t, err)
	require.Equal(t, http.StatusServiceUnavailable, res.StatusCode, "a copy that is closing takes no new socket")
}

func TestTheUpgradeIsRefusedWithoutAnAccountOrARightRoleOrRoom(t *testing.T) {
	f := newFixture(t, 1)

	_, res, err := f.dial("")
	require.Error(t, err)
	require.Equal(t, http.StatusUnauthorized, res.StatusCode)

	_, res, err = f.dial(f.token(t, uuid.New(), constants.RoleAdmin))
	require.Error(t, err)
	require.Equal(t, http.StatusForbidden, res.StatusCode, "the chat is for teachers and parents")

	first, _, err := f.dial(f.token(t, uuid.New(), constants.RoleGuru))
	require.NoError(t, err)

	defer func() { _ = first.Close() }()

	readFrame(t, first)

	_, res, err = f.dial(f.token(t, uuid.New(), constants.RoleWali))
	require.Error(t, err)
	require.Equal(t, http.StatusServiceUnavailable, res.StatusCode, "a full copy answers before the upgrade")
}

func TestAPlainRequestToTheSocketRouteNeedsAnUpgrade(t *testing.T) {
	f := newFixture(t, 10)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+f.addr+"/api/v1/chat/ws", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+f.token(t, uuid.New(), constants.RoleGuru))

	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer func() { _ = res.Body.Close() }()

	require.Equal(t, http.StatusUpgradeRequired, res.StatusCode)
	require.Contains(t, res.Header.Get("Content-Type"), "json")
}

func TestIsRefusalTellsAnAnswerFromAFailure(t *testing.T) {
	require.True(t, isRefusal(apperror.ErrChatConversationNotFound))
	require.True(t, isRefusal(apperror.ErrChatSendingClosed))
	require.False(t, isRefusal(apperror.Internal(errors.New("database down"))))
	require.False(t, isRefusal(errors.New("plain")))
}
