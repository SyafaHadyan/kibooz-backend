package rest

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/gofiber/contrib/v3/websocket"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/chatlive"
	"github.com/SyafaHadyan/kibooz-backend/internal/middleware"
)

const (
	// pingEvery is how often the server pings a socket, and pongWait is how long it waits for any sign of life before it closes
	// the socket. An app that answers the pings, which every WebSocket client does by itself, stays connected while it is idle.
	pingEvery = 25 * time.Second
	pongWait  = 60 * time.Second
	// writeWait bounds one write, so a peer that stopped reading cannot hold the goroutine for ever
	writeWait = 10 * time.Second
	// maxFrame is the largest frame that a client may send. Its frames are a few dozen bytes, so anything bigger is not ours.
	maxFrame = 1024
	// closeTokenExpired tells the app that its access token ended, so it refreshes the token and connects again
	closeTokenExpired = 4001
	// maxBadFrames is how many frames that cannot be read a socket may send before it is closed
	maxBadFrames = 20
	// typingEvery is the shortest time between two typing frames of one conversation that are acted on
	typingEvery = time.Second
	// frameBurst and frameRate are the budget of one socket, so a client cannot turn typing frames into a flood of queries
	frameBurst = 10
	frameRate  = 5.0
	// typingTimeout bounds the database work of one typing frame
	typingTimeout = 5 * time.Second
	// maxTypingConversations bounds what the socket remembers about its typing frames
	maxTypingConversations = 50

	// the keys under which the upgrade handler hands the account to the socket handler, which only sees string keys
	localUser    = "chatUser"
	localExpires = "chatExpires"
)

// ready is the first frame of a socket. expiresAt is when the access token ends and the server closes the socket, the zero time
// is left out for a token without an end.
type ready struct {
	Type      string     `json:"type"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// clientFrame is what an app sends over the socket. Only typing exists, everything else is REST.
type clientFrame struct {
	Type           string    `json:"type"`
	ConversationID uuid.UUID `json:"conversationId"`
	Typing         bool      `json:"typing"`
}

// UpgradeSocket checks the account and the room before the connection is upgraded, so a refusal is a plain HTTP answer
func (h *ChatHandler) UpgradeSocket(c fiber.Ctx) error {
	if !websocket.IsWebSocketUpgrade(c) {
		return fiber.ErrUpgradeRequired
	}

	if h.hub.Full() {
		return apperror.ErrChatSocketsFull
	}

	c.Locals(localUser, middleware.UserIDFrom(c))
	c.Locals(localExpires, middleware.ExpiresAtFrom(c))

	return c.Next()
}

// Socket serves one connection until it ends. The app sends REST requests for everything it writes and only uses the socket to
// listen and to tell the other person that it is typing.
func (h *ChatHandler) Socket(conn *websocket.Conn) {
	userID, ok := conn.Locals(localUser).(uuid.UUID)
	if !ok {
		return
	}

	expires, _ := conn.Locals(localExpires).(time.Time)

	client, err := h.hub.Register(userID)
	if err != nil {
		_ = conn.WriteControl(
			websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "busy"), time.Now().Add(writeWait),
		)
		_ = conn.Close()

		return
	}

	defer h.hub.Unregister(client)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn.SetReadLimit(maxFrame)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(pongWait)) })

	written := make(chan struct{})

	go func() {
		defer close(written)

		h.write(conn, client, expires)
	}()

	h.read(ctx, conn, client)

	// the writer ends when the client is closed, and it sends the closing frame on its way out
	client.Close()
	<-written

	_ = conn.Close()
}

// write sends what the hub delivers and the pings, and closes the socket when the client is done or the token ends
func (h *ChatHandler) write(conn *websocket.Conn, client *chatlive.Client, expires time.Time) {
	send := func(messageType int, data []byte) bool {
		_ = conn.SetWriteDeadline(time.Now().Add(writeWait))

		return conn.WriteMessage(messageType, data) == nil
	}

	greeting := ready{Type: "ready"}
	if !expires.IsZero() {
		greeting.ExpiresAt = &expires
	}

	frame, _ := json.Marshal(greeting)
	if !send(websocket.TextMessage, frame) {
		return
	}

	var expired <-chan time.Time

	if !expires.IsZero() {
		timer := time.NewTimer(time.Until(expires))
		defer timer.Stop()

		expired = timer.C
	}

	ticker := time.NewTicker(pingEvery)
	defer ticker.Stop()

	closeWith := func(code int, reason string) {
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(writeWait))
	}

	for {
		select {
		case <-client.Done():
			closeWith(websocket.CloseGoingAway, "going away")

			return
		case <-expired:
			closeWith(closeTokenExpired, "token expired")

			return
		case frame := <-client.Send:
			if !send(websocket.TextMessage, frame) {
				return
			}
		case <-ticker.C:
			if conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait)) != nil {
				return
			}
		}
	}
}

// read handles what the app sends until the socket ends. Its frames are typing hints, and a hint that is refused is dropped.
func (h *ChatHandler) read(ctx context.Context, conn *websocket.Conn, client *chatlive.Client) {
	var (
		bad       int
		tokens    = float64(frameBurst)
		refilled  = time.Now()
		lastTyped = map[uuid.UUID]time.Time{}
	)

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}

		// a token bucket per socket, so a client cannot turn typing frames into a stream of database queries
		now := time.Now()
		tokens = min(frameBurst, tokens+now.Sub(refilled).Seconds()*frameRate)
		refilled = now

		if tokens < 1 {
			_ = conn.WriteControl(
				websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "too many frames"), now.Add(writeWait),
			)

			return
		}

		tokens--

		var frame clientFrame

		if err := json.Unmarshal(data, &frame); err != nil || frame.Type != "typing" || frame.ConversationID == uuid.Nil {
			bad++
			if bad > maxBadFrames {
				_ = conn.WriteControl(
					websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "bad frames"), now.Add(writeWait),
				)

				return
			}

			continue
		}

		if last, seen := lastTyped[frame.ConversationID]; seen && now.Sub(last) < typingEvery {
			continue
		}

		if len(lastTyped) >= maxTypingConversations {
			clear(lastTyped)
		}

		lastTyped[frame.ConversationID] = now

		// the reader does not read the socket while a query waits, so a query that stalls must end before the read deadline does
		typingCtx, cancelTyping := context.WithTimeout(ctx, typingTimeout)
		err = h.useCase.Typing(typingCtx, client.UserID, frame.ConversationID, frame.Typing)

		cancelTyping()

		if err != nil && !isRefusal(err) {
			log.Printf("a typing frame failed %v", err)
		}
	}
}

// isRefusal tells an answer that means the frame was not allowed, which a socket ignores, from a failure worth a log line
func isRefusal(err error) bool {
	var appErr *apperror.Error

	return errors.As(err, &appErr) && appErr.Status < 500
}
