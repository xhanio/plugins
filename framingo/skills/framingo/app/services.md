# Services — Writing Your Own

Every business capability is a service package under `pkg/services/`. The
lifecycle interfaces it composes and the supervisor that runs it are
framework reference — [supervisor.md](../pkgs/supervisor.md); this file is
the authoring convention, and `example/pkg/services/example/` is its
reference implementation — every snippet below is lifted from it.

## The File Split

```
services/example/
├── model.go     # Manager (model.Example + lifecycle) and Repository (its repo.* contracts)
├── manager.go   # unexported struct, New()/newManager(), Name(), Dependencies()
├── option.go    # Option, applyOptions(), setDefaults(), WithLogger, With<Dynamic>…
├── business.go  # the business methods — model.Example's contract
├── lifecycle.go # Init/Start/Stop/Info
└── metric.go    # the instruments struct, built in Init (when the service records metrics)
```

Further files name a concern of their own — the user service keeps its
password policy in `password.go`, the certificate service its `describe.go`.

The example groups its services in tiers, mirroring the server component's
registration layers ([components-server.md](components-server.md)):
`services/repository/` (data access — one service, `repository.Repository`
in its `model.go`, implementing every `types/repo/` interface plus
`Transaction`, one file per domain and the `orm` → `entity` mapping in
`convert.go`; it holds `platform.DB()` and has no lifecycle or probes of
its own — see Health Probes below), `services/system/` (auth, user, role,
organization, certificate — and `platform`, the bundle below, a plain
struct the supervisor never sees), and business services such as
`services/example/` on top. System and business services take their own
`Repository` interface — declared in the service's `model.go`, composing
the `repo.*` contracts it reads and writes through — never the database
(`model.Database`, `db.Manager`): only the repository touches it. One
repository instance implements every contract, so the wiring hands it to
each service once, as is; what a service may touch is named in its
`model.go`.

**Stay in your layer.** A service reaches down only to the repo level —
the `repo.*` contracts its `Repository` names — and its currency is the
service level's own types: the `repo` contracts hand it entities — the
repository maps its `orm` rows before they leave it — so a service never
meets a GORM model, and `api` DTOs never come down either — routers unpack
those first. Peer services compose through `model.*` interfaces
(auth is built on `model.UserAuthN`), never by reaching into each other's
persistence.

**The service decides; the repository stores.** The rules about a
service's own data are the service's — hashing a password before
`CreateUser` (it hands the repository a `repo.UserCreateOptions` carrying
the hash), refusing to delete or demote the built-in admin, which
organization a new user joins. The repository counts, filters by the
options it is given, and stores what it is handed
([design/data-access.md](../design/data-access.md)). A service does not
ask who is calling either: authorization is the route's, and a check that
spans two services is the router's
([design/who-decides.md](../design/who-decides.md)) — so a test or a CLI
drives it with nothing but IDs.

And before writing these files, write the contracts —
`model.X`, the entities, the `repo`/`orm` pair if it persists: the order is
in [types.md](types.md).

## The Interface Goes in Two Places

Both halves get called "the service interface"; they are different files
with different consumers. Templates:
[`_templates/types-model-order.go`](../_templates/types-model-order.go) and
[`_templates/services-order-model.go`](../_templates/services-order-model.go).

| File | Declares | Who depends on it |
|---|---|---|
| `pkg/types/model/example.go` | `model.Example` — business methods + `common.Service`, **no lifecycle** | Routers and other services |
| `pkg/services/example/model.go` | `example.Manager` = `model.Example` + the lifecycle interfaces; `example.Repository` = the `repo.*` contracts the service reads and writes through | Only `pkg/components/server/` wiring |

The real pair:

```go
// pkg/types/model/example.go
type Example interface {
	common.Service
	HelloWorld(ctx context.Context, message string) (*entity.HelloWorld, error)
}
```

```go
// pkg/services/example/model.go
type Manager interface {
	model.Example
	common.Initializable
	common.Debuggable
	common.Daemon
}

// Repository is the slice of the shared repository this service may touch.
type Repository interface {
	repo.HelloWorld
}
```

A service that reads more names more: the user service's is `repo.User`
plus `repo.Organization`. Why the interface, not the instance, says what a
service may touch: [design/narrow-contracts.md](../design/narrow-contracts.md).

A router takes `model.Example`, never `example.Manager` — that keeps the
implementation package-private and stops services importing each other. If a
router imports `pkg/services/...`, the split has been skipped.

## `manager.go` — Struct, Constructors, Dependencies

`New` returns the interface; `newManager` returns the concrete type so package
tests can construct it and reach the unexported state. That seam is why test
files default to `package <service>` rather than `package <service>_test` —
see [testing.md](testing.md).


```go
type manager struct {
	log  log.Logger
	name string

	repository Repository
	mb         common.RawMessageSender
	meter      fmodel.Meter

	mu       sync.RWMutex // guards greeting: Init rewrites it, a restart again
	greeting string       // dynamic config

	loop syncutil.Loop      // takes new work: Stop waits for it, no deadline
	wg   syncutil.WaitGroup // work the loop hands off, from any Start: Stop waits until its ctx ends

	inst *instruments // built once at Init
}

func New(platform model.Platform, repo Repository, opts ...Option) Manager {
	return newManager(platform, repo, opts...)
}

// newManager returns the concrete manager, the form package tests construct.
func newManager(platform model.Platform, repo Repository, opts ...Option) *manager {
	m := &manager{
		repository: repo,
		mb:         platform.MessageBus(),
		meter:      platform.Meter(),
	}
	m.applyOptions(opts...)
	m.setDefaults()
	m.log = m.log.By(m)
	return m
}

func (m *manager) Name() string {
	return m.name
}
```

`nameutil.Name` derives the name from the package path relative to the
project root, with the layout root (`nameutil.Root`, default `pkg`)
stripped so the kind stays visible — this service is
`services/example/manager`, a router is `routers/example/router`. The
project prefix is judged from the gopro-injected `info.ProjectPath`;
binaries built without it (plain `go build`, `go test`) fall back to the
full import path.

The same name decides the config root: `nameutil.ConfigRoot(m.name)` is
its package segment (`example`, and still `example` from the unshortened
import path), or the whole name once a `WithName` option gives one — the
framework services carry it; add it to an app service that may run as a
second instance — see `lifecycle.go` below.

The name is fixed at construction, in exactly this position: inside
`setDefaults()`, which runs after `applyOptions(opts...)` and before
`log.By(m)`, which reads it. `Name()` is a plain getter — never compute
the name lazily inside it; a getter that writes state is a data race
waiting for its first concurrent caller.

**Every default lives in `setDefaults`, never in the struct literal.** A
literal seed is applied *before* the options, so it cannot tell "the caller
said nothing" from "the caller passed a zero" — `WithLogger(nil)` overwrites
it and panics at the first log call, and the same holds for any interface or
string field. `setDefaults` runs after, so a zero field is unambiguously
unset. It is also the one place to look for what a service falls back to.
The exception is a field whose zero value is *meaningful*: seed it in the
literal, before the options, because post-apply defaulting would undo a
deliberate zero — `wsutil` seeds its ping interval that way, since
`WithPing(0, ...)` disables pings.

```go
func (m *manager) Dependencies() []common.Service {
	return []common.Service{m.repository, m.mb}
}
```

Note the dependency shapes: the service asks for the **narrowest interface
that serves it** — `common.RawMessageSender`, not `messagebus.Manager` —
and lists it as is. A platform handle is never nil; a genuinely optional
dependency - auth's LDAP and API-token sources - is nil when the app wires
none, and the supervisor skips a nil entry, so `Dependencies()` filters
nothing out ([design/service-wiring.md](../design/service-wiring.md)).
Skipping covers ordering only: the service checks an optional dependency
where it calls it.

**The platform bundle.** The app bundles its framework plumbing behind
`model.Platform`, a `types/model` contract; a service takes it as is — no
per-consumer slice of it — and imports no service package to do so:
`services/system/platform` only builds the bundle, for the wiring and for
tests. The bundle rides
first in every app service's constructor — `New(platform, repo, ...)` for
the system and business services, `New(platform, um, lm, tm, ...)` for
auth, `New(platform, ...)` for the repository, which reads `DB()` from it —
whether the service uses it yet or not: one constructor shape across the
app, so a service that comes to need a handle changes neither its signature
nor the wiring ([design/service-wiring.md](../design/service-wiring.md)).
One with no use for it names the parameter `_` in `newManager`. Framework
services don't take it — they cannot know an app's bundle. Business
dependencies like the repository stay explicit arguments — the bundle
carries plumbing, not peers. The constructor plucks the handles it uses
into ordinary fields, so `Dependencies()` stays as precise as ever, and a
test builds a bundle with `platform.New(nil, meter, nil, mb)`, leaving nil
only what the code under test never reaches. Getters return `model.*` handles
(`fmodel.Meter`, `fmodel.MessageBus`), never concrete managers. The wiring
combines the bundle from services it actually constructed: every handle is
always there, so an app without pubsub drops the getter rather than passing
nil ([components-server.md](components-server.md)).

**Service metrics** (`metric.go`): instruments live in an `instruments` struct;
the constructor stores the `model.Meter` handle and `Init` builds them,
guarded by `if m.inst == nil` — the provider behind
`model.Meter` keeps one identity for the life of the process, so cached
instruments survive service restarts and never need a rebuild. The service
name is the instrumentation scope. The meter is always there, like every
platform handle: an app that wants no metrics wires the metrics service
disabled, and the instruments record into nothing. Creation goes in `Init`, not the
constructor, because it can fail — and its failures (an invalid instrument
name, a conflicting registration) are deterministic programmer errors, so
failing `Init` surfaces them at first boot in dev/CI. That is different
from *runtime* telemetry trouble (collector down, export errors), which
stays inside the metrics service and never touches business services. Do
not swallow the error into a noop instrument: the API's error-path return
value has no documented contract, and silent degradation hides a typo
until someone misses the metric in production.

## `option.go`

```go
type Option func(*manager)

func (m *manager) applyOptions(opts ...Option) {
	for _, opt := range opts {
		opt(m)
	}
}

// setDefaults fills every field the options left zero. It runs after
// applyOptions, so a zero field means the caller said nothing.
func (m *manager) setDefaults() {
	if m.log == nil {
		m.log = log.Default
	}
	if m.name == "" {
		m.name = nameutil.Name(m)
	}
}

// WithLogger stores the logger raw - newManager stamps it with By once the
// name resolves, so stamping here would bake in an empty caller field.
func WithLogger(logger log.Logger) Option {
	return func(m *manager) {
		m.log = logger
	}
}

func WithDynamicConfig(greeting string) Option {
	return func(m *manager) {
		m.greeting = greeting
	}
}
```

## `lifecycle.go`

`Init` re-reads dynamic config from the context `confutil.Config` — through the same
`applyOptions` + `setDefaults` pair the constructor uses, so a value has one
way in and one place that says what a zero falls back to. Keys are read
under `nameutil.ConfigRoot(m.name)`: the package segment of the auto name
(`services/example/manager` → `example` — a name with slashes cannot be a
root, no env override could spell it), or the whole name when `WithName`
gave one (a second database named `clickhouse` reads
`clickhouse.connection.*`). The name is the one identity; there is no
second field. `Start` launches the run loop:

```go
func (m *manager) Init(ctx context.Context) error {
	// dynamic config change, read under this service's root
	config := confutil.FromContext(ctx)
	root := nameutil.ConfigRoot(m.name)
	// locked: requests read the greeting while a restart reloads it
	m.mu.Lock()
	m.applyOptions(
		WithDynamicConfig(config.GetString("%s.greeting", root)),
	)
	m.setDefaults()
	m.mu.Unlock()
	if m.inst == nil {
		inst, err := m.newInstruments()
		if err != nil {
			return err
		}
		m.inst = inst
	}
	return nil
}

func (m *manager) Start(ctx context.Context) error {
	if !m.loop.Start(ctx, m.run) {
		m.log.Warnf("%s already started", m.Name())
	}
	return nil
}

func (m *manager) Stop(ctx context.Context) error {
	m.loop.Stop()
	return m.wg.Wait(ctx)
}

// run is the template's run loop: add tickers or queues as cases, and hand
// work that takes time to m.wg.Go, with ctx as an argument.
func (m *manager) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			// return at once: Stop waits for the loop with no deadline, so slow cleanup goes in m.wg.Go
			m.log.Infof("service %s stopped", m.Name())
			return
		}
	}
}
```

This is the [`Daemon` contract](../pkgs/supervisor.md#the-daemon-contract), its two waits
one type each. `m.loop.Stop()` returns once the run loop has, with no deadline, so the next
`Start` never runs beside it — which is why the loop returns as soon as its `ctx` ends and
leaves anything slow to `m.wg`. `m.wg.Wait(ctx)` then waits for that work, from every start,
until its `ctx` ends. Work a stop leaves runs on by itself, which is why it takes its start's
`ctx` and channels as arguments rather than reading them from the manager.

## Health Probes

Implement `common.Liveness`/`common.Readiness` when the service has
something real to report of its own — state it holds, a connection it owns.
A service that only fronts another needs neither: the repository probes
nothing, because the database it holds is a dependency, and its probes
already roll up into everything downstream. The framework's database
manager is the worked example (`pkg/services/db/health.go`, interface halves
declared in its `model.go`):

```go
// Alive: the service's own wiring ONLY. A liveness failure makes the
// supervisor restart this service - Init reconnects, so a missing handle is
// exactly what a restart fixes - but no restart of this client raises a
// database server: that is Ready's story.
func (m *manager) Alive(_ context.Context) error {
	if m.DB() == nil {
		return errors.Newf("database %s has no connection handle", m.Name())
	}
	return nil
}

// Ready: "can it serve right now" - derive from the caller's context,
// capping it, never fabricating a fresh one.
func (m *manager) Ready(ctx context.Context) error {
	sqlDB := m.DB()
	if sqlDB == nil {
		return errors.Newf("database %s has no connection handle", m.Name())
	}
	ctx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		return errors.Wrapf(err, "database ping failed")
	}
	return nil
}
```

The rule that decides what goes where: **`Alive` fails only if a restart
would fix it; a dependency outage fails `Ready`, never `Alive`.** The full
split table, monitor semantics, and the escalation ladder:
[supervisor.md](../pkgs/supervisor.md).

## `business.go`

The business method opens its own child span (handle-free — see
[tracing.md](../pkgs/tracing.md) "Spans in App Code"; every return path
must either `traceutil.Fail` or `span.End()`), reads auth state from the
context, delegates persistence to the repository — which hands back the
entity — fires an event, and counts the call:

```go
func (m *manager) HelloWorld(ctx context.Context, message string) (*entity.HelloWorld, error) {
	ctx, span := traceutil.Start(ctx, m.name, "HelloWorld")

	credential, ok := entity.CredentialFrom(ctx)
	if !ok {
		return nil, traceutil.Fail(span, errors.Unauthorized.Newf("credential not found in context"))
	}
	m.mu.RLock()
	greeting := m.greeting
	m.mu.RUnlock()
	if greeting == "" {
		greeting = "hello world!"
	}
	m.log.Infof("%s %s from %s", greeting, message, credential.UserName)

	result, err := m.repository.CreateHelloWorld(ctx, message)
	if err != nil {
		return nil, traceutil.Fail(span, errors.Wrap(err))
	}
	m.mb.SendRawMessage(ctx, m, "helloworld", result) // no return value — fire and forget
	m.inst.hello.Add(ctx, 1)
	span.End()
	return result, nil
}
```
