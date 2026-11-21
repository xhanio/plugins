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
type Reconcilable interface {                                          // what it registered with a dependency
    Reconcile(ctx context.Context, dep Service, state DependencyState) error
}
```

### The `Daemon` contract

`Start` and `Stop` are called on one instance, and more than once: the supervisor's restarts
run `Stop`, `Init` and `Start` on the same service.

- `Start` on a started service does nothing.
- What a start runs lives until `Stop`: `Start`'s `ctx` bounds the start alone and is never
  kept, its values aside. Whatever `Start` waits on, it waits on `ctx`: the supervisor's
  start deadline reaches it only that way. The start's own context comes from
  `context.WithCancel(context.WithoutCancel(ctx))`, as `syncutil.Loop` makes it, and only
  `Stop` cancels it. `Stop`'s `ctx` bounds the stop the same way.
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

Which work a stop may leave, which it ends hard, and which the next start waits for depends
on what the work holds: [design/lifecycle.md](../design/lifecycle.md).

The bound is the caller's, carried by the context:

- `WithStartPolicy(timeout)` gives each service's `Start`, at the first start and at each
  restart, a deadline. Past it, that `Start`'s context ends, and a `Start` that honors it
  fails like any other.
- `WithStopPolicy(timeout)` gives the supervisor's whole shutdown, and the stop of each
  restart, a deadline. Past it, each service still to stop gets an ended context.
- A signal handler that hears the signal again cancels the context of the stop going on,
  which forces it ([components-server.md](../app/components-server.md#signalgo--os-signal-handling)).
- An API that stops on request turns the grace period it was given into the context, with
  `context.WithTimeout`.

Keep on the manager what lasts across starts: the service's state, a `syncutil.Loop` for
what takes new work, whose `Stop()` returns once the loop has, and a `syncutil.WaitGroup`
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

**A name stands for one instance.** The dependency graph, the stats and the by-name operations all key on `Name()`, so `Register` refuses another instance under a name already taken — registered before, or earlier in the same call, or brought in as a dependency — with a `Conflict`; it is a wiring mistake, so fail startup on it. Meeting the same instance again (registered directly and as someone's dependency) is fine. Two instances of one service type need distinct names: `WithName` on the framework services. A nil service is skipped, and so is a nil entry in any `Dependencies()` list — registration, the init wait, the start gate, the health sweep and the reconciles all pass over it — so a service can list an optional dependency the app did not wire. `Register` is wiring-time (it is not locked against the monitor): call it, then `TopoSort`, before `Init`.

Full signature set:

```go
type Manager interface {          // = model.Supervisor + health + Initializable + Daemon + Debuggable
    Name() string
    Dependencies() []common.Service
    Register(services ...common.Service) error    // Conflict on a name another instance holds
    TopoSort() error
    Services() []common.Service
    Stats() ([]*entity.SupervisorStats, error)    // point-in-time copies, topological order: dependencies above dependents;
                                                  // the error combines each stat's Healthcheck() (stopped, init, start or reconcile error)

    Init(ctx context.Context) error
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Info(w io.Writer, debug bool)

    Alive(ctx context.Context) error              // health, on Manager not model.Supervisor: fails only when recovery is spent (a service dead with restarts exhausted)
    Ready(ctx context.Context) error              // health: the roll-up, nil iff every registered service is ready

    InitService(ctx context.Context, name string) error
    StartService(ctx context.Context, name string) error  // Conflict while its Init has not succeeded
    StopService(ctx context.Context, name string) error
    RestartService(ctx context.Context, name string) error
    Restart(ctx context.Context) error             // whole graph
}
```

The manager:
- Resolves dependencies via topological sort
- Calls `Init(ctx)` on `Initializable` services in dependency order — optionally waiting for dependencies, see [Waiting at Init](#waiting-at-init) below
- Calls `Start` on `Daemon` services in the same order, with the `ctx` given to its own `Start`, each bounded by `WithStartPolicy` — see [Starting](#starting) below
- Stops them in reverse topological order, dependents before their dependencies
- Monitors `Liveness` and `Readiness` probes — see [Health Probes & Escalation](#health-probes--escalation) below
- Monitoring and restarts tune via `WithMonitorPolicy(interval, maxRetries, restartDelay)` — sweep cadence (0, the default, disables monitoring), in-process restarts per service, pause before each restart; `WithStartPolicy(timeout)` bounds each service's `Start`, at the first start and at each restart (0, the default, leaves it to the context); `WithStopPolicy(timeout)` bounds the whole shutdown and the stop of each restart, forcing what is left once it passes (0, the default, leaves the bound to the context `Stop` gets); `WithName` and `WithLogger` as on every service

## Starting

A service is started once its own `Init` succeeded and every dependency it
lists is running — initialized, started, not stopped, and neither its start
nor a `Reconcile` since has failed
([design/lifecycle.md](../design/lifecycle.md#a-service-starts-once-what-it-depends-on-runs)):

- **A failed `Init`** is not started; its init error is the record, so the
  supervisor's `Start` does not report it again.
- **A dependency that failed to start** gates its dependents, with "dependency X
  is not running" as their start error, through a dependency with no `Start` of
  its own; `Start` returns those errors with the failed start's. A service that
  does not depend on it starts.
- **The monitor** restarts what was left, in dependency order, once the
  dependency runs. A restart, the monitor's or `RestartService`, leaves a service as
  it is while a dependency is not running and returns an `Unavailable`; the
  attempt is not counted against `maxRetries`.
- **A failed `Start` still starts the supervisor**: its monitor runs, and `Stop`
  stops what did start. Whether the process goes on is the caller's call. The
  example waits at `Init` at boot until every service inits, and exits on a
  failed `Start`; a SIGHUP reload, in a process already serving, inits in
  one pass and starts what it can after a failed `Init`, as the
  supervisor's own `Restart` does, the error logged and in `/readyz`.

## Reconciling with a dependency

A dependency's state lasts no longer than its run: the task manager forgets
the tasks added to it when it stops, the plan manager its plans, pubsub its
subscriptions. A dependent
that registers with a dependency implements `common.Reconcilable`, and puts
back what it holds when told
([design/service-wiring.md](../design/service-wiring.md#a-restart-is-invisible-to-the-dependents)):

| The supervisor | tells each dependent that runs on |
|---|---|
| restarts the dependency, once its stop is done | `DependencyStopped` |
| ... once its `Init` and `Start` succeeded | `DependencyRunning` |
| ... once its `Init` or `Start` failed | `DependencyFailed` |
| `StopService` | `DependencyStopped` |
| `StartService` | `DependencyRunning`, or `DependencyFailed` |

- **Direct dependents alone, running ones alone**, in dependency order,
  under the lock the restart holds, each call bounded by `WithStartPolicy`.
  A dependent that is not running registers everything when it starts.
- **Never at the whole graph's start, stop or restart**: the dependents stop
  and start with it.
- **A state, not a moment.** A stop is told once it is done; `Reconcile`
  relies on nothing but the state it is given, and a call that finds nothing
  to do changes nothing. A state it does not act on returns nil.
- **A failed `Reconcile` fails the dependent**: its `ReconcileErr`, in its
  stats and its health, keeps its own dependents from starting, and the
  monitor restarts it, whose `Init` and `Start` register everything. It is
  cleared when the dependent starts again.
- `Reconcile` registers and nothing slower, and never calls the supervisor,
  whose lock it runs under.

The plan manager and the messagebus implement it: on `DependencyRunning`
the plan manager adds every plan's task to the task manager again, and on
`DependencyStopped` ends canceled the firings the stop dropped; the
messagebus subscribes every module again.

## Waiting at Init

By default `Init` is one pass: each service's turn checks only that its
dependencies' `Init` succeeded, and fails otherwise — no probing, no
waiting. `WithInitPolicy` turns each turn of the first `Init` — the boot's —
into a retry loop for slow-starting infrastructure (a database still
bootstrapping when the process boots). A later `Init`, a SIGHUP reload's or
the supervisor's own `Restart`, makes one pass whatever the policy: the
process was serving and its dependencies ran a moment ago, so what fails is
the change — a config that does not hold, which no wait fixes — and the
pass returns, for what can start to start and the monitor's restarts to
take the rest:

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

For a dependency the app cannot run without, prefer `WithInitPolicy(-1)`
with a long `maxDelay`: during init there is no traffic to protect, giving
up after k tries only moves the retry to the platform as a crash loop, and
each blocked try is logged with its error, a wrong password included. The
example waits as long as it takes, `WithInitPolicy(-1, time.Second,
5*time.Minute)`, one try in five minutes at the slowest. The `Init` context
bounds the wait: cancellation cuts it short mid-delay.

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
- **A failure of the service's own triggers restart**: a failed liveness
  probe, or a failed `Init`, `Start` or `Reconcile` in its stats; readiness
  is reported, never restarted, and a dependency's failure only rolls up.
  The probe context is the monitor's own — cancellation at shutdown reaches
  a ping in flight.
- **`WithMonitorPolicy`'s maxRetries sets the escalation regime**:
  `n > 0` restarts up to n times, then the supervisor gives up — and, when
  it is liveness that failed, its own `Alive()` goes red (a service whose
  `Init` or `Start` keeps failing shows in `Ready` and `Stats`); `0` disables in-process restarts, so a liveness
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

// In Init(ctx), resolve the reloadable settings under the service's root, each
// key over its default, and publish them whole:
func (m *manager) Init(ctx context.Context) error {
    config := confutil.FromContext(ctx)
    s := m.defaults // fixed at New: the built-in defaults as the options set them
    s.setting = config.GetStringOr(s.setting, "%s.setting", nameutil.ConfigRoot(m.name))
    m.mu.Lock()
    m.settings = s
    m.mu.Unlock()
    return nil
}
```

A setting `Init` reads is reloadable: its option sets the default, the config
wins where it sets the key, and a key dropped at a reload falls back to the
default. A setting it does not read is construction, fixed at `New`
([design/config.md](../design/config.md),
[app/services.md](../app/services.md#optiongo)).

Every getter takes a **format string**: with no args it is the key itself (`GetString("my.setting")`), otherwise it is Sprintf-ed first. That is how a service reads under its own root instead of a hardcoded one — `nameutil.ConfigRoot(m.name)` is the whole name when `WithName` gave one, or the package segment of the auto name (`services/db/manager` → `db`); see [app/services.md](../app/services.md).

**You never call `WrapContext` yourself in a service.** The supervisor wraps the `Config` it was constructed with (`supervisor.New(confutil.New(viper), ...)`) into the context it passes to every service's `Init` — that's the whole delivery mechanism. The wiring layer keeps the viper itself: it owns the file, the env prefix, and when the file is re-read — the example on SIGHUP alone, with every service stopped ([design/config.md](../design/config.md)).

`FromContext` **never returns nil**: with no config in the context it returns an empty `Config`, so every plain getter yields the zero value and every `Or` getter its default. No nil check is needed, but it also means a missing config looks like "all defaults" rather than an error — if a setting is mandatory, validate it in `Init`.

Precedence is viper's: an env var the wiring layer's `AutomaticEnv` finds beats the YAML file, which beats a default; framingo binds no CLI flags of its own ([config.md](config.md#environment-overrides)).

For the full annotated YAML template (log, db, api, pprof, custom service keys) and dynamic-key notes, see [config.md](config.md).
