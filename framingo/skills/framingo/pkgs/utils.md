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
- `graph/` - Generic directed graph with topological sort
- `buffer/` - Ring buffer with pooling
- `queue/` - FIFO queue
- `staque/` - Hybrid stack/queue with priority; items implement `staque.PriorityItem` = `common.Unique` + `common.Weighted` (ordered by priority, `Key()` as tiebreak)
- `trie/` - Prefix tree for string matching
- `lease/` - Time-based lease management

## Notable `pkg/utils/` Helpers

- `confutil` — `Config`, the read side of the app's viper (`New(viper)`), delivered through the `Init` context (`WrapContext`/`FromContext`, never nil). Getters take format-string keys — `GetInt("%s.connection.max_open", nameutil.ConfigRoot(m.name))` — so a service reads under its own root ([supervisor.md](supervisor.md) Configuration Pattern)
- `paramutil` — ordered key/value params and the notations they're spelled in (`a=b&c=d`, `a=b c=d`, command lines); backs env/arg merging and DSN building
- `nameutil` — service/router names: `Name(m)` is the package dir relative to the project root (judged from the gopro-injected `info.ProjectPath`; full import path without it) with the layout root (`nameutil.Root`, default `pkg`) stripped, joined with the type name — `services/db/manager`, `routers/user/router`. `Package(m)` is just the package dir's base name — the middleware default, where the package name is the middleware name (`middlewares/cors` → `cors`). `ConfigRoot(name)` is the config key root a service reads its dynamic keys under, derived from the name alone: the package segment of a category-visible name (`services/db/manager` → `db`), or the whole name when it has no path (`WithName("clickhouse")` → `clickhouse.connection.*`); services call it on `m.name` in `Init` and keep no second field
- `otelutil` — the shared plumbing behind the telemetry services: the name-to-factory `Registry` exporter subpackages register into, the transport-agnostic otlp config vocabulary (`Duration`, `Retry`, `Config`, the strict `Decode`), and the request attribute helpers the api server's metrics and the trace middleware's span share, so a series and a span never disagree about the same request: `RequestMethod` (the `http.request.method` convention's set or `_OTHER`) and `ProtocolVersion` (`network.protocol.version` as `1.1` or `2`)
- `reflectutil` — `Locate` (import path + type name), struct field scan/apply
- `certutil` — cert bundle loading for TLS options
- `cmdutil` — `New(bin, args, opts...)` command runner (context, env, dir, stdout/stderr writers, buffered output). `WithProcessGroup()` — or `UseProcessGroup(*exec.Cmd)` on a bare `exec.Cmd` — starts the command in its own process group and makes canceling its context SIGTERM the whole group, so the shells and workers it spawned stop too (a no-op where process groups don't exist). Without it, a canceled `sh -c`/`make` dies alone and its children keep the output pipes open
- `ioutil` — `LimitWriter` keeps the first N bytes written, `TailWriter` (`NewTailWriter(n)`) the last N in a fixed ring buffer, concurrency-safe — the end of a long command's output; plus file, tar and crypto helpers
- `cronutil` — `Parser`, the one cron grammar framingo schedules with (5 fields, optional leading seconds, `@daily`/`@every 30m`-style descriptors, optional `CRON_TZ=`), and `Validate(spec)` to reject a bad schedule up front with the task manager's own rules
- `sliceutil`, `maputil`, `strutil`, `netutil`, `timeutil` — small stateless helpers
