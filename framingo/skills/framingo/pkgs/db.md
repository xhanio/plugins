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
    // maxOpen, maxIdle, maxLifetime, maxIdleTime, execTimeout — all five required
    db.WithConnection(10, 5, 5*time.Minute, 0, 30*time.Second),
    db.WithMigration("migrations", 0),            // directory, target version (0 = latest)
    db.WithLogger(logger),
)
```

If `WithType` names a driver that hasn't been blank-imported, `db.Manager` startup fails with `unsupported db type: <name> (driver not registered — blank-import the corresponding pkg/services/db/drivers/* package)`.

Exact option signatures — **note `Source.Port` and `WithMigration`'s version are `uint`**, so read them with `GetUint`, not `GetInt`:

```go
func WithType(dbtype string) Option
func WithName(name string) Option
func WithDataSource(source Source) Option
func WithMigration(sqlDir string, version uint) Option
func WithConnection(maxOpen, maxIdle int, maxLifetime, maxIdleTime, execTimeout time.Duration) Option
func WithLogger(logger log.Logger) Option

type Source struct {
    Host     string
    Port     uint                 // GetUint
    User     string
    Password string
    DBName   string
    Secure   bool
    Params   map[string]string    // GetStringMapString
}
```

## Manager Interface

```go
type Manager interface {
    common.Service
    common.Initializable
    common.Debuggable
    ORM() *gorm.DB                                                              // raw GORM access
    DB() *sql.DB                                                                // raw sql.DB access
    FromContext(ctx context.Context) *gorm.DB                                   // context-aware (extracts TX if present)
    FromContextTimeout(ctx context.Context, timeout time.Duration) (*gorm.DB, context.CancelFunc)
    Cleanup(schema bool) error                                                  // truncate tables (schema=true drops schema)
    Reload() error                                                              // drop + re-migrate
    Transaction(ctx context.Context, fn func(ctx context.Context) error, opts ...*sql.TxOptions) error
}
```

## Context-Aware Queries and Transactions

Always use `FromContext` in service/handler code to support transactions:

```go
// Simple query - automatically uses transaction if one exists in context
func (s *myService) GetUser(ctx context.Context, id string) (*User, error) {
    db := s.dbMgr.FromContext(ctx)
    var user User
    return &user, db.First(&user, "id = ?", id).Error
}

// Transaction - wraps fn in a DB transaction, rolls back on error or panic
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

## Dynamic Config Keys

During `Init(ctx)`, the DB manager re-reads these from the context Viper, **overriding whatever `WithConnection` set at construction** — so a restart picks up pool changes without a rebuild:
- `db.connection.max_open` - max open connections
- `db.connection.max_idle` - max idle connections
- `db.connection.max_lifetime` - connection max lifetime
- `db.connection.max_idle_time` - idle connection max lifetime
- `db.connection.exec_timeout` - query execution timeout

DB records implement the `orm.Record` interface — see [types.md](types.md) for the ORM base types.
