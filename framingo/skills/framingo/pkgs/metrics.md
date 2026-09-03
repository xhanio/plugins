# Metrics — `pkg/services/metrics`

OpenTelemetry metrics as a supervised service. Same doctrine as tracing
([tracing.md](tracing.md)): framingo never touches the otel globals — no
`otel.SetMeterProvider` — everything that records measurements receives the
explicit `model.Meter` handle.

```go
// pkg/types/model/metrics.go
type Meter interface {
	common.Service
	Provider() metric.MeterProvider // always non-nil; identity never changes
}
```

Only otel *API* types appear in `model`; the SDK stays behind
`pkg/services/metrics`.

## The Stable Provider

The one deep difference from tracing. A tracing restart swaps the whole
provider and callers re-resolve per span; metrics callers hold
*instruments* (`meter.Int64Counter(...)`), create them once, and cache
them — and an instrument minted from a retired provider records into
nothing, silently. So the metrics manager builds one SDK provider +
`ManualReader` pair at construction and **never replaces them**: `Init`
swaps only the exporter behind the shared reader. Cached instruments stay
valid for the life of the process; a restart changes where aggregated
state is exported, never what is recording. This identity is
`model.Meter`'s documented promise, and the package's
`TestInstrumentSurvivesRestart` pins it.

The consequence for callers is the exact inverse of tracing's rule:
tracing says *never* cache the provider; metrics says resolve
`Provider()` once, build instruments once, cache them, and never rebuild
on restart. That inversion is why the two handles are separate interfaces.

## The Service

```go
type Manager interface {
	model.Meter
	common.Liveness      // wiring only — see below
	common.Initializable // re-reads metrics.* keys, swaps the exporter
	common.Daemon        // Start runs the export loop; Stop flushes
	common.Debuggable
}

m := metrics.New(metrics.WithLogger(logger)) // all further config via metrics.* keys in Init
```

Lifecycle facts that matter:

- **`Init` runs on every restart** and re-reads the `metrics.*` keys
  through the same `applyOptions` + `setDefaults` pair the constructor
  uses — config is the authority; an absent key lands on the default. On
  the happy path the replacement exporter is built first and swapped in
  atomically, so a probe landing mid-restart still sees a live exporter.
  Nothing is flushed here — provider and reader stay, so whatever
  aggregated meanwhile is picked up by the next export cycle.
- **`Start` is a real Daemon** (unlike tracing's no-op): the
  `ManualReader` only collects when the export loop drives it. The loop
  ticks every `metrics.interval` (default 60s, the SDK's own cadence),
  collects, and exports — re-reading the interval each tick, so an `Init`
  that changed it takes effect without restarting the loop. An empty
  collection is skipped: a stdout exporter stays quiet instead of
  printing an empty envelope per interval.
- **`Stop` is the flush**: it ends the loop, runs one final
  collect+export bounded by a 5s shutdown budget — a stalled collector
  must not wedge the supervisor's stop sequence — then shuts the exporter
  down. The flush is best-effort: a dead collector loses the last
  window's data, never the shutdown.
- **`Alive` guards wiring only** (enabled but no exporter built — a
  restart rebuilds it; a deliberately stopped manager stays alive). There
  is deliberately **no `Ready`**, and export failures never fail `Alive`:
  metrics are auxiliary, the monitor restarts on liveness failure, and a
  dead collector must not flap the service — export errors surface in
  logs and in `Info` (last-export time and error under debug).
- **Register telemetry at the end of the infra tier.** The topo sort pops
  dependency-less services in reverse registration order, so the services
  listed last init first and, with stop reversed, flush last — after the
  draining api server's final requests
  ([../app/components-server.md](../app/components-server.md)).

## Exporters — registry + blank imports

Identical contract to tracing's ([tracing.md](tracing.md) "Exporters"): a
registry keyed by name, each subpackage registering a `Factory` in its
`init()`, binaries blank-importing only what they use, and an enabled
config naming an unimported exporter failing `Init` with "not registered".

```go
_ "github.com/xhanio/framingo/pkg/services/metrics/exporters/otlpgrpc" // metrics.OTLPGRPC = "otlpgrpc"
_ "github.com/xhanio/framingo/pkg/services/metrics/exporters/otlphttp" // metrics.OTLPHTTP = "otlphttp"
_ "github.com/xhanio/framingo/pkg/services/metrics/exporters/stdout"   // metrics.Stdout   = "stdout"
```

A `Factory func(ctx, config []byte) (sdkmetric.Exporter, error)` receives
the `metrics.config` block as raw YAML (nil when absent) and
strict-decodes its own shape — unknown keys fail Init. The otlp pair
shares the transport-agnostic core from `pkg/utils/otelutil` (`endpoint`,
`insecure`, `headers`, `compression`, `timeout`, `retry{enabled,
initial_interval, max_interval, max_elapsed_time}`), `otlphttp` adds
`url_path`, and `stdout` takes `pretty` and `timestamps` (both default
true). The one knob tracing has that metrics lacks is `max_request_size`
— a span-batching concern. Custom exporters are app-registered factories
(`metrics.Register("mine", factory)` — the closure captures what config
cannot express), never injected instances; the package's own tests inject
through the concrete manager's unexported field.

## Options

`WithLogger`, `WithName`, plus the `metrics.*`-backed trio re-applied on
every `Init`: `WithEnabled` (its own switch — choosing an exporter never
implies it), `WithExporter(name, config)` (registry name + the kv block
its factory strict-decodes; nil config = exporter defaults), and
`WithInterval` (zero reads as unset and lands on the 60s default — a
config key asking for literal zero also lands there, never on a spinning
loop).

The metric resource is the build's application identity and is **not
configurable** — `service.name`/`service.version` come from
`info.ApplicationName`/`info.ApplicationVersion` (ldflags-injected;
omitted, not blank, without the injection). Same rules as the span
resource.

## Config

```yaml
metrics:
  enabled: true        # default false — opt in
  exporter: stdout     # otlpgrpc | otlphttp | stdout (blank-import the subpackage)
  interval: 60s        # export loop period
  # config:                    # exporter-specific kv block, opaque to the manager
  #   endpoint: localhost:4317 # otlp: collector host:port, no scheme
  #   insecure: true
  #   headers: { authorization: "Bearer ..." }
  #   compression: gzip
  #   timeout: 10s
  #   retry: { initial_interval: 5s }
```

## Instruments in App Code

Business services never import this package: they consume `model.Meter`
through their narrow `Infra` view, build their instruments once in `Init`
(where failure is a first-boot error — an invalid instrument name is a
deterministic programmer mistake, not a runtime condition), cache them in
a `metrics` struct, and record with the service name as the
instrumentation scope. The full pattern with code is in
[../app/services.md](../app/services.md) "Service metrics".

`metrics.New()` alone — no Init, no config, no exporter — is already a
usable `model.Meter`: a real provider that aggregates into nothing
exported, the right stand-in for wiring and tests. For asserting on
recorded values, `pkg/utils/testutil` supplies `NewMeterRecorder()` (a
`ManualReader` + real SDK provider — create instruments, record,
`Collect`, assert; no flush, no sleep), `NewMeter(p)` (a bare
`model.Meter` serving that provider; deliberately no `Swap` counterpart
to testutil.Tracer's, since the production service never swaps
providers), and `SumInt64(rm, name)` (a counter's total out of one
collected export — assert on its `found` so a mistyped name fails the
test instead of reading zero).

## Gotchas

- Metrics are off unless `metrics.enabled` says so — wiring an exporter
  alone records into an SDK nothing exports. Enabled with no exporter
  named anywhere fails Init.
- The otlp `endpoint` is `host:port` with **no scheme**; otlp exporters
  connect lazily, so an unreachable collector fails neither Init nor any
  probe — it surfaces as export errors in logs and `Info`.
- Nothing recorded is lost to timing: the reader aggregates from
  construction, temporality is cumulative, and the first enabled export
  cycle ships the accumulated state — instruments created before
  `Start`, or during a disabled stretch, count the whole time.
- Don't create instruments per call — mint once, cache, `Add`/`Record`
  per call. Creation locks the SDK's instrument cache; recording is the
  hot path.
- Restart semantics are exporter-only: expect the same provider pointer
  across restarts, and treat any design that rebuilds instruments on
  restart as a smell — nothing requires it.
