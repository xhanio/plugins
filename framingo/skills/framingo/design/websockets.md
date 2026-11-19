# WebSockets: the bridge, closing, and stopping

## The bus bridge is a utility, not a messagebus method

**Decision.** `wsutil.Bridge(conn, messenger, opts...)` joins a WebSocket
connection to a bus `Messenger` both ways until the connection closes. It
lives in `pkg/utils/wsutil`, not on `model.MessageBus`. A `bridge` is one
connection's session — its two ends, its keepalive (`WithPing`), its
context — and the pumps hang on it.

**Why.** It needs a `Messenger` and nothing of the bus, so the bus no
longer imports `coder/websocket`, and the ping settings belong to the bridge
that pings, not to a bus that never does. The name says what it does: it
bridges two transports, both ways, blocking like `io.Copy`.

## Close by what the handler returned

**Decision.** `wsutil.Close(ctx, conn, err)` ends a connection once its
handler returned: a normal closure for nil; nothing for the peer's own close;
released with nothing sent when the peer dropped it (EOF); going away once
`ctx` ended; otherwise an internal error carrying `err` — the one case it
returns, for the caller to log. The bridge does not call it: the connection
is the server's, and the server closes it.

## The server closes its connections when it stops

**Decision.** net/http's `Shutdown` neither closes nor waits for a hijacked
connection, so the api server does: each echo gets a context its WebSocket
handlers run under; `stop` ends it as it begins — `context.AfterFunc`
closes each connection going away and an upgrade from then on is refused —
then waits for the handlers until its own context ends. That context is also
what `wsutil.Close` is handed: the request's never ends for a hijacked
connection.

**Why.** Otherwise a stop left every handler running — a bridge's messenger
still subscribed to the bus — into the start after it. The wait group spans
starts, as the framework's daemons share one: a handler a stop gave up on
is still waited for by the next.
