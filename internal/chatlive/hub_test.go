package chatlive

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
)

// fakeBus connects hubs in memory, the way Redis connects API copies
type fakeBus struct {
	mu        sync.Mutex
	handlers  []func([]byte)
	fail      bool
	published int
}

func (b *fakeBus) Publish(_ context.Context, payload []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.published++

	if b.fail {
		return context.DeadlineExceeded
	}

	for _, handle := range b.handlers {
		handle(payload)
	}

	return nil
}

func (b *fakeBus) Run(ctx context.Context, handle func([]byte)) {
	b.mu.Lock()
	b.handlers = append(b.handlers, handle)
	b.mu.Unlock()

	<-ctx.Done()
}

func received(t *testing.T, client *Client) Event {
	t.Helper()

	select {
	case frame := <-client.Send:
		var event Event

		require.NoError(t, json.Unmarshal(frame, &event))

		return event
	case <-time.After(time.Second):
		require.FailNow(t, "no event arrived")
	}

	return Event{}
}

func nothing(t *testing.T, client *Client) {
	t.Helper()

	select {
	case frame := <-client.Send:
		require.FailNow(t, "an event arrived", string(frame))
	case <-time.After(30 * time.Millisecond):
	}
}

func TestAnEventReachesOnlyTheSocketsOfItsRecipients(t *testing.T) {
	hub := NewHub(Config{MaxSockets: 10, MaxPerUser: 3})
	alice, bob, carol := uuid.New(), uuid.New(), uuid.New()

	first, err := hub.Register(alice)
	require.NoError(t, err)

	second, err := hub.Register(alice)
	require.NoError(t, err)

	other, err := hub.Register(bob)
	require.NoError(t, err)

	outsider, err := hub.Register(carol)
	require.NoError(t, err)

	conversation := uuid.New()
	hub.Publish([]uuid.UUID{alice, bob}, Event{Type: EventMessage, ConversationID: conversation, Message: &dto.ChatMessage{ID: 7, Body: "hi"}})

	for _, client := range []*Client{first, second, other} {
		event := received(t, client)
		require.Equal(t, EventMessage, event.Type)
		require.Equal(t, conversation, event.ConversationID)
		require.EqualValues(t, 7, event.Message.ID)
	}

	nothing(t, outsider)
	require.Equal(t, 4, hub.Count())
}

func TestTheOldestSocketMakesRoomWhenAnAccountHasTooMany(t *testing.T) {
	hub := NewHub(Config{MaxSockets: 10, MaxPerUser: 2})
	user := uuid.New()

	clients := make([]*Client, 0, 3)

	for range 3 {
		client, err := hub.Register(user)
		require.NoError(t, err)

		clients = append(clients, client)
	}

	require.Equal(t, 2, hub.Count())

	select {
	case <-clients[0].Done():
	default:
		require.FailNow(t, "the oldest socket was not closed")
	}

	for _, client := range clients[1:] {
		select {
		case <-client.Done():
			require.FailNow(t, "a newer socket was closed")
		default:
		}
	}

	// the next connection closes the oldest one that is left
	next, err := hub.Register(user)
	require.NoError(t, err)

	select {
	case <-clients[1].Done():
	default:
		require.FailNow(t, "the second oldest socket was not closed")
	}

	select {
	case <-next.Done():
		require.FailNow(t, "the new socket was closed")
	default:
	}
}

func TestAFullProcessRefusesANewSocketAndAFreedSlotWorksAgain(t *testing.T) {
	hub := NewHub(Config{MaxSockets: 2, MaxPerUser: 5})

	first, err := hub.Register(uuid.New())
	require.NoError(t, err)

	_, err = hub.Register(uuid.New())
	require.NoError(t, err)
	require.True(t, hub.Full())

	_, err = hub.Register(uuid.New())
	require.ErrorIs(t, err, ErrFull)

	hub.Unregister(first)
	hub.Unregister(first)

	require.Equal(t, 1, hub.Count(), "removing a socket twice counts it once")
	require.False(t, hub.Full())

	_, err = hub.Register(uuid.New())
	require.NoError(t, err)
}

func TestASlowSocketIsClosedAndTheOthersKeepReceiving(t *testing.T) {
	hub := NewHub(Config{MaxSockets: 10, MaxPerUser: 5})
	slow, fast := uuid.New(), uuid.New()

	stuck, err := hub.Register(slow)
	require.NoError(t, err)

	healthy, err := hub.Register(fast)
	require.NoError(t, err)

	for range sendBuffer + 1 {
		hub.Publish([]uuid.UUID{slow, fast}, Event{Type: EventTyping, ConversationID: uuid.New()})

		// the healthy socket reads as fast as events come
		<-healthy.Send
	}

	select {
	case <-stuck.Done():
	default:
		require.FailNow(t, "the socket that never reads was not closed")
	}

	require.Equal(t, 1, hub.Count())

	hub.Publish([]uuid.UUID{fast}, Event{Type: EventRead, ConversationID: uuid.New()})
	require.Equal(t, EventRead, received(t, healthy).Type)
}

func TestTwoCopiesOfTheAPIShareEventsThroughTheBus(t *testing.T) {
	bus := &fakeBus{}
	one := NewHub(Config{MaxSockets: 10, MaxPerUser: 3, Bus: bus})
	two := NewHub(Config{MaxSockets: 10, MaxPerUser: 3, Bus: bus})

	// a short wait, so a skipped number that never comes is found quickly
	one.gapWait = 30 * time.Millisecond
	two.gapWait = 30 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go one.Run(ctx)
	go two.Run(ctx)

	require.Eventually(t, func() bool {
		bus.mu.Lock()
		defer bus.mu.Unlock()

		return len(bus.handlers) == 2
	}, time.Second, 5*time.Millisecond)

	alice, bob := uuid.New(), uuid.New()

	onOne, err := one.Register(alice)
	require.NoError(t, err)

	onTwo, err := two.Register(bob)
	require.NoError(t, err)

	one.Publish([]uuid.UUID{alice, bob}, Event{Type: EventMessageDeleted, ConversationID: uuid.New(), MessageID: 9})

	require.EqualValues(t, 9, received(t, onTwo).MessageID, "the other copy delivers it to its own socket")
	require.EqualValues(t, 9, received(t, onOne).MessageID)
	nothing(t, onOne)
}

func TestABusThatFailsStillDeliversOnThisProcess(t *testing.T) {
	bus := &fakeBus{fail: true}
	hub := NewHub(Config{MaxSockets: 10, MaxPerUser: 3, Bus: bus})
	user := uuid.New()

	client, err := hub.Register(user)
	require.NoError(t, err)

	hub.Publish([]uuid.UUID{user}, Event{Type: EventTyping, ConversationID: uuid.New()})

	require.Equal(t, EventTyping, received(t, client).Type)
}

func TestPublishingToNobodyAndWithoutABusDoesNothing(t *testing.T) {
	hub := NewHub(Config{})

	hub.Publish(nil, Event{Type: EventRead})
	hub.Publish([]uuid.UUID{uuid.New()}, Event{Type: EventRead})
	hub.Run(context.Background())

	require.Zero(t, hub.Count())
	require.NotEmpty(t, hub.Collectors())
}

func TestShutdownClosesEverySocketAndTakesNoNewOnes(t *testing.T) {
	hub := NewHub(Config{MaxSockets: 10, MaxPerUser: 3})

	first, err := hub.Register(uuid.New())
	require.NoError(t, err)

	second, err := hub.Register(uuid.New())
	require.NoError(t, err)

	hub.Shutdown()
	hub.Shutdown()

	for _, client := range []*Client{first, second} {
		select {
		case <-client.Done():
		default:
			require.FailNow(t, "a socket stayed open")
		}
	}

	require.True(t, hub.Full())

	_, err = hub.Register(uuid.New())
	require.ErrorIs(t, err, ErrFull)
}

// connected starts two hubs on one bus and registers a socket on the second
func connected(t *testing.T) (one *Hub, bus *fakeBus, sender uuid.UUID, onTwo *Client) {
	t.Helper()

	bus = &fakeBus{}
	one = NewHub(Config{MaxSockets: 10, MaxPerUser: 3, Bus: bus})
	two := NewHub(Config{MaxSockets: 10, MaxPerUser: 3, Bus: bus})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go one.Run(ctx)
	go two.Run(ctx)

	require.Eventually(t, func() bool {
		bus.mu.Lock()
		defer bus.mu.Unlock()

		return len(bus.handlers) == 2
	}, time.Second, 5*time.Millisecond)

	sender = uuid.New()

	onTwo, err := two.Register(sender)
	require.NoError(t, err)

	return one, bus, sender, onTwo
}

func TestAGapInWhatAnotherCopyPublishedMakesTheSocketsResync(t *testing.T) {
	one, bus, user, onTwo := connected(t)

	one.Publish([]uuid.UUID{user}, Event{Type: EventMessageDeleted, ConversationID: uuid.New(), MessageID: 1})
	require.EqualValues(t, 1, received(t, onTwo).MessageID)

	// the bus is down for one event, which the other copy never sees
	bus.mu.Lock()
	bus.fail = true
	bus.mu.Unlock()

	one.Publish([]uuid.UUID{user}, Event{Type: EventMessageDeleted, ConversationID: uuid.New(), MessageID: 2})

	bus.mu.Lock()
	bus.fail = false
	bus.mu.Unlock()

	one.Publish([]uuid.UUID{user}, Event{Type: EventMessageDeleted, ConversationID: uuid.New(), MessageID: 3})

	require.EqualValues(t, 3, received(t, onTwo).MessageID)
	require.Equal(t, EventResync, received(t, onTwo).Type, "the app is told that it missed something once the skipped number did not come")
	nothing(t, onTwo)
}

// envelopeOf is what another copy puts on the bus
func envelopeOf(t *testing.T, origin string, seq uint64, recipients []uuid.UUID, event Event) []byte {
	t.Helper()

	payload, err := json.Marshal(envelope{Origin: origin, Seq: seq, Recipients: recipients, Event: event})
	require.NoError(t, err)

	return payload
}

func counterValue(t *testing.T, hub *Hub, name string) float64 {
	t.Helper()

	registry := prometheus.NewRegistry()

	for _, collector := range hub.Collectors() {
		require.NoError(t, registry.Register(collector))
	}

	families, err := registry.Gather()
	require.NoError(t, err)

	for _, family := range families {
		if family.GetName() == name {
			return family.GetMetric()[0].GetCounter().GetValue()
		}
	}

	return 0
}

func TestNumbersThatReachTheBusOutOfOrderAreNoLoss(t *testing.T) {
	hub := NewHub(Config{MaxSockets: 10, MaxPerUser: 3})
	hub.gapWait = 40 * time.Millisecond

	user := uuid.New()

	client, err := hub.Register(user)
	require.NoError(t, err)

	event := Event{Type: EventTyping, ConversationID: uuid.New()}

	hub.receive(envelopeOf(t, "a", 1, []uuid.UUID{user}, event))
	hub.receive(envelopeOf(t, "a", 3, []uuid.UUID{user}, event))
	hub.receive(envelopeOf(t, "a", 2, []uuid.UUID{user}, event))

	for range 3 {
		require.Equal(t, EventTyping, received(t, client).Type)
	}

	time.Sleep(120 * time.Millisecond)

	nothing(t, client)
	require.Zero(t, counterValue(t, hub, "kibooz_chat_event_gaps_total"), "number 2 came inside the wait")
}

func TestANumberThatNeverComesIsALossAfterTheWait(t *testing.T) {
	hub := NewHub(Config{MaxSockets: 10, MaxPerUser: 3})
	hub.gapWait = 40 * time.Millisecond

	user := uuid.New()

	client, err := hub.Register(user)
	require.NoError(t, err)

	event := Event{Type: EventTyping, ConversationID: uuid.New()}

	hub.receive(envelopeOf(t, "a", 1, []uuid.UUID{user}, event))
	hub.receive(envelopeOf(t, "a", 3, []uuid.UUID{user}, event))

	require.Equal(t, EventTyping, received(t, client).Type)
	require.Equal(t, EventTyping, received(t, client).Type, "the event is delivered at once and the loss is found later")
	require.Equal(t, EventResync, received(t, client).Type)
	require.EqualValues(t, 1, counterValue(t, hub, "kibooz_chat_event_gaps_total"))
}

func TestAJumpThatIsTooBigIsALossAtOnce(t *testing.T) {
	hub := NewHub(Config{MaxSockets: 10, MaxPerUser: 3})
	hub.gapWait = time.Hour

	user := uuid.New()

	client, err := hub.Register(user)
	require.NoError(t, err)

	event := Event{Type: EventTyping, ConversationID: uuid.New()}

	hub.receive(envelopeOf(t, "a", 1, []uuid.UUID{user}, event))
	hub.receive(envelopeOf(t, "a", 2+maxTrackedSkips+1, []uuid.UUID{user}, event))

	require.Equal(t, EventResync, received(t, client).Type, "the resync does not wait for the numbers of a jump that big")
}

func TestAHeartbeatShowsTheLossOfTheLastEnvelopeOfABurst(t *testing.T) {
	one, bus, user, onTwo := connected(t)

	one.Publish([]uuid.UUID{user}, Event{Type: EventMessageDeleted, ConversationID: uuid.New(), MessageID: 1})
	require.EqualValues(t, 1, received(t, onTwo).MessageID)

	// the bus is down for the last event, and no later event follows
	bus.mu.Lock()
	bus.fail = true
	bus.mu.Unlock()

	one.Publish([]uuid.UUID{user}, Event{Type: EventMessageDeleted, ConversationID: uuid.New(), MessageID: 2})

	bus.mu.Lock()
	bus.fail = false
	bus.mu.Unlock()

	nothing(t, onTwo)

	one.send(nil, Event{})

	require.Equal(t, EventResync, received(t, onTwo).Type, "the number of the heartbeat comes after the one that was lost")
	require.EqualValues(t, 1, counterValue(t, one, "kibooz_chat_bus_publish_failed_total"))
}

func TestNoResyncWhenNothingWasLostAndFirstEnvelopeOfACopyIsNeverALoss(t *testing.T) {
	one, _, user, onTwo := connected(t)

	for id := int64(1); id <= 3; id++ {
		one.Publish([]uuid.UUID{user}, Event{Type: EventMessageDeleted, ConversationID: uuid.New(), MessageID: id})
		require.EqualValues(t, id, received(t, onTwo).MessageID)
	}

	nothing(t, onTwo)
}

func TestTheNumbersOfOtherCopiesAreForgottenBeyondTheLimit(t *testing.T) {
	hub := NewHub(Config{})

	for range maxOrigins + 5 {
		skipped, lost := hub.track(envelope{Origin: uuid.NewString(), Seq: 1})
		require.Empty(t, skipped)
		require.False(t, lost)
	}

	require.LessOrEqual(t, len(hub.seen), maxOrigins)
}

func TestTheOriginThatWasHeardFromLongestAgoIsTheOneThatIsForgotten(t *testing.T) {
	hub := NewHub(Config{})

	for number := range maxOrigins {
		hub.track(envelope{Origin: "copy-" + string(rune('A'+number)), Seq: 1})
	}

	// the first one is heard from again, so the second is the one that has been quiet longest
	hub.track(envelope{Origin: "copy-A", Seq: 2})
	hub.track(envelope{Origin: "new", Seq: 1})

	require.Contains(t, hub.seen, "copy-A")
	require.Contains(t, hub.seen, "new")
	require.NotContains(t, hub.seen, "copy-B")
	require.Len(t, hub.seen, maxOrigins)
}

func TestAResyncReachesEverySocketAndClosesASlowOne(t *testing.T) {
	hub := NewHub(Config{MaxSockets: 10, MaxPerUser: 3})

	fast, err := hub.Register(uuid.New())
	require.NoError(t, err)

	slow, err := hub.Register(uuid.New())
	require.NoError(t, err)

	for range sendBuffer {
		slow.Send <- []byte("{}")
	}

	hub.resync()

	require.Equal(t, EventResync, received(t, fast).Type)
	require.Equal(t, 1, hub.Count(), "the socket that cannot keep up was closed")

	select {
	case <-slow.Done():
	default:
		require.FailNow(t, "the slow socket stayed open")
	}
}

func TestALateEnvelopeIsNeitherALossNorAStepBack(t *testing.T) {
	hub := NewHub(Config{})

	skipped, lost := hub.track(envelope{Origin: "a", Seq: 1})
	require.Empty(t, skipped)
	require.False(t, lost)

	skipped, lost = hub.track(envelope{Origin: "a", Seq: 3})
	require.Equal(t, []uint64{2}, skipped, "number 2 never came yet")
	require.False(t, lost)

	skipped, lost = hub.track(envelope{Origin: "a", Seq: 2})
	require.Empty(t, skipped, "number 2 arrives late, which is no new loss")
	require.False(t, lost)
	require.False(t, hub.stillMissing("a", []uint64{2}), "and nothing is waited for any more")

	skipped, lost = hub.track(envelope{Origin: "a", Seq: 4})
	require.Empty(t, skipped, "the next number follows 3, and 2 did not move the count back")
	require.False(t, lost)

	skipped, lost = hub.track(envelope{Origin: "a", Seq: 4})
	require.Empty(t, skipped, "a number that comes twice is no loss either")
	require.False(t, lost)

	for range 2 {
		skipped, lost = hub.track(envelope{Origin: "old", Seq: 0})
		require.Empty(t, skipped, "a copy from before the numbers existed sends 0 every time")
		require.False(t, lost)
	}
}

func TestGapsInsideTheWaitCostOneResyncAtTheEnd(t *testing.T) {
	hub := NewHub(Config{MaxSockets: 10, MaxPerUser: 3})
	hub.resyncWait = 60 * time.Millisecond

	client, err := hub.Register(uuid.New())
	require.NoError(t, err)

	hub.requestResync()
	require.Equal(t, EventResync, received(t, client).Type, "the first one goes at once")

	for range 5 {
		hub.requestResync()
	}

	nothing(t, client)

	require.Equal(t, EventResync, received(t, client).Type, "the gaps that came meanwhile are sent once when the time is over")
	nothing(t, client)
}

func TestRunPublishesAHeartbeatUntilTheContextEnds(t *testing.T) {
	bus := &fakeBus{}
	hub := NewHub(Config{MaxSockets: 10, MaxPerUser: 3, Bus: bus})
	hub.heartbeatWait = 5 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		hub.Run(ctx)
		close(done)
	}()

	count := func() int {
		bus.mu.Lock()
		defer bus.mu.Unlock()

		return bus.published
	}

	require.Eventually(t, func() bool { return count() >= 3 }, 2*time.Second, 5*time.Millisecond, "a heartbeat is published again and again")

	cancel()
	<-done

	// one that was already on its way may still be counted
	time.Sleep(30 * time.Millisecond)

	stopped := count()

	time.Sleep(50 * time.Millisecond)

	require.Equal(t, stopped, count(), "no heartbeat after the context ended")
}
