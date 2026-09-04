# Framingo Config YAML Reference

Reference config structure for all framework services. Use this as a template when creating new applications.

## Example Config YAML

```yaml
# Logging — used by log.New() in service.go
log:
  level: 0                    # zapcore.Level: -1=Debug, 0=Info, 1=Warn, 2=Error (raise/lower to filter)
  file: /var/log/myapp/app.log
  rotation:
    max_size: 100             # MB per log file
    max_backups: 3            # number of rotated files to keep
    max_age: 7                # days to retain old log files

# Database — used by db.New() options + dynamic config in db.Manager.Init().
# `db` is the instance's config root (nameutil.ConfigRoot of its name); a
# second instance built with db.WithName("clickhouse") reads under `clickhouse:`
db:
  type: postgres              # postgres | mysql | sqlite | clickhouse (the chosen type's driver subpackage must be blank-imported; sqlite needs CGO_ENABLED=1)
  source:
    host: localhost
    port: 5432
    user: myapp
    password: secret
    dbname: myapp_db
  migration:
    dir: ./migrations         # path to migration SQL files
    version: 0                # target version (0 = latest)
  connection:
    max_open: 10              # max open connections
    max_idle: 5               # max idle connections
    max_lifetime: 1h          # connection max lifetime
    max_idle_time: 30m        # idle connection max lifetime
    exec_timeout: 30s         # query execution timeout

# Tracing (OpenTelemetry) — read in tracing.Manager.Init(ctx), so changes
# apply on service restart
tracing:
  enabled: false              # the tracing switch - naming an exporter does not turn it
                              # on; disabled serves a noop provider (spans are free)
  exporter: stdout            # otlpgrpc | otlphttp | stdout — the matching
                              # pkg/services/tracing/exporters/* subpackage must be blank-imported
  sample_ratio: 1.0           # ParentBased(TraceIDRatioBased)
                              # service.name/version are NOT configurable at all: they are
                              # the build's ApplicationName/ApplicationVersion
  # config:                   # exporter-specific kv block — opaque to the manager,
  #   endpoint: localhost:4317   # strict-decoded by the chosen exporter's factory,
  #   insecure: true             # so a typo'd key fails startup (otlp keys shown;
  #   compression: gzip          # stdout takes pretty/timestamps instead)
  #   timeout: 10s
  #   retry: { initial_interval: 5s }

# API servers — iterated by m.config.GetStringMap("api") in service.go
# Each key becomes a named server instance via m.api.Add(name, ...)
api:
  health:                     # the probe listener: /healthz + /readyz mount
    host: 0.0.0.0             # here, off the public server (see the
    port: 8081                # example's routers/health)
    prefix: /
  http:                       # server name: "http"
    host: 0.0.0.0
    port: 8080
    prefix: /api/v1           # server endpoint path
    middlewares:              # optional: per-middleware default configs - a
      trace: true             # plain mapping, unordered; each block reaches
      cors: true              # its middleware wherever nothing more specific
      throttle:               # is configured (router.yaml overrides per group
        rps: 100.0            # or handler). "trace: true" activates the
        burst_size: 200       # framework's tracing middleware, "cors: true"
                              # the example's server-level cors middleware; no
                              # entry (see the health server) leaves one dormant
  admin:                      # server name: "admin"
    host: 0.0.0.0
    port: 9090
    prefix: /admin
  # HTTPS example with TLS
  # https:
  #   host: 0.0.0.0
  #   port: 443
  #   prefix: /api/v1
  #   cert: /path/to/server.crt
  #   key: /path/to/server.key

# TLS CA certificate (used when any api server has cert/key configured)
# ca:
#   cert: /path/to/ca.crt

# pprof profiling — optional, set port to enable
pprof:
  port: 6060                  # 0 = disabled

# Custom service config — read in service Init() via confutil.FromContext(ctx) under the service's config root
# example:
#   greeting: hello world!
```

**Notes**:
- `db.type` is matched against the driver registry; the corresponding `pkg/services/db/drivers/{postgres,mysql,sqlite,clickhouse}` subpackage must be blank-imported by the binary or `db.Manager.Init` returns `unsupported db type: <name> (driver not registered ...)`
- `db.type: sqlite` requires `CGO_ENABLED=1` and a C toolchain — its engine is `mattn/go-sqlite3`, a cgo wrapper around the C library. Built with `CGO_ENABLED=0` the binary still compiles, but `db.Manager.Init` fails at connect with `Binary was compiled with 'CGO_ENABLED=0', go-sqlite3 requires cgo to work`. This rules out cgo-free targets such as `FROM scratch` images and simple cross-compilation. The other drivers are pure Go.
- `db.connection.*` keys are read dynamically during `db.Manager.Init(ctx)` via `confutil.FromContext(ctx)`, allowing values to change on service restart. They are read under the instance's **config root**: `db` for the default instance, or the name given to `db.WithName` — so two databases live under two roots:
  ```yaml
  postgres:
    connection: { max_open: 10 }
  clickhouse:
    connection: { max_open: 4 }
  ```
  with `db.New(db.WithName("postgres"), ...)` and `db.New(db.WithName("clickhouse"), ...)` in the wiring layer, which reads the static keys (`type`, `source.*`, `migration.*`) under the same roots
- `api.*` is iterated as a string map — each top-level key under `api` becomes a named server instance
- TLS is enabled per-server when `api.<name>.cert` is set
- `api.<name>.middlewares` is passed to `server.WithMiddlewareConfigs` as each middleware's per-server default config; a router.yaml entry carrying its own block still wins (handler over group over server). Server-level middlewares (`WithMiddlewares`) are activated by the same mapping — an entry enables (`cors: true`, `cors:`, or a policy block), no entry leaves one dormant, and booleans anywhere in the mapping are switches, not config, arriving as `Func`'s `enabled` argument; the lifecycle built-ins take no config and claim no names. Names in this mapping are matched exactly and a typo'd one silently configures nothing (unlike router.yaml refs, which fail registration); viper lowercases keys, so keep middleware names lowercase. YAML 1.1 truthiness applies — `cors: yes`/`on` enable it too
- Custom service config keys are accessed in `Init(ctx)` via `confutil.FromContext(ctx).GetString("%s.key", nameutil.ConfigRoot(m.name))` — a format string, so the root follows the service's name (see [app/services.md](../app/services.md))
- Pubsub, messagebus, and planner services are configured entirely via functional options, not YAML keys
- `tracing.*` keys (or `<name>.*` for a `WithName`d instance) are read dynamically in `tracing.Manager.Init(ctx)` — a set key wins over the constructed options and stays sticky across restarts until overridden. Per-server activation is the standard middleware mapping: `trace: true` under `api.<name>.middlewares` enables the framework's tracing middleware on that server ([tracing.md](tracing.md))
