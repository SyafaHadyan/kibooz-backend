package chatlive

import (
	"context"
	"log"

	"github.com/SyafaHadyan/kibooz-backend/internal/infra/redis"
)

// channelName is the Redis channel that every API copy publishes chat events to and listens on
const channelName = "chat:events"

// RedisBus carries the events between the API copies over Redis pub/sub. It only makes delivery faster, and nothing depends on it.
type RedisBus struct {
	redis *redis.Redis
}

func NewRedisBus(client *redis.Redis) *RedisBus {
	return &RedisBus{redis: client}
}

func (b *RedisBus) Publish(ctx context.Context, payload []byte) error {
	return b.redis.Publish(ctx, channelName, payload)
}

func (b *RedisBus) Run(ctx context.Context, handle func(payload []byte)) {
	subscription := b.redis.Subscribe(ctx, channelName)

	defer func() { _ = subscription.Close() }()

	// the channel reconnects and subscribes again by itself when Redis drops, and it closes when the subscription does
	messages := subscription.Channel()

	for {
		select {
		case <-ctx.Done():
			return
		case message, open := <-messages:
			if !open {
				if ctx.Err() == nil {
					log.Println("the chat event subscription closed, live delivery between copies stops until the next start")
				}

				return
			}

			handle([]byte(message.Payload))
		}
	}
}
