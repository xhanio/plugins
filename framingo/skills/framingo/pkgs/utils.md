# Utilities — Logging, Data Structures, Helpers

## Logging

Located in `pkg/utils/log`. Built on zap.

```go
import "github.com/xhanio/framingo/pkg/utils/log"

// Build the app's root logger once, in components/server/<app>/service.go.
// Without this the `log:` block in config.yaml is inert.
logger := log.New(
    log.WithLevel(config.GetInt("log.level")),            // -1=Debug 0=Info 1=Warn 2=Error
    log.WithFileWriter(                                    // file, maxSize(MB), maxBackups, maxAge(days)
        config.GetString("log.file"),
        config.GetInt("log.rotation.max_size"),
        config.GetInt("log.rotation.max_backups"),
        config.GetInt("log.rotation.max_age"),
    ),
    // log.NoStdout(),          // file only — suppress the console core
    // log.WithTimeFormat(fmt), // override the timestamp layout
)

// Scope it per service — By() prefixes records with the service name.
logger = logger.By(myService)   // takes a common.Named
logger.Infof("started on port %d", port)
logger.Debugf("processing request %s", id)
logger.Errorf("failed: %s", err)
```

Exact signatures — the option type is `log.Option`, so you can build a slice conditionally (e.g. only add the file writer when `log.file` is set):

```go
func New(opts ...Option) Logger
func WithLevel(level int) Option
func WithFileWriter(file string, maxSize, maxBackups, maxAge int) Option
func WithTimeFormat(format string) Option
func NoStdout() Option
```

`log.Default` is a package-level logger at Debug level — fine for tests and examples, but an app that reads config should build its own with `log.New`.

`Logger` also exposes `Sugared() *zap.SugaredLogger`, `Level() zapcore.Level`, `With(args ...any) Logger`, and the `Debug/Info/Warn/Error/Fatal` families in plain, `-ln`, and `-f` forms.

## Data Structures

Available in `pkg/structs/`:
- `graph/` - `Graph[T common.Named]`: `Add(node, deps...)`, `TopoSort()` (fails on a cycle), `Nodes()` in sorted order — the supervisor's dependency graph, keyed on `Name()`
- `buffer/` - read/write buffers drawn from a slice pool: `NewPool[T](sizes...)` (size classes, with hit stats) and `NewPooledBuffer(size, pool)`; `Pool`/`PooledBuffer` are the byte forms
- `queue/` - `NewDoubleBufferQueue[T](ctx, initialSize, swapInterval)`: writers fill one pooled buffer while readers drain the other, swapped on the interval
- `staque/` - Hybrid stack/queue with priority; items implement `staque.PriorityItem` = `common.Unique` + `common.Weighted` (ordered by priority ascending, `Key()` as tiebreak; `WithLessFunc` replaces the order — the task manager's breaks ties by arrival). `Pop`/`Shift` never wait; `WaitPop(ctx)`/`WaitShift(ctx)` wait while it is empty and, once `ctx` ends, return its error and take nothing. Change a queued element's priority only through `Update(items...)`, which moves them all in one go or, if a key is not queued, none (`NotFound`): setting it on the element itself leaves it out of place. `NewSimple[T](maxSize)` is the plain bounded stack/queue, with `PopN`/`ShiftN`
- `trie/` - `trie.New[T]()`, a prefix tree of string keys to values, safe for concurrent use: `Add`, `Find`, `Remove`, `HasKeysWithPrefix`, `Keys` (empty, never nil), `PrefixSearch` (nil when nothing matches), `FuzzySearch` (keys holding the pattern's runes in order, sorted)
- `lease/` - `lease.New(id, duration, opts...)`: a lease that expires unless refreshed, extended or renewed, with `OnExpired`/`OnCancel`/`OnRefresh`/`OnExtend`/`OnRenew` hooks; `Once()` and `UseWallTime()` as options

## Notable `pkg/utils/` Helpers

- `confutil` — `Config`, the read side of the app's viper (`New(viper)`), delivered through the `Init` context (`WrapContext`/`FromContext`, never nil). Getters take format-string keys — `GetInt("%s.connection.max_open", nameutil.ConfigRoot(m.name))` — so a service reads under its own root ([supervisor.md](supervisor.md) Configuration Pattern)
- `paramutil` — ordered key/value params and the notations they're spelled in (`a=b&c=d`, `a=b c=d`, command lines); backs env/arg merging and DSN building
- `nameutil` — service/router names: `Name(m, strip...)` is the package dir relative to the project root (judged from the gopro-injected `info.ProjectPath`; full import path without it) with the layout root (`nameutil.Root`, default `pkg`) stripped, joined with the type name — `services/db/manager`, `routers/user/router`; a `strip` prefix, whole segments, comes off as well. `Package(m)` is just the package dir's base name — the middleware default, where the package name is the middleware name (`middlewares/cors` → `cors`). `ConfigRoot(name)` is the config key root a service reads its dynamic keys under, derived from the name alone: the package segment of a category-visible name (`services/db/manager` → `db`), or the whole name when it has no path (`WithName("clickhouse")` → `clickhouse.connection.*`); services call it on `m.name` in `Init` and keep no second field
- `osutil` — queries about the operating system, with no process-wide state: `Timezone()` is the zone the system is configured with, as timedatectl reports it on a systemd host, and an error where it reports none. For plain local time use `time.Local`; the task manager reads schedules in `time.Local` unless given `task.WithLocation`
- `otelutil` — the shared plumbing behind the telemetry services: the name-to-factory `Registry` exporter subpackages register into, the transport-agnostic otlp config vocabulary (`Duration`, `Retry`, `Config`, the strict `Decode`), and the request attribute helpers the api server's metrics and the trace middleware's span share, so a series and a span never disagree about the same request: `RequestMethod` (the `http.request.method` convention's set or `_OTHER`) and `ProtocolVersion` (`network.protocol.version` as `1.1` or `2`)
- `envutil` — `EnvPrefix(name)` (upper-cased, spaces and dashes to `_`: the env prefix the wiring layer derives from `info.ProductName`, [config.md](config.md#environment-overrides)) and `Merge(sets...)`, which merges `KEY=value` lists left to right — a repeated key keeps its first position and its last value
- `syncutil` — `WaitGroup`, a `sync.WaitGroup` whose `Wait(ctx)` gives up with `ctx.Err()` once `ctx` ends and whose `Add` is legal at any time, even while a `Wait` is in progress; `Go(f)` counts a goroutine. The zero value is ready. It is what a `Daemon` keeps across starts to count its work. `Loop` is the goroutine that takes new work: `Start(ctx, run)` runs one at a time (false while one runs), under a context keeping `ctx`'s values but never its cancel or deadline, `Stop()` cancels it and returns once `run` has, with no deadline, and `Running()` reports it; the caller serializes `Start` and `Stop`, as the supervisor does ([supervisor.md](supervisor.md#the-daemon-contract))
- `job` — the unit the task manager runs: `job.New(id, fn, opts...)` with `job.Func func(job.Context) error` (`job.Wrap` adapts a `func(context.Context) error`), labels (`WithLabel`/`WithLabels`), states (`created`, `running`, `canceling`, `succeeded`, `failed`, `canceled`), `Cancel`, `Signal(job.SignalKill)`, progress and stats; `job/executor` wraps one with `WithTimeout`/`WithRetry`. Signals and `jobs.Command`: [plan.md](plan.md#commands-and-job-signals)
- `traceutil` — `Follow(current)`, `LiveProvider(model.Tracer)`, `Start(ctx, scope, name)`, `Fail(span, err)`: [tracing.md](tracing.md)
- `testutil` — test doubles and helpers: `SetupDB()` (an in-memory SQLite `db.Manager`, [db.md](db.md)), `NewTracer`/`NewRecorder` ([tracing.md](tracing.md)), `NewMeter`/`NewMeterRecorder`/`SumInt64`/`Histogram` ([metrics.md](metrics.md))
- `printutil` — `NewTable(w)`, the aligned tables every `Info(w, debug)` prints (`Header`, `Title`, `Row`, `Object`, `Flush`)
- `pageutil` — `PaginationParams` (`IsPaginated`, `Offset`, `Limit`) and `NewPagination(items, total, params)`, the paged response shape
- `reflectutil` — `Locate` (import path + type name), struct field scan/apply
- `certutil` — X.509 bundles: loading them for TLS options, and a CA signing with them. `SignServer`/`SignClient` generate the key; `SignCSR(csr, &CSRRequest{ExtKeyUsage: []x509.ExtKeyUsage{...}, CommonName: ...})` signs a requester's key instead and returns the certificate without one — `ParseCSR` checks the CSR's signature (the requester holds the key), `NewCSR(key, cn, dns, ips)` makes one. The request, not the CSR, decides the identity: a CSR naming another CN, DNS name or IP is refused. Serial numbers are random; a zero `ValidPeriod` lasts as long as the CA. `EncodePKCS12(bundle, password)` reverses `ParsePKCS12` — certificate, key and chain, AES-256 with PBKDF2 — and refuses a bundle without a key
- `cmdutil` — `New(bin, args, opts...)` command runner (context, env, dir, input, stdout/stderr writers). `Start` returns without waiting and `Wait` waits; `Run` does both. It keeps `exec.Cmd`'s contract: a command runs once, `Wait` is called once and never concurrently — a second call is an error, not a repeat of the first one's result — and `Output`/`Error`/`ExitCode` are read after `Wait` (or `Run`) returns; read while it runs they race exec's copying goroutines. A stream given no writer is collected for `Output`/`Error`, bounded by `WithMaxBuffer`; to follow output live, give the stream a writer (`WithStdout`/`WithStderr`), which exec writes from its own goroutine. The async pattern is `jobs.Command`'s: `Start`, `go func() { done <- p.Wait() }()`, read `ExitCode` only after receiving from `done`. `WithProcessGroup()` — or `UseProcessGroup(*exec.Cmd)` on a bare `exec.Cmd` — starts the command in its own process group and makes canceling its context SIGTERM the whole group, so the shells and workers it spawned stop too (a no-op where process groups don't exist). To stop it yourself instead of through a context, `Terminate`/`Kill` send it, or its group, a SIGTERM/SIGKILL; `SetProcessGroup`, `TerminateProcessGroup` and `KillProcessGroup` do the same on a bare `exec.Cmd`. `MergeArgs(sets...)` merges argument lists the way `envutil.Merge` does env lists, a repeated flag keeping its first position and its last values. `jobs.Command` builds on it (see [plan.md](plan.md)). Without it, a canceled `sh -c`/`make` dies alone and its children keep the output pipes open
- `jobs` — predefined job functions: `jobs.Command` runs a command as a job (see [plan.md](plan.md#commands-and-job-signals)). Ready-made job functions like it go here.
- `plans` — predefined kinds of plan: `plans.NewCommand(plan)` reads a command plan into its log header and what each run does, which the plan manager runs inside a run's bookkeeping.
- `ioutil` — `LimitWriter` keeps the first N bytes written (reporting the rest written, so it never short-writes), `LimitReader` errors once more than N bytes arrive instead of returning EOF, so truncated untrusted input never looks complete, `TailWriter` (`NewTailWriter(n)`) the last N in a fixed ring buffer, concurrency-safe — the end of a long command's output; `ProgressWriter`, plus file, tar and crypto helpers
- `cronutil` — `Parser`, the one cron grammar framingo schedules with (5 fields, optional leading seconds, `@daily`/`@every 30m`-style descriptors, optional `CRON_TZ=`), and `Validate(spec)` to reject a bad schedule up front with the task manager's own rules
- `wsutil` — WebSocket helpers. `Bridge(conn, messenger, opts...)` joins the connection to a bus `Messenger` both ways (bus → ws as JSON, JSON frames → `Messenger.Send`), blocks until the connection ends, then closes the messenger — the connection stays the caller's to close; `WithPing(interval, timeout)` tunes the server-side pings (30s/10s by default, interval `0` disables them; a pong missing past the timeout ends the session), `WithLogger` where the session logs. It depends only on `model.Messenger`, so any bridge to the bus goes here, not on the messagebus. `Close(ctx, conn, err)` ends a connection once its handler returned `err` — normal closure for nil, nothing for the peer's own close, released with nothing sent when the peer dropped it (EOF), going away once `ctx` ended, otherwise an internal error carrying `err`, the one case it returns `err` for the caller to log; the api server closes every WS handler's connection with it
- `sliceutil`, `maputil`, `strutil`, `netutil`, `timeutil`, `pathutil` — small stateless helpers
