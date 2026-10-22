// Package chatlive delivers chat events to the sockets that are open on this process, and to the sockets of the other API
// copies through a bus such as Redis. It never decides who may see an event, the caller names the recipients. Delivery is only a
// shortcut, so a lost event costs nothing but a delay. The messages are in PostgreSQL and an app that missed one asks for it with
// the cursor.
package chatlive

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
)

// The kinds of event that a socket receives
const (
	EventMessage        = "message"
	EventMessageDeleted = "message.deleted"
	EventRead           = "read"
	EventTyping         = "typing"
	// EventResync tells a socket that events may have been lost, so the app asks the REST routes for what it missed
	EventResync = "resync"
)

// resyncFrame is the whole frame of a resync, which names no conversation because any of them may have lost an event
var resyncFrame = []byte(`{"type":"resync"}`)

// maxOrigins bounds the memory of the sequence numbers that other copies were last seen at, one entry for each start of a copy
const maxOrigins = 64

// resyncEvery is the shortest time between two resync frames of one process. A bus that flaps would otherwise make every app refetch
// its conversations again and again, so a gap that is found inside the time is sent once when it is over.
const resyncEvery = 5 * time.Second

// Event is what a socket is sent, as one JSON text frame. Only the fields of its type are set.
type Event struct {
	Type           string    `json:"type"`
	ConversationID uuid.UUID `json:"conversationId"`
	// Message is the new message, for a message event
	Message *dto.ChatMessage `json:"message,omitempty"`
	// MessageID is the deleted message, for a message.deleted event
	MessageID int64 `json:"messageId,omitempty"`
	// UserID is the person who read or who is typing, for a read and a typing event
	UserID *uuid.UUID `json:"userId,omitempty"`
	// LastReadMessageID is how far that person has read, for a read event
	LastReadMessageID int64 `json:"lastReadMessageId,omitempty"`
	// Typing tells whether the person started or stopped typing, for a typing event
	Typing *bool `json:"typing,omitempty"`
}

// ErrFull is returned when this process has no room for another socket, or is shutting down
var ErrFull = errors.New("the process has no room for another chat socket")

// sendBuffer is how many frames wait for a socket that is slow to read. A socket that falls further behind is closed, and the
// app that reconnects catches up with its cursor, so a slow phone never holds memory for the others.
const sendBuffer = 32

// Client is one open socket. The handler of the socket writes what arrives on Send and stops when Done is closed.
type Client struct {
	UserID uuid.UUID
	Send   chan []byte

	// opened orders the sockets of an account by when they were registered, so the oldest one is the one that makes room
	opened uint64
	done   chan struct{}
	once   sync.Once
}

// Done is closed when the hub no longer delivers to the client, because it was replaced, was too slow or the socket ended
func (c *Client) Done() <-chan struct{} { return c.done }

// Close stops the delivery to the client, and a second call changes nothing
func (c *Client) Close() { c.once.Do(func() { close(c.done) }) }

// Bus carries an event to the other API copies
type Bus interface {
	// Publish sends the payload to every copy, and the error may be ignored because a copy that is not reached only delivers late
	Publish(ctx context.Context, payload []byte) error
	// Run calls handle with every payload that arrives until the context ends, and reconnects by itself when the bus drops
	Run(ctx context.Context, handle func(payload []byte))
}

// Config sets the limits of one process
type Config struct {
	// MaxSockets is how many sockets one process holds, and MaxPerUser is how many one account may keep open at once
	MaxSockets int
	MaxPerUser int
	// Bus is optional, without it only the sockets of this process receive events
	Bus Bus
}

// envelope is what travels on the bus. Origin lets a copy skip what it published itself, because it delivered that locally.
type envelope struct {
	Origin string `json:"o"`
	// Seq counts what this origin published, from 1. A receiver that sees a number skipped knows that events were lost on the way
	// and tells its sockets to resync.
	Seq        uint64      `json:"n"`
	Recipients []uuid.UUID `json:"r"`
	Event      Event       `json:"e"`
}

// Hub keeps the open sockets of the process by account
type Hub struct {
	cfg    Config
	origin string

	mu      sync.RWMutex
	clients map[uuid.UUID]map[*Client]struct{}
	total   int
	counter uint64
	closed  bool

	// published numbers the envelopes of this process, and seen holds the last number of every other origin
	published atomic.Uint64
	seenMu    sync.Mutex
	seen      map[string]uint64

	// lastResync and resyncTimer keep the resync frames apart by resyncEvery, and resyncWait is that time, a field for the tests
	resyncMu    sync.Mutex
	lastResync  time.Time
	resyncTimer *time.Timer
	resyncWait  time.Duration

	gaps prometheus.Counter

	sockets  prometheus.Gauge
	events   *prometheus.CounterVec
	dropped  *prometheus.CounterVec
	refusals prometheus.Counter
}

func NewHub(cfg Config) *Hub {
	return &Hub{
		cfg:     cfg,
		origin:  uuid.NewString(),
		clients: map[uuid.UUID]map[*Client]struct{}{},
		seen:    map[string]uint64{},

		resyncWait: resyncEvery,
		gaps: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "kibooz", Name: "chat_event_gaps_total", Help: "Gaps found in the events that other copies published.",
		}),
		sockets: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "kibooz", Name: "chat_sockets", Help: "Chat sockets that are open on this process.",
		}),
		events: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "kibooz", Name: "chat_events_delivered_total", Help: "Events written to the sockets of this process, by type.",
		}, []string{"type"}),
		dropped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "kibooz", Name: "chat_sockets_dropped_total", Help: "Sockets closed by the hub, by reason.",
		}, []string{"reason"}),
		refusals: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "kibooz", Name: "chat_sockets_refused_total", Help: "Sockets refused because the process was full.",
		}),
	}
}

// Collectors are the metrics of the hub, for the registry that the metrics listener serves
func (h *Hub) Collectors() []prometheus.Collector {
	// the series exist from the start, so a reason that never happened still shows
	for _, kind := range []string{EventMessage, EventMessageDeleted, EventRead, EventTyping, EventResync} {
		h.events.WithLabelValues(kind)
	}

	for _, reason := range []string{"slow", "replaced"} {
		h.dropped.WithLabelValues(reason)
	}

	return []prometheus.Collector{h.sockets, h.events, h.dropped, h.refusals, h.gaps}
}

// Full reports whether the process has no room for another socket, or is shutting down
func (h *Hub) Full() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return h.closed || (h.cfg.MaxSockets > 0 && h.total >= h.cfg.MaxSockets)
}

// Shutdown stops taking sockets and closes the open ones, so each handler ends its socket with a going away frame and the apps
// reconnect to another copy. Closing a socket twice changes nothing.
func (h *Hub) Shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.closed = true

	for _, own := range h.clients {
		for client := range own {
			client.Close()
		}
	}
}

// Count is how many sockets are open on this process
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return h.total
}

// Register adds a socket for the account. When the account already has MaxPerUser sockets the oldest one is closed to make
// room, so a phone that reconnects before the server noticed the old connection is gone is never turned away.
func (h *Hub) Register(userID uuid.UUID) (*Client, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed || (h.cfg.MaxSockets > 0 && h.total >= h.cfg.MaxSockets) {
		h.refusals.Inc()

		return nil, ErrFull
	}

	h.counter++

	client := &Client{UserID: userID, Send: make(chan []byte, sendBuffer), opened: h.counter, done: make(chan struct{})}

	own := h.clients[userID]
	if own == nil {
		own = map[*Client]struct{}{}
		h.clients[userID] = own
	}

	// the oldest connection makes room, and the account keeps the newest ones
	for len(own) >= h.cfg.MaxPerUser && h.cfg.MaxPerUser > 0 {
		var oldest *Client

		for candidate := range own {
			if oldest == nil || candidate.opened < oldest.opened {
				oldest = candidate
			}
		}

		delete(own, oldest)
		h.total--

		oldest.Close()
		h.dropped.WithLabelValues("replaced").Inc()
	}

	own[client] = struct{}{}
	h.total++

	h.sockets.Set(float64(h.total))

	return client, nil
}

// Unregister removes a socket that ended, and a socket that was already removed is left alone
func (h *Hub) Unregister(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	own := h.clients[client.UserID]
	if _, present := own[client]; present {
		delete(own, client)
		h.total--
	}

	if len(own) == 0 {
		delete(h.clients, client.UserID)
	}

	client.Close()
	h.sockets.Set(float64(h.total))
}

// Publish delivers the event to the sockets of the recipients on this process and tells the other copies through the bus
func (h *Hub) Publish(recipients []uuid.UUID, event Event) {
	if len(recipients) == 0 {
		return
	}

	h.deliver(recipients, event)

	if h.cfg.Bus == nil {
		return
	}

	payload, err := json.Marshal(envelope{Origin: h.origin, Seq: h.published.Add(1), Recipients: recipients, Event: event})
	if err != nil {
		log.Printf("chat event could not be encoded for the bus %v", err)

		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// a bus that is down only means that the sockets of the other copies hear about it from the app that polls
	_ = h.cfg.Bus.Publish(ctx, payload)
}

// Run takes the events that other copies publish until the context ends. Without a bus it returns at once.
func (h *Hub) Run(ctx context.Context) {
	if h.cfg.Bus == nil {
		return
	}

	h.cfg.Bus.Run(ctx, func(payload []byte) {
		var received envelope

		if err := json.Unmarshal(payload, &received); err != nil {
			log.Printf("a chat event from the bus could not be read %v", err)

			return
		}

		if received.Origin == h.origin {
			return
		}

		if h.lostBefore(received) {
			h.gaps.Inc()
			log.Printf("chat events of another copy are missing before number %d, the sockets are told to resync", received.Seq)

			h.requestResync()
		}

		h.deliver(received.Recipients, received.Event)
	})
}

// lostBefore remembers the number of the envelope and tells whether the ones before it did not all arrive. The first envelope
// of an origin never counts as a loss, because a socket that this process holds was opened after the copy that sent it started,
// or it asks for what it missed when it connects, and a copy that started while this one could not hear the bus is a blind spot
// that the next gap makes up for. A number that is not above the last one is a late arrival of an envelope that the publisher
// numbered before another one but sent after it, and it is neither a loss nor a reason to go back.
func (h *Hub) lostBefore(received envelope) bool {
	h.seenMu.Lock()
	defer h.seenMu.Unlock()

	last, known := h.seen[received.Origin]

	if !known && len(h.seen) >= maxOrigins {
		// the copies that were restarted long ago are not coming back, so one entry makes room, and the others stay
		for origin := range h.seen {
			delete(h.seen, origin)

			break
		}
	}

	if received.Seq > last {
		h.seen[received.Origin] = received.Seq
	}

	return known && received.Seq > last+1
}

// requestResync sends a resync to the sockets of this process, at once when the last one was long enough ago, and otherwise
// once when the time is over, so any number of gaps inside it cost one frame
func (h *Hub) requestResync() {
	h.resyncMu.Lock()
	defer h.resyncMu.Unlock()

	if h.resyncTimer != nil {
		return
	}

	wait := h.resyncWait - time.Since(h.lastResync)
	if wait <= 0 {
		h.lastResync = time.Now()

		h.resync()

		return
	}

	h.resyncTimer = time.AfterFunc(wait, func() {
		h.resyncMu.Lock()
		h.resyncTimer = nil
		h.lastResync = time.Now()
		h.resyncMu.Unlock()

		h.resync()
	})
}

// resync tells every socket of this process that events may have been lost. It writes to a full buffer as little as deliver
// does, so a socket that is too slow is closed and catches up when it reconnects.
func (h *Hub) resync() {
	var slow []*Client

	h.mu.RLock()

	for _, own := range h.clients {
		for client := range own {
			select {
			case client.Send <- resyncFrame:
				h.events.WithLabelValues(EventResync).Inc()
			default:
				slow = append(slow, client)
			}
		}
	}

	h.mu.RUnlock()

	for _, client := range slow {
		h.dropped.WithLabelValues("slow").Inc()
		h.Unregister(client)
	}
}

// deliver writes the frame to every socket of every recipient on this process. A socket whose buffer is full is closed.
func (h *Hub) deliver(recipients []uuid.UUID, event Event) {
	frame, err := json.Marshal(event)
	if err != nil {
		log.Printf("chat event could not be encoded %v", err)

		return
	}

	var slow []*Client

	h.mu.RLock()

	for _, userID := range recipients {
		for client := range h.clients[userID] {
			select {
			case client.Send <- frame:
				h.events.WithLabelValues(event.Type).Inc()
			default:
				slow = append(slow, client)
			}
		}
	}

	h.mu.RUnlock()

	for _, client := range slow {
		h.dropped.WithLabelValues("slow").Inc()
		h.Unregister(client)
	}
}
