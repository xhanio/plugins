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
    Dependencies() []Service    // declare startup dependencies; a nil entry is skipped
}

// Optional lifecycle interfaces - implement as needed
type Initializable interface { Init(ctx context.Context) error }       // setup (called on start AND restart)
type Daemon interface { Start(ctx context.Context) error; Stop(ctx context.Context) error }  // long-running
type Liveness interface { Alive(ctx context.Context) error }           // health probe (failure = auto-restart)
type Readiness interface { Ready(ctx context.Context) error }          // readiness probe (failure = reported only)
// Probes take the caller's context - probes may do I/O (a database ping), and
// the caller owns the deadline and shutdown signal. Implementations may layer
// a tighter timeout on top, never a looser one.
type Debuggable interface { Info(w io.Writer, debug bool) }            // debug output
```

### The `Daemon` contract

`Start` and `Stop` are called on one instance, and more than once: the supervisor's restarts
run `Stop`, `Init` and `Start` on the same service.

- `Start` on a started service does nothing.
- `Stop(ctx)` takes no new work, cancels the work going on, and waits for all of it to end,
  what an earlier `Stop` left included, until `ctx` ends. With `context.Background()` it
  waits as long as that takes.
- When `ctx` ends first, `Stop` ends the work hard where it can, and returns an error wrapping
  `ctx.Err()`, leaving the rest to end on its own. The task manager sends its executions
  `job.SignalKill`; the API server closes the connections still open. A `ctx` that has
  already ended makes it a forced stop.
- Either way, `Stop` returns only after whatever takes new work, a run loop or a consumer, has
  returned. That takes no longer than the cancel, and keeps two starts from both taking work.
- `Start` works right after any `Stop`. What a stop left runs on until it ends, and must not
  reach into the new start.
- `Stop` on a stopped service waits for what an earlier one left, until `ctx` ends. The
  supervisor, which leaves that to its services, returns at once.

The bound is the caller's, carried by the context:

- `WithStopPolicy(timeout)` gives the supervisor's whole shutdown, and the stop of each
  restart, a deadline. Past it, each service still to stop gets an ended context.
- A signal handler that hears the signal again cancels the context of the stop going on,
  which forces it ([components-server.md](../app/components-server.md#signalgo--os-signal-handling)).
- An API that stops on request turns the grace period it was given into the context, with
  `context.WithTimeout`.

Keep on the manager what lasts across starts: the service's state, and a `syncutil.WaitGroup`
that counts the work of every start, whose `Wait(ctx)` gives up when `Stop`'s context ends.
Give a start its own context, and its own of whatever hands out work or capacity, such as a
semaphore; what a stop leaves holds only those of the start it came from. A goroutine takes
them as arguments, never reading them back from the manager, where the next start replaces
them. [services.md](../app/services.md#lifecyclego) shows the shape.

### Identity interfaces

`pkg/types/common` also defines three small identity interfaces that
services and their satellites lean on:

```go
type Named interface { Name() string }                    // embedded in Service
type Unique interface { Key() string }                    // stable dedup/ordering key
type Weighted interface { Priority() int; SetPriority(priority int) }
```

- `Named` is the currency of identity everywhere: `log.Logger.By(caller
  common.Named)` is why every constructor ends with `m.log = m.log.By(m)`,
  `messagebus.Register(module common.Named)` takes it, and the supervisor's
  dependency graph (`pkg/structs/graph`) is generic over it.
- `Unique` + `Weighted` together form `staque.PriorityItem` — the contract
  of the priority stack/queue ([utils.md](utils.md)). The task manager's queue
  entries implement both; it orders them by priority, lower first, then by when
  they were queued.

## Supervisor

Orchestrates all services. Located in `pkg/services/supervisor`.

```go
import (
    "github.com/xhanio/framingo/pkg/services/supervisor"
    "github.com/xhanio/framingo/pkg/utils/confutil"
)

// Create manager with the app's config — the wiring layer's viper, wrapped once
mgr := supervisor.New(confutil.New(config),
    supervisor.WithLogger(logger),
    supervisor.WithMonitorPolicy(30*time.Second, 3, 0), // sweep cadence, restarts per service, restart delay
)

// Register services (order doesn't matter - topologically sorted)
if err := mgr.Register(dbService, apiServer, pubsubBus, myService); err != nil {
    return err // a name taken by another instance
}

// Sort, init, start
if err := mgr.TopoSort(); err != nil {
    return err
}
mgr.Init(ctx)
mgr.Start(ctx)
```

**A name stands for one instance.** The dependency graph, the stats and the by-name operations all key on `Name()`, so `Register` refuses another instance under a name already taken — registered before, or earlier in the same call, or brought in as a dependency — with a `Conflict`; it is a wiring mistake, so fail startup on it. Meeting the same instance again (registered directly and as someone's dependency) is fine. Two instances of one service type need distinct names: `WithName` on the framework services. A nil service is skipped, and so is a nil entry in any `Dependencies()` list — registration, the init wait and the health sweep all pass over it — so a service can list an optional dependency the app did not wire. `Register` is wiring-time (it is not locked against the monitor): call it, then `TopoSort`, before `Init`.

Full signature set:

```go
type Manager interface {          // = model.Supervisor + health + Initializable + Daemon + Debuggable
    Name() string
    Dependencies() []common.Service
    Register(services ...common.Service) error    // Conflict on a name another instance holds
    TopoSort() error
    Services() []common.Service
    Stats() ([]*entity.SupervisorStats, error)    // point-in-time copies, topological order: dependencies above dependents;
                                                  // the error combines each stat's Healthcheck() (stopped, init or start error)

    Init(ctx context.Context) error
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Info(w io.Writer, debug bool)

    Alive(ctx context.Context) error              // health, on Manager not model.Supervisor: fails only when recovery is spent (a service dead with restarts exhausted)
    Ready(ctx context.Context) error              // health: the roll-up, nil iff every registered service is ready

    InitService(ctx context.Context, name string) error
    StartService(name string) error
    StopService(ctx context.Context, name string) error
    RestartService(ctx context.Context, name string) error
    Restart(ctx context.Context) error             // whole graph
}
```

The manager:
- Resolves dependencies via topological sort
- Calls `Init(ctx)` on `Initializable` services in dependency order — optionally waiting for dependencies, see [Waiting at Init](#waiting-at-init) below
- Calls `Start` on `Daemon` services in the same order, each with `context.Background()`: the `ctx` given to the supervisor's own `Start` bounds only its monitor, never a service's run
- Stops them in reverse topological order, dependents before their dependencies
- Monitors `Liveness` and `Readiness` probes — see [Health Probes & Escalation](#health-probes--escalation) below
- Monitoring and restarts tune via `WithMonitorPolicy(interval, maxRetries, restartDelay)` — sweep cadence (0, the default, disables monitoring), in-process restarts per service, pause before each restart; `WithStopPolicy(timeout)` bounds the whole shutdown and the stop of each restart, forcing what is left once it passes (0, the default, leaves the bound to the context `Stop` gets); `WithName` and `WithLogger` as on every service

## Waiting at Init

By default `Init` is one pass: each service's turn checks only that its
dependencies' `Init` succeeded, and fails otherwise — no probing, no
waiting. `WithInitPolicy` turns each turn into a bounded retry loop for
slow-starting infrastructure (a database still bootstrapping when the
process boots):

```go
mgr := supervisor.New(confutil.New(config),
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
dependency outage is the classic trap — a service must not fail liveness
when the database it uses is unreachable, because restarting the service
raises no database; it fails readiness instead, or better, probes nothing
and lets the database's own readiness roll up to it. Every implementer in
the tree keeps that split:

| Implementer | `Alive(ctx)` fails when (restart is the remedy) | `Ready(ctx)` fails when (stop routing traffic) |
|---|---|---|
| `db.Manager` | no connection handle (`Init` reconnects) | database ping fails (capped at 3s) |
| api server manager | a listener stopped serving (`Init` rebuilds echo and re-binds) | same — not accepting = not ready |
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

Framingo uses instance-based Viper (NOT the global singleton), seen by services only through `confutil.Config` — the read side, propagated via `context.Context`:

```go
import (
    "github.com/xhanio/framingo/pkg/utils/confutil"
    "github.com/xhanio/framingo/pkg/utils/nameutil"
)

// In Init(ctx), read dynamic config under the service's root:
func (m *manager) Init(ctx context.Context) error {
    config := confutil.FromContext(ctx)
    root := nameutil.ConfigRoot(m.name)
    m.setting = config.GetString("%s.setting", root)
    return nil
}
```

Every getter takes a **format string**: with no args it is the key itself (`GetString("my.setting")`), otherwise it is Sprintf-ed first. That is how a service reads under its own root instead of a hardcoded one — `nameutil.ConfigRoot(m.name)` is the whole name when `WithName` gave one, or the package segment of the auto name (`services/db/manager` → `db`); see [app/services.md](../app/services.md).

**You never call `WrapContext` yourself in a service.** The supervisor wraps the `Config` it was constructed with (`supervisor.New(confutil.New(viper), ...)`) into the context it passes to every service's `Init` — that's the whole delivery mechanism. The wiring layer keeps the viper itself: it owns the file, the env prefix and the watch.

`FromContext` **never returns nil**: with no config in the context it returns an empty `Config`, so every getter yields the zero value. No nil check is needed, but it also means a missing config looks like "all defaults" rather than an error — if a setting is mandatory, validate it in `Init`.

Precedence is viper's: an env var the wiring layer's `AutomaticEnv` finds beats the YAML file, which beats a default; framingo binds no CLI flags of its own ([config.md](config.md#environment-overrides)).

For the full annotated YAML template (log, db, api, pprof, custom service keys) and dynamic-key notes, see [config.md](config.md).
