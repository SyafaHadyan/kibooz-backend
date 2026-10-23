package e2e

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/gofiber/fiber/v3"
	golangjwt "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/chatlive"
	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/redis"
)

var (
	liveOnce sync.Once
	liveURL  string
)

// liveServer serves the app on a real port, because a WebSocket cannot be upgraded through app.Test. It starts once for the
// whole run, and the other tests keep using app.Test.
func liveServer(t *testing.T) string {
	t.Helper()

	handler := app(t)

	liveOnce.Do(func() {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen for the socket tests: %v", err)
		}

		go func() { _ = handler.Listener(listener, fiber.ListenConfig{DisableStartupMessage: true}) }()

		liveURL = listener.Addr().String()
	})

	require.NotEmpty(t, liveURL)

	return liveURL
}

// socket is an open chat socket with a reader that keeps the frames in order
type socket struct {
	conn   *websocket.Conn
	frames chan map[string]any
	closed chan int
}

func dialChat(t *testing.T, token string) (*socket, *http.Response, error) {
	t.Helper()

	header := http.Header{}
	if token != "" {
		header.Set("Authorization", "Bearer "+token)
	}

	conn, res, err := websocket.DefaultDialer.Dial("ws://"+liveServer(t)+"/api/v1/chat/ws", header)
	if err != nil {
		return nil, res, err
	}

	s := &socket{conn: conn, frames: make(chan map[string]any, 64), closed: make(chan int, 1)}

	go func() {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				code := -1

				var closeErr *websocket.CloseError
				if asClose(err, &closeErr) {
					code = closeErr.Code
				}

				s.closed <- code

				return
			}

			var frame map[string]any

			if json.Unmarshal(data, &frame) == nil {
				s.frames <- frame
			}
		}
	}()

	t.Cleanup(func() { _ = conn.Close() })

	return s, res, nil
}

func asClose(err error, target **websocket.CloseError) bool {
	closeErr, ok := err.(*websocket.CloseError) //nolint:errorlint // the reader returns the close error itself

	if ok {
		*target = closeErr
	}

	return ok
}

func (s *socket) next(t *testing.T) map[string]any {
	t.Helper()

	select {
	case frame := <-s.frames:
		return frame
	case code := <-s.closed:
		require.FailNow(t, "the socket closed", "code %d", code)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "no frame arrived")
	}

	return nil
}

func (s *socket) expectQuiet(t *testing.T) {
	t.Helper()

	select {
	case frame := <-s.frames:
		require.FailNow(t, "a frame arrived that should not have", "%v", frame)
	case <-time.After(300 * time.Millisecond):
	}
}

func (s *socket) send(t *testing.T, frame map[string]any) {
	t.Helper()

	require.NoError(t, s.conn.WriteJSON(frame))
}

func TestChatSocketDeliversMessagesDeletesReadMarksAndTyping(t *testing.T) {
	guru := registerGuru(t, "Flamboyan")
	classID, code := classOf(t, guru)
	parent, _ := registerWali(t, code, "Live Child")
	other, _ := registerWali(t, code, "Other Live Child")

	parentID := chatContactOf(t, call(t, http.MethodGet, chatBase+"/contacts", guru.Token, nil), "Live Child")["id"].(string)
	guruID := chatContactOf(t, call(t, http.MethodGet, chatBase+"/contacts", parent.Token, nil), "")["id"].(string)

	direct := call(t, http.MethodPost, chatBase+"/conversations", parent.Token, map[string]any{"userId": guruID})
	require.Equal(t, http.StatusCreated, direct.Status, "body %v", direct.Body)

	directID := direct.data("id").(string)

	room := call(t, http.MethodPost, chatBase+"/conversations", guru.Token, map[string]any{"classId": classID})
	require.Equal(t, http.StatusCreated, room.Status, "body %v", room.Body)

	roomID := room.data("id").(string)

	send := func(token string, conversation string, body string) result {
		res, _ := callKey(t, http.MethodPost, chatBase+"/conversations/"+conversation+"/messages", token, uuid.NewString(), map[string]any{"body": body})
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

		return res
	}

	teacherSocket, _, err := dialChat(t, guru.Token)
	require.NoError(t, err)

	parentSocket, _, err := dialChat(t, parent.Token)
	require.NoError(t, err)

	otherSocket, _, err := dialChat(t, other.Token)
	require.NoError(t, err)

	t.Run("a socket starts with a ready frame that says when the token ends", func(t *testing.T) {
		for _, s := range []*socket{teacherSocket, parentSocket, otherSocket} {
			frame := s.next(t)

			require.Equal(t, "ready", frame["type"])
			require.NotEmpty(t, frame["expiresAt"])
		}
	})

	t.Run("a private message reaches both people and nobody else", func(t *testing.T) {
		res := send(parent.Token, directID, "Good morning, Ma'am")

		for _, s := range []*socket{parentSocket, teacherSocket} {
			frame := s.next(t)

			require.Equal(t, "message", frame["type"])
			require.Equal(t, directID, frame["conversationId"])
			require.Equal(t, "Good morning, Ma'am", frame["message"].(map[string]any)["body"])
			require.EqualValues(t, messageIDOf(t, res), frame["message"].(map[string]any)["id"])
		}

		otherSocket.expectQuiet(t)
	})

	t.Run("a message of the room reaches every member", func(t *testing.T) {
		send(guru.Token, roomID, "Hello class")

		for _, s := range []*socket{teacherSocket, parentSocket, otherSocket} {
			frame := s.next(t)

			require.Equal(t, "message", frame["type"])
			require.Equal(t, roomID, frame["conversationId"])
			require.Equal(t, "Hello class", frame["message"].(map[string]any)["body"])
		}
	})

	t.Run("typing goes to the other person of a private thread only", func(t *testing.T) {
		parentSocket.send(t, map[string]any{"type": "typing", "conversationId": directID, "typing": true})

		frame := teacherSocket.next(t)

		require.Equal(t, "typing", frame["type"])
		require.Equal(t, directID, frame["conversationId"])
		require.Equal(t, parentID, frame["userId"])
		require.Equal(t, true, frame["typing"])

		parentSocket.expectQuiet(t)
		otherSocket.expectQuiet(t)

		// a room shows no typing, a conversation of somebody else is ignored, and frames that make no sense are dropped
		parentSocket.send(t, map[string]any{"type": "typing", "conversationId": roomID, "typing": true})
		otherSocket.send(t, map[string]any{"type": "typing", "conversationId": directID, "typing": true})
		parentSocket.send(t, map[string]any{"type": "unknown"})
		parentSocket.send(t, map[string]any{"type": "typing", "conversationId": "not-a-uuid"})

		teacherSocket.expectQuiet(t)
		parentSocket.expectQuiet(t)
	})

	t.Run("a read mark reaches both people of a private thread", func(t *testing.T) {
		window := call(t, http.MethodGet, chatBase+"/conversations/"+directID+"/messages", guru.Token, nil)
		last := idsOf(t, window)[0]

		res := call(t, http.MethodPut, chatBase+"/conversations/"+directID+"/read", guru.Token, map[string]any{"messageId": last})
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		for _, s := range []*socket{teacherSocket, parentSocket} {
			frame := s.next(t)

			require.Equal(t, "read", frame["type"])
			require.Equal(t, guruID, frame["userId"])
			require.EqualValues(t, last, frame["lastReadMessageId"])
		}

		otherSocket.expectQuiet(t)
	})

	t.Run("deleting a message tells the conversation once", func(t *testing.T) {
		res := send(parent.Token, directID, "Oops")

		parentSocket.next(t)
		teacherSocket.next(t)

		id := messageIDOf(t, res)
		path := chatBase + "/conversations/" + directID + "/messages/" + itoa(id)

		require.Equal(t, http.StatusOK, call(t, http.MethodDelete, path, parent.Token, nil).Status)

		for _, s := range []*socket{parentSocket, teacherSocket} {
			frame := s.next(t)

			require.Equal(t, "message.deleted", frame["type"])
			require.EqualValues(t, id, frame["messageId"])
		}

		require.Equal(t, http.StatusOK, call(t, http.MethodDelete, path, parent.Token, nil).Status)
		parentSocket.expectQuiet(t)
	})
}

func TestChatSocketRefusesWhatIsNotAnAuthenticatedUpgrade(t *testing.T) {
	guru := registerGuru(t, "Anyelir")

	_, res, err := dialChat(t, "")
	require.Error(t, err)
	require.Equal(t, http.StatusUnauthorized, res.StatusCode)

	_, res, err = dialChat(t, "not-a-token")
	require.Error(t, err)
	require.Equal(t, http.StatusUnauthorized, res.StatusCode)

	// a plain request, with a good token and without the upgrade headers
	plain := call(t, http.MethodGet, chatBase+"/ws", guru.Token, nil)
	plain.requireError(t, http.StatusUpgradeRequired, "UPGRADE_REQUIRED")
}

func TestChatSocketClosesWhenTheAccessTokenEnds(t *testing.T) {
	guru := registerGuru(t, "Teratai")
	_, code := classOf(t, guru)
	parent, _ := registerWali(t, code, "Short Token Child")

	guruID := chatContactOf(t, call(t, http.MethodGet, chatBase+"/contacts", parent.Token, nil), "")["id"].(string)

	claims := struct {
		Role constants.Role `json:"role"`
		golangjwt.RegisteredClaims
	}{
		Role: constants.RoleGuru,
		RegisteredClaims: golangjwt.RegisteredClaims{
			Subject: guruID, Issuer: constants.AccessTokenIssuer,
			IssuedAt: golangjwt.NewNumericDate(time.Now()), ExpiresAt: golangjwt.NewNumericDate(time.Now().Add(2 * time.Second)),
		},
	}

	signed, err := golangjwt.NewWithClaims(golangjwt.SigningMethodHS256, claims).SignedString([]byte(os.Getenv("JWT_SECRET_KEY")))
	require.NoError(t, err)

	s, _, err := dialChat(t, signed)
	require.NoError(t, err)
	require.Equal(t, "ready", s.next(t)["type"])

	select {
	case code := <-s.closed:
		require.Equal(t, 4001, code, "the app knows to refresh the token and connect again")
	case <-time.After(10 * time.Second):
		require.FailNow(t, "the socket stayed open after its token ended")
	}
}

func TestChatSocketKeepsFiveSocketsForOneAccount(t *testing.T) {
	guru := registerGuru(t, "Mawar")

	sockets := make([]*socket, 0, 6)

	for range 6 {
		s, _, err := dialChat(t, guru.Token)
		require.NoError(t, err)
		require.Equal(t, "ready", s.next(t)["type"])

		sockets = append(sockets, s)
	}

	// the sixth connection made room by closing the oldest one
	select {
	case <-sockets[0].closed:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the oldest socket was not closed")
	}

	require.True(t, strings.HasPrefix(liveURL, "127.0.0.1:"))
}

func TestTwoCopiesShareChatEventsThroughRedis(t *testing.T) {
	app(t)

	cfg, err := env.New()
	require.NoError(t, err)

	first := redis.New(cfg)
	second := redis.New(cfg)

	t.Cleanup(func() {
		_ = first.Close()
		_ = second.Close()
	})

	one := chatlive.NewHub(chatlive.Config{MaxSockets: 10, MaxPerUser: 2, Bus: chatlive.NewRedisBus(first)})
	two := chatlive.NewHub(chatlive.Config{MaxSockets: 10, MaxPerUser: 2, Bus: chatlive.NewRedisBus(second)})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go one.Run(ctx)
	go two.Run(ctx)

	reader, writer := uuid.New(), uuid.New()

	onTwo, err := two.Register(reader)
	require.NoError(t, err)

	onOne, err := one.Register(writer)
	require.NoError(t, err)

	conversation := uuid.New()

	// the subscriptions need a moment to start, so the event is published until the other copy has it
	require.Eventually(t, func() bool {
		one.Publish([]uuid.UUID{reader, writer}, chatlive.Event{Type: chatlive.EventTyping, ConversationID: conversation})

		select {
		case frame := <-onTwo.Send:
			return strings.Contains(string(frame), conversation.String())
		case <-time.After(100 * time.Millisecond):
			return false
		}
	}, 10*time.Second, 50*time.Millisecond, "the other copy delivers it to its own socket")

	select {
	case frame := <-onOne.Send:
		require.Contains(t, string(frame), conversation.String(), "the copy that published delivered it locally")
	case <-time.After(time.Second):
		require.FailNow(t, "the publishing copy did not deliver to its own socket")
	}
}
