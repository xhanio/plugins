# Database Service

Located in `pkg/services/db`. Each engine (PostgreSQL, MySQL, SQLite, ClickHouse) lives in its own subpackage under `pkg/services/db/drivers/` and self-registers via `db.Register` in `init()`. The core `db` package does not import any concrete GORM/migrate driver — **blank-import the driver subpackage(s) your binary actually needs**, so SQLite-only binaries don't drag in the Postgres/MySQL/ClickHouse client libraries (~17MB saving).

## Creating a DB Manager

```go
import (
    "github.com/xhanio/framingo/pkg/services/db"

    // Blank-import only the engines this binary supports.
    _ "github.com/xhanio/framingo/pkg/services/db/drivers/postgres"
    // _ "github.com/xhanio/framingo/pkg/services/db/drivers/mysql"
    // _ "github.com/xhanio/framingo/pkg/services/db/drivers/sqlite"      // needs CGO_ENABLED=1
    // _ "github.com/xhanio/framingo/pkg/services/db/drivers/clickhouse"
)

dbMgr := db.New(
    // WithType takes a plain string; db.Postgres/MySQL/SQLite/Clickhouse are
    // string constants, so config.GetString("db.type") can be passed directly
    // — there is no separate parse step.
    db.WithType(db.Postgres),
    db.WithDataSource(db.Source{
        Host:     "localhost",
        Port:     5432,
        User:     "app",
        Password: "secret",
        DBName:   "mydb",
        Secure:   false,
        Params:   map[string]string{"sslmode": "disable"},
    }),
    // maxOpen, maxIdle, maxLifetime, maxIdleTime, execTimeout; a zero is unset and
    // takes the default (10, 5, 5m, never idle out, 30s) - but see Dynamic Config Keys
    db.WithConnection(10, 5, 5*time.Minute, 0, 30*time.Second),
    db.WithMigration("migrations", 0),            // directory, target version (0 = latest); "" runs none
    db.WithLogger(logger),
)
```

If `WithType` names a driver that hasn't been blank-imported, `db.Manager`'s `Init` fails with `unsupported db type: <name> (driver not registered — blank-import the corresponding pkg/services/db/drivers/* package)`.

Exact option signatures — **note `Source.Port` and `WithMigration`'s version are `uint`**, so read them with `GetUint`, not `GetInt`:

```go
func WithType(dbtype string) Option
func WithName(name string) Option                 // also the config root: "clickhouse" reads clickhouse.connection.*
func WithDataSource(source Source) Option
func WithMigration(sqlDir string, version uint) Option
func WithConnection(maxOpen, maxIdle int, maxLifetime, maxIdleTime, execTimeout time.Duration) Option
func WithPlugins(plugins ...gorm.Plugin) Option   // installed in order on every connect; repeated calls append
func WithLogger(logger log.Logger) Option         // Debug level also turns on GORM's statement log

type Source struct {
    Host     string
    Port     uint                 // GetUint
    User     string
    Password string               // print:"-": left out of Info
    DBName   string               // sqlite: the file path; empty is :memory:
    Secure   bool                 // postgres sslmode=require, mysql tls=true, clickhouse secure=true - unless Params sets it
    Params   map[string]string    // GetStringMapString; appended to the DSN, and on postgres they override the fields
}
```

## Drivers

Each `drivers/*` subpackage registers a `db.Driver` under its type name in `init()`:

```go
type Driver struct {
    Dialector func(dsn string) gorm.Dialector
    Migration func(sqlDB *sql.DB) (database.Driver, error) // golang-migrate's
    DSN       func(s Source) (string, error)
    Cleanup   func(db *gorm.DB, dbName string, schema bool) error
}

func Register(dbtype string, d Driver) // panics if dbtype is already registered
```

An engine framingo does not ship is a package of your own that calls `db.Register` the same way; its type name is then a valid `WithType`. A field left nil fails the operation that needs it — no `Migration` fails `Init` only when a migration directory is set, no `Cleanup` fails `Cleanup` and `Reload`.

## Migrations

Every `Init` connects anew — a supervisor restart re-inits — and swaps the new pool in whole, so a query reads one pool or the other, never one half built; the pool it replaces is closed once the queries already running on it finish. With a migration directory set, `Init` runs golang-migrate from `file://<dir>` right after connecting — every `Init`, so on every supervisor restart too: up to `version`, or all the way up with `0`. No change is a warning, not an error; any other failure fails `Init`. To migrate once per release instead of on every replica start, leave the directory empty in the daemon and run a one-off command that builds the same manager with it set and calls `Init` — the example's `exampleapp migrate` ([components-server.md](../app/components-server.md#migratego--the-one-off-schema-run)).

## Manager Interface

```go
type Manager interface {
    model.Database
    model.DatabaseDebugger
    common.Liveness      // Alive: guards the manager's wiring only (Init reconnects, so restart is the remedy)
    common.Readiness     // Ready: pings the database, capped at 3s - an unreachable server is not-ready, never a liveness failure
    common.Initializable
    common.Debuggable
}

// pkg/types/model
type Database interface {
    common.Service
    ORM() *gorm.DB                                                              // raw GORM access
    DB() *sql.DB                                                                // raw sql.DB access
    FromContext(ctx context.Context) *gorm.DB                                   // context-aware (extracts TX if present)
    FromContextTimeout(ctx context.Context, timeout time.Duration) (*gorm.DB, context.CancelFunc) // 0 = exec_timeout
    Transaction(ctx context.Context, fn func(tctx context.Context) error, opts ...*sql.TxOptions) error
}

type DatabaseDebugger interface {
    Cleanup(schema bool) error                                                  // empty every table (schema=true drops and recreates the schema)
    Reload() error                                                              // Cleanup(true) + re-migrate
}
```

The methods above split across two contracts. `model.Database` is what consumers hold — connections, queries, transactions — and the platform bundle hands it to repositories. `model.DatabaseDebugger` carries the destructive pair, `Cleanup` and `Reload`, for tests and dev tooling only; nothing holding a `model.Database` can reach them. Tests get both from `testutil.SetupDB()`, which returns the `db.Manager` on in-memory SQLite (needs `CGO_ENABLED=1`), and a test helper that only needs to clean up can return the narrower `model.DatabaseDebugger`. Each `SetupDB` call gets a database of its own, shared by every connection in its pool (a named in-memory database in shared-cache mode — a plain `:memory:` would give each connection a separate, empty one).

## Context-Aware Queries and Transactions

Always use `FromContext` in service/handler code to support transactions:

```go
// Simple query - automatically uses transaction if one exists in context
func (s *myService) GetUser(ctx context.Context, id string) (*User, error) {
    db := s.dbMgr.FromContext(ctx)
    var user User
    return &user, db.First(&user, "id = ?", id).Error
}

// Transaction - wraps fn in a DB transaction: commits on nil, rolls back on an
// error (returned as is) or a panic (re-panicked after the rollback)
err := s.dbMgr.Transaction(ctx, func(txCtx context.Context) error {
    // All FromContext calls within this fn use the same transaction
    if err := s.createOrder(txCtx, order); err != nil {
        return err // triggers rollback
    }
    return s.updateInventory(txCtx, order.Items) // committed if nil
})

// Manual context wrapping (advanced)
tx := dbMgr.ORM().Begin()
txCtx := db.WrapContext(ctx, tx)
```

A `Transaction` inside another — `ctx` already carries one — nests as a savepoint on that transaction rather than beginning a second one, so it takes no new connection (a one-connection pool cannot deadlock on it) and its `opts` are ignored. Its error rolls back to the savepoint only; the outer `fn` decides whether to return it.

## Tracing

Per-statement spans come from a GORM plugin, wired through `WithPlugins`:

```go
import (
    "github.com/xhanio/framingo/pkg/services/db/plugins/otelgorm"
    // framingo's own pkg/services/tracing already owns this name in most apps
    "gorm.io/plugin/opentelemetry/tracing"
)

p, err := otelgorm.New(tracingSvc, tracing.WithoutQueryVariables())
if err != nil {
    return errors.Wrap(err)
}
dbMgr := db.New(
    db.WithType(db.Postgres),
    db.WithPlugins(p),
)
```

It wraps `gorm.io/plugin/opentelemetry/tracing` and lives in its own
subpackage for the same reason the engine drivers do: that package imports the
ClickHouse, MySQL and PostgreSQL dialectors unconditionally (it type-switches
on them for `server.address`), so importing it from `pkg/services/db` would
link all three client libraries into every binary. Only a binary that wires the
plugin pays for it.

What framingo fixes on top of upstream:

- **The provider stays live.** The upstream plugin resolves
  `provider.Tracer(...)` once, at construction. `otelgorm` hands it a
  delegating provider (`traceutil.LiveProvider`) that re-resolves
  `model.Tracer.Provider()` on every span, so a tracing restart takes effect
  without rebuilding the database connection whatever `model.Tracer` it was
  given; the tracing service's own provider follows its restarts already.
- **Metrics reporting is off.** The plugin's DBStats half reaches for
  `otel.GetMeterProvider()`, the global framingo never touches.
`WithoutMetrics` is the only default `New` forces, because it is the only one
upstream gives no way to reverse. Everything else is the caller's, applied
after framingo's — `WithAttributes`, `WithDBSystem`, `WithoutQueryVariables`,
`WithQueryFormatter`, `WithRecordStackTrace`, `WithoutServerAddress` all pass
straight through.

> **Pass `WithoutQueryVariables()` unless the data is fixtures.** Upstream
> interpolates bound values into `db.query.text` by default, and framingo no
> longer overrides that. The values are where the credentials and the PII are,
> and the span travels to a backend with weaker access controls than the
> database. The placeholder form still names the statement and its table.

`New` returns an error rather than panicking, so a nil tracer — the plugin
wired before the tracing service is assigned — surfaces at wiring time instead
of as a nil dereference on the first query:

```go
p, err := otelgorm.New(tracingSvc,
    tracing.WithoutQueryVariables(),
    tracing.WithAttributes(attribute.String("deployment.environment", env)))
if err != nil {
    return errors.Wrap(err)
}
db.WithPlugins(p)
```

Overriding `WithTracerProvider` is the one override that costs something: it
replaces the live handle with a pinned provider, reintroducing the restart bug.

No new config key and no separate switch: with `tracing.enabled: false` the
handle serves a noop provider and the spans cost nothing. The db service does
**not** declare tracing in `Dependencies()` — the supervisor rolls a
dependency's probes into every dependent's healthcheck, and a dead collector
must never make the database unready.

### A span nests only if a context reached the statement

The plugin starts its span from `tx.Statement.Context`, so the question is
never which accessor you used — it is whether a context got as far as the
statement. Three forms carry one:

| Form | Span | Transaction from ctx |
|---|---|---|
| `FromContext(ctx)` | nests | picked up |
| `FromContextTimeout(ctx, d)` | nests | picked up |
| `ORM().WithContext(ctx)` | nests | **bypassed** |
| `ORM()` | detached root | bypassed |

`FromContext` is not doing anything exotic — its non-transaction branch *is*
`ORM().WithContext(ctx)`:

<!-- source: pkg/services/db/context.go -->
```go
func (m *manager) FromContext(ctx context.Context) *gorm.DB {
	tx, ok := ctx.Value(common.ContextKeyTX).(*gorm.DB)
	if !ok {
		return m.ORM().WithContext(ctx)
	}
	return tx
}
```

So `ORM().WithContext(ctx)` traces identically. What it drops is the other
half: an active transaction placed in the context by `Transaction(...)`. Use it
and the query runs on the base connection, outside that transaction — a
correctness problem well before it is a tracing one, and a deadlock on a
one-connection pool. That is why `FromContext(ctx)` stays the default.

Bare `ORM()` is the only form that loses the span. Queries issued that way are
still traced, but as **root spans**: real timings, no link to the request that
caused them, and a backend full of orphan single-span traces. Reach for it only
where there is genuinely no context — schema setup, tests.

## Dynamic Config Keys

During `Init(ctx)`, the DB manager re-reads these from the context `confutil.Config` and **replaces whatever `WithConnection` set at construction** — so a restart picks up pool changes without a rebuild. The replacement is whole: a key the config does not set falls back to the default, not to the constructed value, so set the pool through these keys rather than `WithConnection`. They are read under the instance's config root — `db` by default, or the `WithName` name, so a second instance (`db.WithName("clickhouse")`) reads `clickhouse.connection.*`:
- `db.connection.max_open` - max open connections
- `db.connection.max_idle` - max idle connections
- `db.connection.max_lifetime` - connection max lifetime
- `db.connection.max_idle_time` - idle connection max lifetime
- `db.connection.exec_timeout` - query execution timeout

The manager takes any GORM model; nothing requires a record to implement `orm.Record` ([types.md](types.md#orm-base-types)), though the example's records declare it.
