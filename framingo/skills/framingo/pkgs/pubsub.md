# Pub/Sub Messaging

Two layers: the low-level pub/sub primitive (`pkg/services/pubsub`) and a higher-level message bus (`pkg/services/messagebus`) that routes module messages through a single topic on top of it.

## Pub/Sub Primitive

`pkg/services/pubsub` with pluggable drivers under `pkg/services/pubsub/driver/` (Memory, Redis, Kafka).

`pubsub.Manager` = `model.Pubsub` + `Daemon`/`Initializable`/`Debuggable`. The business half:

```go
type Pubsub interface {
    common.Service
    // from identifies the publisher; subscribers registered under the same
    // name do NOT receive their own message. ctx bounds only the
    // cross-instance hop (Redis/Kafka), so a cancel can deliver partially.
    Publish(ctx context.Context, from, topic, kind string, payload any) error
    // Returns a CHANNEL, not a handler registration. Caller must Unsubscribe.
    Subscribe(name, topic string) (<-chan entity.PubsubMessage, error)
    // Closes the channel - of EVERY subscriber under that name on that topic.
    Unsubscribe(name, topic string) error
}
```

Drivers:

| Constructor | Reach |
|---|---|
| `driver.NewMemory(logger, opts...)` | in-process only |
| `driver.NewRedis(redisClient, logger, opts...) (Driver, error)` | across instances, publishing to `pubsub:<topic>` and pattern-subscribing to `pubsub:<topic>*` |
| `driver.NewKafka(brokers, groupID, logger, opts...) (Driver, error)` | across instances, one Kafka topic `pubsub`; each instance joins its own `groupID-<uuid>` group so every instance sees every message |

A remote driver's `Publish` delivers to its own instance's subscribers directly, then publishes; each event carries the instance it came from, so the copy the broker hands back to that instance is skipped and a subscriber gets each message once.

A message crossing instances travels as JSON: a subscriber on another
instance gets its `Payload` as a `json.RawMessage`, never the publisher's
Go value.

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

A pubsub stop forgets every subscription and closes its channel, which ends
the loop above. A subscriber that runs on beside a pubsub restarted alone
subscribes again: it is a `common.Reconcilable`, and does so on
`DependencyRunning` ([supervisor.md](supervisor.md#reconciling-with-a-dependency)).

**Do not reach for `Subscribe(topic, handler)`** — there is no handler-registration form at this layer. Handler-style dispatch (`HandleMessage`/`HandleRawMessage`) lives one layer up in `messagebus`; use that if you want handlers rather than channels.

Topics are hierarchical in the subscriber's favour: subscribing to `orders` receives `orders`, `orders/created`, `orders/created/eu`, etc.

### Slow subscribers

Each subscriber gets a growable pending queue (capped, `driver.WithQueueCap`, default 50000) drained by its own
goroutine, so a briefly slow consumer loses nothing and never stalls `Publish`. When the queue
fills, the consumer has stopped draining entirely, and `driver.WithOnFull` decides what happens:

```go
// Default: discard the message, count it, log it (throttled).
driver.NewMemory(logger)

// Close the subscriber's channel instead, so it reconnects and resumes from its own cursor.
driver.NewMemory(logger, driver.WithOnFull(driver.DropSubscriber))
```

`driver.WithChannelBuffer` sizes the subscriber's channel itself (default 256).

Pick `DropSubscriber` when consumers can reconnect and replay (a persisted log plus a cursor).
A lost connection is recoverable and visible; a lost message is neither. All three drivers accept
these options, and expose `Dropped()` / `Evicted()` via the optional `driver.Stats` interface,
which `pubsub.Manager.Info` reports alongside its own published count.

An in-process bus is never a delivery guarantee — it dies with the process. Durability belongs in
whatever log the consumer replays from.

## Message Bus

`pkg/services/messagebus` wraps a `model.Pubsub` and dispatches via a single well-known topic (`/messages` by default). Modules register once and receive typed (`common.Message`) or raw (`kind`, payload) messages.

```go
import "github.com/xhanio/framingo/pkg/services/messagebus"

mb := messagebus.New(ps, messagebus.WithLogger(logger))

// Register any named module (a service is one): modules implementing
// common.MessageHandler / common.RawMessageHandler are auto-subscribed under
// their name; others are a no-op.
mb.Register(someService)

// Direct channel access (e.g. WebSocket bridge). The name must be unique per
// connection: it is the subscription, and Unsubscribe drops every subscriber
// sharing it.
messenger, err := mb.NewMessenger("ws:" + session.UID() + ":" + uuid.NewString())
if err != nil {
    return errors.Wrap(err)
}
// pkg/utils/wsutil: blocks until the session ends, closes the messenger, and
// returns how the server closes the conn (in a WS handler, the server closes it)
return wsutil.Bridge(wsConn, messenger, wsutil.WithLogger(logger))
```

`Register` works before or after `Start` — modules registered earlier are
subscribed when the bus starts, later ones at once — and a second module
under a taken name is ignored. `Stop` unsubscribes every module and waits
for running handlers until its context ends. A pubsub stop forgets every
subscription; when the supervisor restarts pubsub alone, the messagebus — a
`common.Reconcilable` — subscribes every module again
([supervisor.md](supervisor.md#reconciling-with-a-dependency)). A
`Messenger`'s channel closes with it, and its `wsutil.Bridge` ends the
session, telling the client to try again later (`StatusTryAgainLater`). Options: `WithLogger`,
`WithName`, and `WithTopic` (default `messagebus.DefaultTopic`), which lets
several buses share one pubsub. `messagebus.Manager` is the
`model.MessageBus` plus `Daemon`, `Debuggable` and `Reconcilable`.

`wsutil.Bridge` relays both ways as JSON: each `entity.PubsubMessage` on the
messenger's channel is written to the socket, and each frame read that
parses as one is sent on the bus as its `kind`/`payload` (unparseable frames
are skipped). It pings the client every 30s and tears the session down on a
pong missing for 10s — `wsutil.WithPing(interval, timeout)` tunes both, an
interval of 0 turns pings off. The example's `messagebus` router serves this
as a `method: WS` route ([api.md](api.md#websocket-handlers)).

A session of its own shapes the bridge with four options:

- `WithFilter(keep)` writes only the messages `keep` returns true for — the
  session's own, matched against what its handshake said;
- `WithFrame(frame)` writes what `frame` makes of a message, its payload
  alone say, instead of the envelope;
- `WithInbound(handle)` takes each frame the client sends instead of relaying
  it to the bus — an ack, say; an error it returns ends the session with that
  close;
- `WithDone(ch, err)` ends the session once `ch` closes, closing with `err`:
  `CloseWith(websocket.StatusNormalClosure, "displaced")` for a session a
  newer one claimed, so its client stops reconnecting.

A handshake the client must begin with is `wsutil.ReadFirst[T](ctx, conn,
timeout, reason)`: the first frame decoded into a `T`, or, none in time or
one unreadable, a `CloseWith(StatusPolicyViolation, reason)` to return; a
peer gone before it spoke is released with nothing sent, and
`wsutil.PeerGone(err)` tells the two apart for the logs. A `ctx` that ends
first is a third case, `ctx.Err()`, neither — never met in the api server,
which ends no WebSocket request's context and on stop closes the connection,
a peer gone. What the frame must
hold is the handler's to check, refusing with
`wsutil.CloseWithCause(status, reason, why)`: the peer gets the reason, the
error keeps why:

```go
reg, err := wsutil.ReadFirst[Registration](c, conn, 5*time.Second, "registration required")
if err != nil {
    if !wsutil.PeerGone(err) {
        log.Warnf("stream refused: %s", err) // the cause too; the peer got the reason
    }
    return err
}
if reg.SessionID == "" {
    return wsutil.CloseWithCause(websocket.StatusPolicyViolation, "registration required",
        errors.Newf("registration without a session id"))
}
lease := registry.Claim(reg.SessionID) // the app's: a newer claim evicts this one
defer lease.Release()
messenger, err := mb.NewMessenger("stream:" + reg.SessionID + ":" + uuid.NewString())
if err != nil {
    return errors.Wrap(err)
}
return wsutil.Bridge(conn, messenger,
    wsutil.WithFilter(func(m entity.PubsubMessage) bool {
        ev, ok := m.Payload.(*Event)
        return ok && ev.TargetSessionID == reg.SessionID
    }),
    wsutil.WithFrame(func(m entity.PubsubMessage) any { return m.Payload }),
    wsutil.WithDone(lease.Evicted(), wsutil.CloseWith(websocket.StatusNormalClosure, "displaced")),
)
```

Which session holds a key is the app's — a registry where a newer claim
evicts the older, closing its `Evicted()` channel — since what it holds
beside, and who watches it, is the app's too.

## Message Interfaces

Defined in `pkg/types/common` (not in `pubsub`/`messagebus`):

```go
// Typed messages
type Message interface { Kind() string }

// Handlers — each embeds Named, not Service: the handler is the module
// Register takes, and its name is its subscription. It needs no Dependencies.
// MessageHandler sees only messages whose payload is a common.Message (sent
// by SendMessage on this instance); RawMessageHandler sees every message.
// A module implementing both gets a typed message twice, once per method.
type MessageHandler interface {
    Named
    HandleMessage(ctx context.Context, msg Message) error
}
type RawMessageHandler interface {
    Named
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
    Send(ctx context.Context, kind string, payload any) error  // skips itself; the publish error
    Close()                                  // unsubscribes and closes Ch
}
```

An empty name is an error; the caller must `Close` the messenger
(`wsutil.Bridge` does it on return).
