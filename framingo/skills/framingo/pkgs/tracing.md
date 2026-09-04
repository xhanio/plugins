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

- **`Init` runs on every restart** and re-reads the `tracing.*` keys
  (`confutil.FromContext`; under `<name>.*` instead for a `WithName`d
  instance), so enabling, disabling, re-pointing, or
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
  spans reach the exporter. Register telemetry at the **end** of the infra
  tier's `Register` call: the topo sort pops dependency-less services in
  reverse registration order, so the services listed last init first and,
  with stop reversed, flush last — after the draining api server's final
  requests.
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

`WithLogger`, `WithName` (which also moves the config root — a manager named
`otel` reads `otel.*`), plus the construction-time values the wiring layer
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

## Database Spans — `pkg/services/db/plugins/otelgorm`

The db service emits one client span per statement through a GORM plugin,
wired with the same explicit handle:

```go
import "github.com/xhanio/framingo/pkg/services/db/plugins/otelgorm"

m.db = db.New(
    db.WithType(config.GetString("db.type")),
    db.WithPlugins(p),   // p, err := otelgorm.New(m.tracing, ...)
)
```

It wraps `gorm.io/plugin/opentelemetry/tracing`, kept in its own subpackage
because that package imports the ClickHouse/MySQL/PostgreSQL dialectors
unconditionally — importing it from `pkg/services/db` would link all three
into every binary and undo the driver registry. Framingo hands it
`traceutil.LiveProvider(t)` so `Provider()` is re-resolved per span (a tracing
restart takes effect without rebuilding the connection) and forces
`WithoutMetrics()` (that path reaches for the global `MeterProvider`, and
upstream offers no way to redirect it). That is the only forced default —
upstream `tracing.Option` values are applied after it, so every other knob
passes through. Pass `tracing.WithoutQueryVariables()` yourself: upstream puts
bound values into `db.query.text` by default, and that is where the credentials
and the PII are.

### Wiring other instrumentation — `pkg/utils/traceutil`

Any library that takes a `trace.TracerProvider` reads `Tracer(...)` once and
holds it, so passing `t.Provider()` directly pins its spans to the provider
live at wiring time — a restart retires that one and it drops spans silently.
`traceutil.LiveProvider(t)` re-resolves per span:

```go
otelgrpc.NewServerHandler(otelgrpc.WithTracerProvider(traceutil.LiveProvider(m.tracing)))
```

It depends only on `model.Tracer` and the otel trace API, so a component with
its own `model.Tracer` implementation does not pull in the tracing service.

For tests, `pkg/utils/testutil` supplies `NewTracer(provider)` (a
`model.Tracer` whose provider `Swap` replaces mid-flight, standing in for a
service restart) and `NewRecorder()` (in-memory exporter + synchronous
provider, so a span is readable the moment it ends).

Reach for it only when the real service cannot do the job — usually it can:

- **Not reading spans back?** `tracing.New()` alone is already a usable
  `model.Tracer` — no `Init`, no config, no exporter. It serves a noop
  provider, so the component under test traces into nothing and costs nothing.
- **Asserting on spans?** `tracing.Register` is exported, so a test can
  register an in-memory exporter and run the real service. `Init` builds the
  provider with `WithBatcher`, so call `Stop(true)` — which flushes
  synchronously — before reading the exporter.
- **Testing a provider swap?** That is the case that wants a double, since the
  service only swaps its provider through `Init`.

**Only context-carrying queries nest.** The plugin starts its span from
`tx.Statement.Context`, so what matters is whether a context reached the
statement, not which accessor produced it. `FromContext(ctx)`,
`FromContextTimeout(ctx, d)` and `ORM().WithContext(ctx)` all parent correctly;
bare `db.Manager.ORM()` carries `context.Background()` and exports detached
root spans. Prefer `FromContext(ctx)` anyway — it also picks up an active
transaction from the context, which `ORM().WithContext` bypasses. See
[db.md](db.md).

Like the middleware, db does **not** declare tracing in `Dependencies()`: a
dead collector must never make the database unready.

## Spans in App Code

Handlers already sit inside the server span: the project `api.Context` is a
valid `context.Context` carrying it, so `svc.Do(c, ...)` propagates for free.
A business method adds its own child span **without holding any tracer** —
`traceutil.Start` derives the tracer from the span already in the context,
so the live provider rides each request and a context without a recording
span (tracing disabled, unit tests) yields a noop:

```go
func (m *manager) HelloWorld(ctx context.Context, message string) (*entity.HelloWorld, error) {
	ctx, span := traceutil.Start(ctx, m.name, "HelloWorld")

	thing, err := m.repository.Create(ctx, ...)
	if err != nil {
		return nil, traceutil.Fail(span, errors.Wrap(err))
	}
	// ...
	span.End()
	return result, nil
}
```

The manual pair keeps the signature plain — no defer, no named returns, no
closure. `traceutil.Fail` records the error, marks the span failed, ends
it, and hands the error back, so an error return stays one line. The
discipline it asks for: **every return path must either `Fail` or
`span.End()`** — a path that does neither leaks an unended span that never
exports.

The scope is `m.name`, matching the service's metric scope. This is why
tracing stays out of the app's platform bundle: no business service takes the
handle — the trace middleware owns extraction and the server span, and
business spans nest under it through the context. A service that genuinely
needs the handle (custom propagation, span links across requests) still
takes `model.Tracer` as a constructor arg and resolves `Provider()` per use
— never cache the `trace.TracerProvider` in a field: a tracing restart swaps
providers, and a retired provider silently drops spans.

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
- Registering tracing anywhere but the tail of the infra tier still works,
  but spans emitted during shutdown may be dropped instead of flushed.
- A db query issued through `ORM()` traces as its own root span, never under
  the request that caused it — `ORM()` has no context to parent from. Use
  `FromContext(ctx)`.
