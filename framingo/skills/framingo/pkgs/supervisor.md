# Supervisor & Service Lifecycle — `pkg/services/supervisor`

How framingo services are shaped, orchestrated, and configured. Writing a
service of your own is the app half of this story —
[services.md](../app/services.md).

## Service Lifecycle Interfaces

All services compose from interfaces in `pkg/types/common`:

```go
// Required - every service must implement this
type Service interface {
    Named                       // Name() string
    Dependencies() []Service    // declare startup dependencies
}

// Optional lifecycle interfaces - implement as needed
type Initializable interface { Init(ctx context.Context) error }       // setup (called on start AND restart)
type Daemon interface { Start(ctx context.Context) error; Stop(wait bool) error }  // long-running
type Liveness interface { Alive(ctx context.Context) error }           // health probe (failure = auto-restart)
type Readiness interface { Ready(ctx context.Context) error }          // readiness probe (failure = reported only)
// Probes take the caller's context - probes may do I/O (a database ping), and
// the caller owns the deadline and shutdown signal. Implementations may layer
// a tighter timeout on top, never a looser one.
type Debuggable interface { Info(w io.Writer, debug bool) }            // debug output
```

### Identity interfaces

`pkg/types/common` also defines three small identity interfaces that
services and their satellites lean on:

```go
type Named interface { Name() string }                    // embedded in Service
type Unique interface { Key() string }                    // stable dedup/ordering key
type Weighted interface { GetPriority() int; SetPriority(priority int) }
```

- `Named` is the currency of identity everywhere: `log.Logger.By(caller
  common.Named)` is why every constructor ends with `m.log = m.log.By(m)`,
  `messagebus.Register(module common.Named)` takes it, and the supervisor's
  dependency graph (`pkg/structs/graph`) is generic over it.
- `Unique` + `Weighted` together form `staque.PriorityItem` — the contract
  of the priority stack/queue ([utils.md](utils.md)). `task.Task` implements
  both, which is how the task manager orders its queue: by priority, then by
  `Key()` as the tiebreak.

## Supervisor

Orchestrates all services. Located in `pkg/services/supervisor`.

```go
import (
    "github.com/spf13/viper"
    "github.com/xhanio/framingo/pkg/services/supervisor"
)

// Create manager with viper config
mgr := supervisor.New(config,
    supervisor.WithLogger(logger),
    supervisor.WithMonitorPolicy(30*time.Second, 3, 0), // sweep cadence, restarts per service, restart delay
)

// Register services (order doesn't matter - topologically sorted)
mgr.Register(dbService, apiServer, pubsubBus, myService)

// Sort, init, start
mgr.TopoSort()
mgr.Init(ctx)
mgr.Start(ctx)
```

Full signature set — `Register` and `TopoSort` differ in whether they return an error, so don't guess:

```go
type Manager interface {          // = model.Supervisor + health + Initializable + Daemon + Debuggable
    Name() string
    Dependencies() []common.Service
    Register(services ...common.Service)          // no return value
    TopoSort() error
    Services() []common.Service
    Stats() ([]*entity.SupervisorStats, error)    // point-in-time copies, topological order: dependencies above dependents

    Init(ctx context.Context) error
    Start(ctx context.Context) error
    Stop(wait bool) error
    Info(w io.Writer, debug bool)

    Alive(ctx context.Context) error              // health, on Manager not model.Supervisor: fails only when recovery is spent (a service dead with restarts exhausted)
    Ready(ctx context.Context) error              // health: the roll-up, nil iff every registered service is ready

    InitService(ctx context.Context, name string) error
    StartService(name string) error
    StopService(name string, wait bool) error
    RestartService(ctx context.Context, name string) error
    Restart(ctx context.Context) error             // whole graph
}
```

The manager:
- Resolves dependencies via topological sort
- Calls `Init(ctx)` on `Initializable` services in dependency order — optionally waiting for dependencies, see [Waiting at Init](#waiting-at-init) below
- Calls `Start(ctx)` on `Daemon` services
- Monitors `Liveness` and `Readiness` probes — see [Health Probes & Escalation](#health-probes--escalation) below
- Monitoring and restarts tune via `WithMonitorPolicy(interval, maxRetries, restartDelay)` — sweep cadence (0, the default, disables monitoring), in-process restarts per service, pause before each restart; `WithStopPolicy(timeout)` bounds the whole shutdown (0, the default, waits indefinitely)

## Waiting at Init

By default `Init` is one pass: each service's turn checks only that its
dependencies' `Init` succeeded, and fails otherwise — no probing, no
waiting. `WithInitPolicy` turns each turn into a bounded retry loop for
slow-starting infrastructure (a database still bootstrapping when the
process boots):

```go
mgr := supervisor.New(config,
    // maxRetries: 0 off (default), n retries, -1 until ctx cancels;
    // the wait starts at 1s and doubles up to 30s
    supervisor.WithInitPolicy(-1, time.Second, 30*time.Second),
)
```

With the policy on, a turn waits until every dependency is *init-ready* —
its `Init` succeeded **and**, if it implements `Readiness`, its
`Ready(ctx)` probe answers nil — then runs the service's own `Init`. Both
blockages are transient and retry with the backoff: a dependency whose
ping hasn't answered yet, and the service's *own* failing `Init`, which
retries at its own turn. Because the walk is topological, a dependency
always resolves at its own turn first — no service ever re-runs another
service's `Init`; dependents only observe. A dependency that exhausted a
bounded policy is permanent, and dependents fail fast with "dependencies
not ready" instead of burning their own budget.

For bootstrap-style dependencies prefer `WithInitPolicy(-1)`: during init
there is no traffic to protect, and giving up after k tries just moves
the retry to the platform as a crash loop. The bound still exists twice —
the `Init` context (your signal handler cancels it, and cancellation cuts
a wait short mid-delay) and the platform's own startup timeout.

## Health Probes & Escalation

One rule decides what goes in which probe, everywhere: **`Alive` fails only
if a restart would fix it; `Ready` means "can it serve right now".** A
dependency outage is the classic trap — the repository must not fail
liveness when the database is unreachable, because restarting the
repository raises no database; it fails readiness instead. Every
implementer in the tree keeps that split:

| Implementer | `Alive(ctx)` fails when (restart is the remedy) | `Ready(ctx)` fails when (stop routing traffic) |
|---|---|---|
| `db.Manager` | no connection handle (`Init` reconnects) | database ping fails |
| api server manager | a listener stopped serving (`Init` rebuilds echo and re-binds) | same — not accepting = not ready |
| example `repository` | no database handle | database ping fails |
| the supervisor itself | recovery spent: a service failing liveness with restarts exhausted | any registered service not ready (the roll-up) |

How the monitor consumes them:

- **Each sweep probes every service exactly once.** A shared dependency is
  checked once and its result reused by every dependent, with failures
  still rolling up into each dependent's healthcheck error.
- **Only liveness failure triggers restart**; readiness is reported. The
  probe context is the monitor's own — cancellation at shutdown reaches a
  ping in flight.
- **`WithMonitorPolicy`'s maxRetries sets the escalation regime**:
  `n > 0` restarts up to n times, then the supervisor gives up — and its
  own `Alive()` goes red; `0` disables in-process restarts, so a liveness
  failure escalates immediately (the platform is the recovery path);
  `-1` retries forever and never escalates.

The supervisor's own verdicts live on `supervisor.Manager` beside its
lifecycle half — model interfaces stay lifecycle-free, and the example's
health router consumes them through its own narrow interface
([routers.md](../app/routers.md)). That completes the escalation ladder:
service restart is the supervisor's job, pod restart is the platform's —
`Ready()` feeds `/readyz` (stop routing, keep the pod), `Alive()` feeds
`/healthz` (replace the pod). Writing probes for your own service:
[services.md](../app/services.md).

**The supervisor does NOT install signal handlers.** There is no `os/signal` anywhere in framingo. Trapping SIGINT/SIGTERM/SIGHUP/SIGUSR1/SIGUSR2 and calling `Stop`/`Restart` is application code you write in `pkg/components/server/<app>/signal.go` — see [layout.md](../app/layout.md).

## Configuration Pattern

Framingo uses instance-based Viper (NOT the global singleton). Config is propagated via `context.Context`:

```go
import "github.com/xhanio/framingo/pkg/utils/confutil"

// In Init(ctx), read dynamic config:
func (s *myService) Init(ctx context.Context) error {
    config := confutil.FromContext(ctx)
    s.setting = config.GetString("my.setting")
    return nil
}
```

**You never call `WrapContext` yourself in a service.** The supervisor wraps the viper instance it was constructed with (`supervisor.New(config, ...)`) into the context it passes to every service's `Init` — that's the whole delivery mechanism.

`FromContext` **never returns nil**: with no config in the context it returns an empty `viper.New()`, so every getter yields the zero value. No nil check is needed, but it also means a missing config looks like "all defaults" rather than an error — if a setting is mandatory, validate it in `Init`.

Priority: CLI flags > env vars > YAML file > defaults.

For the full annotated YAML template (log, db, api, pprof, custom service keys) and dynamic-key notes, see [config.md](config.md).
