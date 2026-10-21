package push

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	// queueSize is how many wake-ups wait for a worker. A burst that is larger drops the rest, which only costs a late
	// notification, because the app catches up when it opens.
	queueSize = 1000
	// workers is how many wake-ups are sent at the same time
	workers = 4
	// jobTimeout bounds the work for one message, the lookup of the tokens and every call to FCM
	jobTimeout = 30 * time.Second
	// stopTimeout is how long the shutdown waits for the wake-ups that are under way
	stopTimeout = 10 * time.Second
)

// TokenRow is a push token with the account it belongs to
type TokenRow struct {
	UserID uuid.UUID
	Token  string
}

// TokenStore is where the tokens of the accounts live
type TokenStore interface {
	// TokensOf returns the tokens of the accounts
	TokensOf(ctx context.Context, userIDs []uuid.UUID) ([]TokenRow, error)
	// DeleteTokenValues removes tokens that FCM no longer knows
	DeleteTokenValues(ctx context.Context, tokens []string) error
}

type job struct {
	recipients     []uuid.UUID
	conversationID uuid.UUID
	messageID      int64
}

// Dispatcher turns a stored message into wake-ups for the devices of the people it is for. It never blocks the request that
// stored the message and never fails it, a wake-up is a shortcut for the app that would otherwise find the message when it opens.
type Dispatcher struct {
	store  TokenStore
	sender Sender

	// mu keeps a wake-up from being queued while the shutdown closes the queue
	mu     sync.RWMutex
	closed bool
	queue  chan job
	wg     sync.WaitGroup

	results *prometheus.CounterVec
}

func NewDispatcher(store TokenStore, sender Sender) *Dispatcher {
	return &Dispatcher{
		store: store, sender: sender, queue: make(chan job, queueSize),
		results: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "kibooz", Name: "push_messages_total", Help: "Wake-up pushes, by result.",
		}, []string{"result"}),
	}
}

// Collectors are the metrics of the dispatcher
func (d *Dispatcher) Collectors() []prometheus.Collector {
	// the series exist from the start, so a result that never happened still shows
	for _, result := range []string{"sent", "failed", "unregistered", "dropped"} {
		d.results.WithLabelValues(result)
	}

	return []prometheus.Collector{d.results}
}

// Start runs the workers, and Stop ends them
func (d *Dispatcher) Start() {
	for range workers {
		d.wg.Go(d.work)
	}
}

// Stop takes no more wake-ups and waits a short time for the ones under way
func (d *Dispatcher) Stop() {
	d.mu.Lock()

	if !d.closed {
		d.closed = true

		close(d.queue)
	}

	d.mu.Unlock()

	finished := make(chan struct{})

	go func() {
		d.wg.Wait()
		close(finished)
	}()

	select {
	case <-finished:
	case <-time.After(stopTimeout):
		log.Println("push workers were still busy at the shutdown, their wake-ups are dropped")
	}
}

// NewMessage queues the wake-ups of a message for the recipients. It returns at once.
func (d *Dispatcher) NewMessage(recipients []uuid.UUID, conversationID uuid.UUID, messageID int64) {
	if len(recipients) == 0 {
		return
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	// a message that arrives during the shutdown, or when the queue is full, is dropped
	if d.closed {
		d.results.WithLabelValues("dropped").Inc()

		return
	}

	select {
	case d.queue <- job{recipients: recipients, conversationID: conversationID, messageID: messageID}:
	default:
		d.results.WithLabelValues("dropped").Inc()
	}
}

func (d *Dispatcher) work() {
	for next := range d.queue {
		d.deliver(next)
	}
}

func (d *Dispatcher) deliver(next job) {
	ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
	defer cancel()

	tokens, err := d.store.TokensOf(ctx, next.recipients)
	if err != nil {
		log.Printf("the push tokens could not be read %v", err)
		d.results.WithLabelValues("failed").Inc()

		return
	}

	var stale []string

	for _, row := range tokens {
		err = d.sender.Send(ctx, Message{Token: row.Token, ConversationID: next.conversationID.String(), MessageID: next.messageID})

		switch {
		case err == nil:
			d.results.WithLabelValues("sent").Inc()
		case isUnregistered(err):
			d.results.WithLabelValues("unregistered").Inc()

			stale = append(stale, row.Token)
		default:
			d.results.WithLabelValues("failed").Inc()
			log.Printf("a push could not be sent %v", err)
		}
	}

	if len(stale) == 0 {
		return
	}

	err = d.store.DeleteTokenValues(ctx, stale)
	if err != nil {
		log.Printf("the stale push tokens could not be removed %v", err)
	}
}
