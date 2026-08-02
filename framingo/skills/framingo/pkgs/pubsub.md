# Pub/Sub Messaging

Two layers: the low-level pub/sub primitive (`pkg/services/pubsub`) and a higher-level message bus (`pkg/services/messagebus`) that routes module messages through a single topic on top of it.

## Pub/Sub Primitive

`pkg/services/pubsub` with pluggable drivers under `pkg/services/pubsub/driver/` (Memory, Redis, Kafka).

`pubsub.Manager` = `model.Pubsub` + `Daemon`/`Initializable`/`Debuggable`. The business half:

```go
type Pubsub interface {
    common.Service
    // from identifies the publisher; subscribers registered under the same
    // name do NOT receive their own message. Returns error.
    Publish(ctx context.Context, from, topic, kind string, payload any) error
    // Returns a CHANNEL, not a handler registration. Caller must Unsubscribe.
    Subscribe(name, topic string) (<-chan entity.PubsubMessage, error)
    Unsubscribe(name, topic string) error
}
```

```go
import (
    "github.com/xhanio/framingo/pkg/services/pubsub"
    "github.com/xhanio/framingo/pkg/services/pubsub/driver"
)

ps := pubsub.New(driver.NewMemory(logger), pubsub.WithLogger(logger))

if err := ps.Publish(ctx, m.Name(), "orders/created", "order.created", order); err != nil {
    return errors.Wrap(err)
}

ch, err := ps.Subscribe("reporting", "orders")   // also receives "orders/created"
if err != nil {
    return errors.Wrap(err)
}
defer ps.Unsubscribe("reporting", "orders")
for msg := range ch {
    // msg is entity.PubsubMessage — carries From, Topic, Kind, Payload
}
```

**Do not reach for `Subscribe(topic, handler)`** — there is no handler-registration form at this layer. Handler-style dispatch (`HandleMessage`/`HandleRawMessage`) lives one layer up in `messagebus`; use that if you want handlers rather than channels.

Topics are hierarchical in the subscriber's favour: subscribing to `orders` receives `orders`, `orders/created`, `orders/created/eu`, etc.

### Slow subscribers

Each subscriber gets a growable pending queue (capped, `driver.WithQueueCap`) drained by its own
goroutine, so a briefly slow consumer loses nothing and never stalls `Publish`. When the queue
fills, the consumer has stopped draining entirely, and `driver.WithOnFull` decides what happens:

```go
// Default: discard the message, count it, log it (throttled).
driver.NewMemory(logger)

// Close the subscriber's channel instead, so it reconnects and resumes from its own cursor.
driver.NewMemory(logger, driver.WithOnFull(driver.DropSubscriber))
```

Pick `DropSubscriber` when consumers can reconnect and replay (a persisted log plus a cursor).
A lost connection is recoverable and visible; a lost message is neither. All three drivers accept
these options, and expose `Dropped()` / `Evicted()` via the optional `driver.Stats` interface,
which `pubsub.Manager.Info` reports.

An in-process bus is never a delivery guarantee — it dies with the process. Durability belongs in
whatever log the consumer replays from.

## Message Bus

`pkg/services/messagebus` wraps a `model.Pubsub` and dispatches via a single well-known topic (`/messages` by default). Modules register once and receive typed (`common.Message`) or raw (`kind`, payload) messages.

```go
import "github.com/xhanio/framingo/pkg/services/messagebus"

mb := messagebus.New(ps, messagebus.WithLogger(logger))

// Register any service: modules implementing common.MessageHandler /
// common.RawMessageHandler are auto-subscribed; others are no-op.
mb.Register(someService)

// Direct channel access (e.g. WebSocket bridge)
messenger, _ := mb.NewMessenger("ws:user-123")
mb.AttachWebSocket(messenger, wsConn) // blocks until conn closes
```

## Message Interfaces

Defined in `pkg/types/common` (not in `pubsub`/`messagebus`):

```go
// Typed messages
type Message interface { Kind() string }

// Handlers — note each embeds Service, so a handler is always a service.
type MessageHandler interface {
    Service
    HandleMessage(ctx context.Context, msg Message) error
}
type RawMessageHandler interface {
    Service
    HandleRawMessage(ctx context.Context, kind string, payload any) error
}

// Senders — take the SENDER as `from` and return NOTHING. Do not write
// `if err := ...SendMessage(...)`; it does not compile.
type MessageSender interface {
    Service
    SendMessage(ctx context.Context, from Named, msg Message)
}
type RawMessageSender interface {
    Service
    SendRawMessage(ctx context.Context, from Named, kind string, payload any)
}
```

`from` is what makes non-self-delivery work — pass the sending service itself:

```go
// inside a service whose receiver is m
m.mb.SendRawMessage(ctx, m, "helloworld", result)   // m implements Named
```

`NewMessenger` returns a `model.Messenger` — the raw-channel handle used for bridges:

```go
type Messenger interface {
    common.Named
    Ch() <-chan entity.PubsubMessage        // closed by Close
    Send(ctx context.Context, kind string, payload any) error
    Close()                                  // unsubscribes and closes Ch
}
```
