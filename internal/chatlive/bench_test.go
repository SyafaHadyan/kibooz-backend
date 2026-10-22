package chatlive

import (
	"testing"

	"github.com/google/uuid"
)

// BenchmarkPublishToSockets delivers one event to every socket of 200 accounts, which is the work of one message in a busy class room
func BenchmarkPublishToSockets(b *testing.B) {
	hub := NewHub(Config{MaxSockets: 1000, MaxPerUser: 3})
	recipients := make([]uuid.UUID, 0, 200)
	clients := make([]*Client, 0, 200)

	for range 200 {
		id := uuid.New()

		client, err := hub.Register(id)
		if err != nil {
			b.Fatal(err)
		}

		recipients = append(recipients, id)
		clients = append(clients, client)
	}

	event := Event{Type: EventMessageDeleted, ConversationID: uuid.New(), MessageID: 1}

	b.ReportAllocs()

	for range b.N {
		hub.Publish(recipients, event)

		// the sockets are read, as the handlers of the sockets do, so no buffer fills up and no socket is closed as slow
		for _, client := range clients {
			<-client.Send
		}
	}
}
