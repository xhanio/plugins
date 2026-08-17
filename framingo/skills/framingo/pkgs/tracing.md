# Tracing — `pkg/services/tracing` + the `trace` Middleware

OpenTelemetry tracing as a supervised service. Framingo never touches the
otel globals (`otel.SetTracerProvider` / `otel.SetTextMapPropagator`):
everything that emits or propagates spans receives an explicit handle — the
`model.Tracer` interface — the same way config travels as a viper instance
rather than the singleton.

```go
// pkg/types/model/tracing.go
type Tracer interface {
	common.Service
	Provider() trace.TracerProvider              // always non-nil; noop when disabled
	Propagator() propagation.TextMapPropagator   // W3C tracecontext + baggage
}
```

Only otel *API* types appear in `model` (precedent: `model.Database` exposes
`*gorm.DB`); the SDK stays behind `pkg/services/tracing`.

## The Service

```go
type Manager interface {
	model.Tracer
	common.Liveness        // wiring only — see below
	common.Initializable   // builds/rebuilds the provider from tracing.* keys
	common.Daemon          // Start is a no-op; Stop flushes and retires the provider
	common.Debuggable
}

m := tracing.New(tracing.WithLogger(logger))   // all further config via tracing.* keys in Init
```

Lifecycle facts that matter:

- **`Init` runs on every restart** and re-reads the `tracing.*` viper keys
  (`confutil.FromContext`), so enabling, disabling, re-pointing, or
  re-sampling tracing is a config edit + service restart. Config is the
  authority: the reload runs through the same `applyOptions` + `setDefaults`
  pair the constructor uses, so an absent key reads zero and lands on the
  default rather than preserving whatever construction set. The previous
  provider is flushed (bounded by 5s) and swapped for the new one.
- **Disabled means noop, not nil.** `Provider()` always returns a working
  handle; with `tracing.enabled: false` (the default) it is a noop provider
  and spans cost nothing. Callers never nil-check.
- **`Stop` is the flush.** The batch processor exports on its own clock;
  `Stop(wait)` shuts the provider down synchronously (5s bound) so buffered
  spans reach the exporter. Register tracing **first** in the basic tier:
  stop runs in reverse, so it flushes last — after the draining api server's
  final requests.
- **`Alive` guards wiring only** (enabled but no provider built — a restart
  rebuilds it, and a deliberate stop does not fail it). There is deliberately
  **no `Ready`**: tracing is auxiliary, and the monitor rolls a dependency's
  probes into every dependent's healthcheck — a dead collector must never
  degrade the api server. Collector reachability is probed nowhere; otlp
  exporters connect lazily, so an unreachable collector doesn't fail Init
  either.

## Exporters — registry + blank imports

Same pattern as the db drivers: the core package holds a registry keyed by
name; each subpackage under `pkg/services/tracing/exporters/*` registers a
`Factory` in its `init()`; binaries blank-import only what they use (the grpc
stack most of all stays out of binaries that don't pick `otlpgrpc`).

```go
_ "github.com/xhanio/framingo/pkg/services/tracing/exporters/otlpgrpc"  // tracing.OTLPGRPC = "otlpgrpc"
_ "github.com/xhanio/framingo/pkg/services/tracing/exporters/otlphttp"  // tracing.OTLPHTTP = "otlphttp"
_ "github.com/xhanio/framingo/pkg/services/tracing/exporters/stdout"    // tracing.Stdout   = "stdout"
```

An enabled config naming an unimported exporter fails `Init` with
`unsupported tracing exporter: <name> (not registered — blank-import ...)`.
A `Factory` receives the `tracing.config` block as raw YAML bytes (nil when
absent) — different exporters need different config shapes, so the manager
never interprets the block; each factory **strict-decodes its own config
struct** and fails Init on unknown keys, the same contract
`api.Middleware.Func` has with its config bytes. The otlp pair shares the
transport-agnostic core (`endpoint`, `insecure`, `headers`, `compression`,
`timeout`, `max_request_size`, `retry{enabled, initial_interval,
max_interval, max_elapsed_time}`), `otlphttp` adds `url_path`, and `stdout`
has its own tiny shape (`pretty`, `timestamps`) — so a `url_path` handed to
`otlpgrpc` is a startup error, not silently ignored. Durations are
`"5s"`-style strings; a present `retry` block defaults `enabled` to true,
matching otlp's own retry-on default.

## Options

`WithLogger`, `WithName`, plus the construction-time values the wiring layer
reads out of config and the `tracing.*` keys re-apply on every `Init`:
`WithEnabled`, `WithExporter(name, config)` — the registry name together
with the kv block its factory strict-decodes, one decision since the block
only has meaning for the exporter that reads it (nil config = exporter
defaults; the programmatic twin of `tracing.exporter` + `tracing.config`) —
and `WithSampleRatio` (default 1.0, wrapped in `ParentBased` so remote
parents keep their sampling decision).

Reading config at construction is the house pattern ([config.md](config.md)):
`service.go` maps keys onto options, and `Init` re-applies the subset that
may change across a restart — for tracing that happens to be all of them,
through the very same options. So the options are the wiring layer's reading
of config, not a second source of truth: an app that wires them without also
supplying config gets the defaults back on the first `Init`.

The span resource is the build's application identity and is **not
configurable at all** — no option, no config key: the binary knows which
application it is, and renaming it from the outside only drifts from that.
`service.name` is `info.ApplicationName` (one binary, one service — the
product name would merge every binary shipped under it) and
`service.version` is `info.ApplicationVersion`. Both are ldflags-injected,
so a build without that injection (framingo does not require gopro) omits
the attribute rather than setting it blank, leaving the backend to show its
own unknown-service placeholder.

`WithEnabled` is its own switch, and **choosing an exporter never implies
it**: `WithExporter` (or `tracing.exporter`) says *where* spans would go if
the service were on, nothing more. Nothing traces until `tracing.enabled` is
true — set at construction by the wiring layer reading the key, and re-read
by `Init` on every restart so the key stays the durable ops switch.

There is also deliberately **no instance-injection option**: everything is a
`Factory`. An exporter whose construction no config can express (vendor
SDKs, dial options, credential objects) is registered by the app under its
own name — `tracing.Register("mine", factory)` — where the factory closure
captures whatever it needs and may ignore the config bytes; `tracing.
exporter: mine` (or `WithExporter("mine", nil)`) then picks it. The tracing
package's own tests inject a `tracetest.NewInMemoryExporter()` by assigning
the concrete manager's unexported field directly and drain deterministically
via the provider's `ForceFlush` before asserting.

## Config

```yaml
tracing:
  enabled: true        # default false — opt in
  exporter: stdout     # otlpgrpc | otlphttp | stdout (blank-import the subpackage)
  sample_ratio: 1.0
  # service.name is not a config key - it comes from the build
  # config:                    # exporter-specific kv block, opaque to the manager
  #   endpoint: localhost:4317 # otlp: collector host:port, no scheme
  #   insecure: true
  #   headers: { authorization: "Bearer ..." }
  #   compression: gzip
  #   timeout: 10s
  #   retry: { initial_interval: 5s }

api:
  http:
    middlewares:
      trace: true      # activates the framework's trace middleware on this server
```

## The Trace Middleware — `pkg/middlewares/trace`

The server span comes from a framework-shipped `fapi.Middleware` named
`trace` — a standard extension-point middleware, not a special case in the
server core. Attach it server-level at `Add` time, **listed first** so its
span covers the middlewares behind it, and activate it per server through
the middleware configs:

```go
import tracemw "github.com/xhanio/framingo/pkg/middlewares/trace"

server.WithMiddlewares(tracemw.New(m.tracing), corsmw.New()),
```

`trace: true` (or `trace:`) under `api.<name>.middlewares` enables it; **no
entry leaves the attachment dormant**, which is how the health/probe listener
serves untraced next to a traced public server. The middleware takes no
config — a block under `trace` fails registration. `WithName` renames an
attachment (config keys follow the name), the way out of a collision with an
app middleware called trace. The roster survives the echo rebuild a restart
performs, so tracing keeps working across server restarts.

Chain position (the server-level slot sees every request, matched or not):

`Request → Recover → [trace, cors, … the WithMiddlewares roster in order] → Logger → Info → Error → [Route middlewares] → Handler`

Per request it: extracts the W3C remote parent from the headers, starts a
`SpanKindServer` span, pushes it into the request context with
`c.SetRequest(r.WithContext(ctx))` — which the project's `api.Context`
bridges to every handler and service call — and after the chain returns,
renames the span to the low-cardinality route template (`GET
/api/items/:id`), sets `http.route`/`url.path`/`http.response.status_code`
semconv attributes, and marks 5xx with `codes.Error` plus the recorded
error. Requests matching no route still get a span under the fallback name
`HTTP <METHOD>`.

The existing framingo tid (`X-TRACE-ID`, the `tid=` log column,
`api.Context.TraceID()`) is untouched; the span records it as the
**`framingo.trace_id`** attribute, which is the jump from a log line to its
trace.

### Dependency semantics — deliberately weak

The middleware declares the tracing service in its `Dependencies()`
(`fapi.Middleware` embeds `common.Service`), but the api server does **not**
surface middleware dependencies as its own: a middleware's backing service
being down degrades that middleware, never the server — a dead collector
must not pull an otherwise-serving pod out of rotation through the
supervisor's healthcheck roll-up. Ordering needs no edge either: routers and
middlewares register in `initAPI()`, after `services.Init(ctx)`, and
start/stop order comes from the registration tiers (tracing first in the
basic tier, api after `TopoSort`). If you want the middleware's dependency
health visible, register the middleware itself with the supervisor — it is a
`common.Service` — and its degradation reports under its own name.

## Spans in App Code

Handlers already sit inside the server span: the project `api.Context` is a
valid `context.Context` carrying it, so `svc.Do(c, ...)` propagates for free.
A service that wants its own child spans takes the `model.Tracer` handle as a
constructor arg and:

```go
ctx, span := s.tracer.Provider().Tracer("services/order").Start(ctx, "order.settle")
defer span.End()
```

Resolve `Provider()` per use — never cache the `trace.TracerProvider` in a
field: a tracing restart swaps providers, and a retired provider silently
drops spans.

## Gotchas

- Tracing is off unless something says `enabled` — the `tracing.enabled` key
  or `WithEnabled` carrying it; wiring an exporter alone traces nothing.
  Enabled with no exporter named anywhere fails Init.
- The otlp `endpoint` is `host:port` with **no scheme**.
- A panicking request exports its span with the fallback name and no error
  status: Trace's deferred `End` unwinds before Recover normalizes the
  response. Accepted trade for keeping panic handling in one place.
- Exceptions are recorded only for 5xx — a 4xx is the caller's mistake, so a
  scanner's 404s don't bloat spans with exception events.
- A WebSocket route on a traced server produces one span covering the whole
  connection, ended (and exported) only at close — session-length spans, not
  per-message ones.
- Registering tracing anywhere but first in the basic tier still works, but
  spans emitted during shutdown may be dropped instead of flushed.
