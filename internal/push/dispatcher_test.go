package push

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	mu      sync.Mutex
	tokens  []TokenRow
	deleted []string
	err     error
}

func (f *fakeStore) TokensOf(_ context.Context, userIDs []uuid.UUID) ([]TokenRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.err != nil {
		return nil, f.err
	}

	var found []TokenRow

	for _, row := range f.tokens {
		for _, id := range userIDs {
			if row.UserID == id {
				found = append(found, row)
			}
		}
	}

	return found, nil
}

func (f *fakeStore) DeleteTokenValues(_ context.Context, tokens []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.deleted = append(f.deleted, tokens...)

	return nil
}

func (f *fakeStore) removed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.deleted...)
}

type fakeSender struct {
	mu       sync.Mutex
	sent     []Message
	failures map[string]error
}

func (f *fakeSender) Send(_ context.Context, message Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.failures[message.Token]; err != nil {
		return err
	}

	f.sent = append(f.sent, message)

	return nil
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.sent)
}

func result(d *Dispatcher, name string) float64 {
	return testutil.ToFloat64(d.results.WithLabelValues(name))
}

func TestEveryDeviceOfTheRecipientsIsWokenAndAStaleTokenIsRemoved(t *testing.T) {
	parent, other, outsider := uuid.New(), uuid.New(), uuid.New()
	store := &fakeStore{tokens: []TokenRow{
		{parent, "phone-1"}, {parent, "phone-2"}, {other, "stale"}, {other, "broken"}, {outsider, "never"},
	}}
	sender := &fakeSender{failures: map[string]error{
		"stale": fmt.Errorf("wrapped %w", ErrUnregistered), "broken": errors.New("firebase answered 503"),
	}}
	dispatcher := NewDispatcher(store, sender)

	dispatcher.Start()
	dispatcher.NewMessage([]uuid.UUID{parent, other}, uuid.MustParse("6f1c1a52-0c55-4d6c-a4a9-8f54a39bd1a8"), 77)
	dispatcher.Stop()

	require.Equal(t, 2, sender.count())

	for _, message := range sender.sent {
		require.Equal(t, int64(77), message.MessageID)
		require.Equal(t, "6f1c1a52-0c55-4d6c-a4a9-8f54a39bd1a8", message.ConversationID)
		require.NotEqual(t, "never", message.Token, "only the recipients are woken")
	}

	require.Equal(t, []string{"stale"}, store.removed(), "a token that Firebase dropped goes, one that failed once stays")
	require.EqualValues(t, 2, result(dispatcher, "sent"))
	require.EqualValues(t, 1, result(dispatcher, "unregistered"))
	require.EqualValues(t, 1, result(dispatcher, "failed"))
}

func TestAFailureToReadTheTokensIsCountedAndNothingIsSent(t *testing.T) {
	store := &fakeStore{err: errors.New("database down")}
	sender := &fakeSender{}
	dispatcher := NewDispatcher(store, sender)

	dispatcher.Start()
	dispatcher.NewMessage([]uuid.UUID{uuid.New()}, uuid.New(), 1)
	dispatcher.Stop()

	require.Zero(t, sender.count())
	require.EqualValues(t, 1, result(dispatcher, "failed"))
}

func TestNobodyToWakeQueuesNothingAndAFullQueueDropsTheRest(t *testing.T) {
	dispatcher := NewDispatcher(&fakeStore{}, &fakeSender{})

	dispatcher.NewMessage(nil, uuid.New(), 1)
	require.Empty(t, dispatcher.queue)

	// no worker runs, so the queue fills up and the next wake-up is dropped
	for range queueSize {
		dispatcher.NewMessage([]uuid.UUID{uuid.New()}, uuid.New(), 1)
	}

	require.Len(t, dispatcher.queue, queueSize)

	dispatcher.NewMessage([]uuid.UUID{uuid.New()}, uuid.New(), 1)
	require.EqualValues(t, 1, result(dispatcher, "dropped"))
}

func TestAWakeUpAfterTheShutdownIsDroppedAndStoppingTwiceIsSafe(t *testing.T) {
	dispatcher := NewDispatcher(&fakeStore{}, &fakeSender{})

	dispatcher.Start()
	dispatcher.Stop()
	dispatcher.Stop()

	dispatcher.NewMessage([]uuid.UUID{uuid.New()}, uuid.New(), 1)
	require.EqualValues(t, 1, result(dispatcher, "dropped"))
}

func TestTheShutdownDoesNotWaitForeverForAStuckSend(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	store := &fakeStore{tokens: []TokenRow{{uuid.Nil, "t"}}}
	dispatcher := NewDispatcher(store, blockingSender{release: release, started: started})

	dispatcher.Start()
	dispatcher.NewMessage([]uuid.UUID{uuid.Nil}, uuid.New(), 1)

	<-started

	finished := make(chan struct{})

	go func() {
		dispatcher.Stop()
		close(finished)
	}()

	select {
	case <-finished:
		require.FailNow(t, "the shutdown did not wait for a send that is under way")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)

	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the shutdown did not finish once the send ended")
	}
}

type blockingSender struct {
	release chan struct{}
	started chan struct{}
}

func (b blockingSender) Send(context.Context, Message) error {
	b.started <- struct{}{}

	<-b.release

	return nil
}

func TestTheCollectorsListTheMetricsOfTheDispatcher(t *testing.T) {
	dispatcher := NewDispatcher(&fakeStore{}, &fakeSender{})

	require.Len(t, dispatcher.Collectors(), 1)
	require.Equal(t, 4, testutil.CollectAndCount(dispatcher.results), "every result has a series from the start")
}
