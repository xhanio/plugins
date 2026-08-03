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

- `confutil` — Viper-from-context delivery ([supervisor.md](supervisor.md) Configuration Pattern)
- `paramutil` — ordered key/value params and the notations they're spelled in (`a=b&c=d`, `a=b c=d`, command lines); backs env/arg merging and DSN building
- `reflectutil` — `Locate` (package-path service names), struct field scan/apply
- `certutil` — cert bundle loading for TLS options
- `sliceutil`, `maputil`, `strutil`, `ioutil`, `netutil`, `timeutil` — small stateless helpers
