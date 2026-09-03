# Services — Writing Your Own

Every business capability is a service package under `pkg/services/`. The
lifecycle interfaces it composes and the supervisor that runs it are
framework reference — [supervisor.md](../pkgs/supervisor.md); this file is
the authoring convention, and `example/pkg/services/example/` is its
reference implementation — every snippet below is lifted from it.

## The File Split

```
services/example/
├── model.go     # Manager — the exported interface: model.Example + lifecycle
├── manager.go   # unexported struct, New()/newManager(), Name(), Dependencies()
├── option.go    # Option, applyOptions(), setDefaults(), WithLogger, With<Dynamic>…
├── business.go  # the business methods — model.Example's contract
└── lifecycle.go # Init/Start/Stop/Info
```

The example groups its services in tiers, mirroring the server component's
registration layers ([components-server.md](components-server.md)):
`services/repository/` (data access — one service implementing every
`types/repo/` interface plus `Transaction`, and the health probes:
`Ready()` pings the database, `Alive()` guards only its own wiring —
[supervisor.md](../pkgs/supervisor.md)), `services/system/` (auth, user,
role, organization, certificate), and business services such as
`services/example/` on top. System and business services take
`repository.Repository`, never `db.Manager` — only the repository touches
the database.

**Stay in your layer.** A service reaches down only to the repo level —
`repository.Repository` / `repo.*` interfaces — and its currency is the
service level's own types: it receives `orm` values at the repo boundary and
maps them to `entity` right there (`business.go` below does exactly this);
`orm` never travels further up, and `api` DTOs never come down — routers
unpack those first. Peer services compose through `model.*` interfaces
(auth is built on `model.UserAuthN`), never by reaching into each other's
persistence. And before writing the five files, write the contracts —
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
| `pkg/services/example/model.go` | `example.Manager` = `model.Example` + the lifecycle interfaces | Only `pkg/components/server/` wiring |

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
	// business.go
	model.Example
	// lifecycle.go
	common.Initializable
	common.Debuggable
	common.Daemon
}
```

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

	repository repository.Repository
	mb         common.RawMessageSender
	meter      fmodel.Meter

	greeting string // dynamic config

	ctx    context.Context
	cancel context.CancelFunc
	wg     *sync.WaitGroup

	metrics *metrics // cached instruments, built once at Init
}

func New(infra Infra, repo repository.Repository, opts ...Option) Manager {
	return newManager(infra, repo, opts...)
}

// newManager returns the concrete manager, the form package tests construct.
func newManager(infra Infra, repo repository.Repository, opts ...Option) *manager {
	m := &manager{
		repository: repo,
		mb:         infra.MessageBus(),
		meter:      infra.Meter(),
		wg:         &sync.WaitGroup{},
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
The exception is a field whose zero value is *meaningful*: messagebus seeds
its ping interval in the literal because `WithPing(0, ...)` deliberately
disables pings, and post-apply defaulting would undo that.

```go
func (m *manager) Dependencies() []common.Service {
	deps := []common.Service{m.repository}
	if s, ok := m.mb.(common.Service); ok {
		deps = append(deps, s)
	}
	return deps
}
```

Note the dependency shapes: the service asks for the **narrowest interface
that serves it** — `common.RawMessageSender`, not `messagebus.Manager` — and
`Dependencies()` type-asserts, because a narrow interface may or may not be
backed by a supervised service.

**The narrow Infra view.** The app bundles its framework plumbing in
`services/system/infra`, but a service never imports that package: it
declares its own `Infra` interface in `model.go`, listing only the getters
it consumes, and the app's bundle satisfies it structurally:

```go
// model.go
// Infra is this service's narrow view of the app's infra bundle.
type Infra interface {
	Meter() fmodel.Meter
	MessageBus() fmodel.MessageBus
}
```

The bundle rides first in every constructor (`New(infra, repo, ...)`);
business dependencies like the repository stay explicit arguments — the
bundle carries plumbing, not peers. This keeps the signature honest about
what the service actually uses, lets a test fake two methods instead of
assembling managers, and leaves `Dependencies()` as precise as ever, since
the constructor plucks the handles into ordinary fields. Getters return
`model.*` handles (`fmodel.Meter`, `fmodel.MessageBus`), never concrete
managers — and only ever *live* ones: the bundle is combined per app from
services the wiring layer actually constructed, so a handle the app lacks
is removed from the bundle and the narrow views, never carried as nil
([components-server.md](components-server.md)).

**Service metrics** (`metric.go`): instruments live in a `metrics` struct;
the constructor stores the `model.Meter` handle and `Init` builds the
instruments, guarded by `if m.metrics == nil` — the provider behind
`model.Meter` keeps one identity for the life of the process, so cached
instruments survive service restarts and never need a rebuild. The service
name is the instrumentation scope. Creation goes in `Init`, not the
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
	if m.ctx == nil {
		m.ctx = context.Background()
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

`Init` re-reads dynamic config from the context viper — through the same
`applyOptions` + `setDefaults` pair the constructor uses, so a value has one
way in and one place that says what a zero falls back to. `Start` launches
the run loop:

```go
func (m *manager) Init(ctx context.Context) error {
	// dynamic config change
	config := confutil.FromContext(ctx)
	m.applyOptions(
		WithDynamicConfig(config.GetString("example.greeting")),
	)
	m.setDefaults()
	if m.metrics == nil {
		metrics, err := m.newMetrics()
		if err != nil {
			return err
		}
		m.metrics = metrics
	}
	return nil
}

func (m *manager) Start(ctx context.Context) error {
	if m.cancel != nil {
		m.log.Warnf("%s already started", m.Name())
		return nil
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		// for/select with a single case on purpose: a ticker, a subscription
		// channel or a work queue can be added as another case without
		// restructuring anything.
		for {
			select {
			case <-ctx.Done():
				m.log.Infof("service %s stopped", m.Name())
				return
			}
		}
	}()
	return nil
}

func (m *manager) Stop(wait bool) error {
	if m.cancel == nil {
		return nil
	}
	m.cancel()
	if wait {
		m.wg.Wait()
	}
	m.cancel = nil
	return nil
}
```

## Health Probes

Implement `common.Liveness`/`common.Readiness` when the service has
something real to report — the repository is the worked example
(`services/repository/health.go`, interface halves declared in its
`model.go` under a `// health.go` comment):

```go
// Alive: the service's own wiring ONLY. A liveness failure makes the
// supervisor restart this service, and no repository restart fixes an
// unreachable database - that is Ready's story.
func (m *manager) Alive(_ context.Context) error {
	if m.db == nil || m.db.DB() == nil {
		return errors.Newf("repository has no database handle")
	}
	return nil
}

// Ready: "can it serve right now" - derive from the caller's context,
// capping it, never fabricating a fresh one.
func (m *manager) Ready(ctx context.Context) error {
	if m.db == nil || m.db.DB() == nil {
		return errors.Newf("repository has no database handle")
	}
	ctx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()
	if err := m.db.DB().PingContext(ctx); err != nil {
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
context, delegates persistence to the repository, maps ORM → entity, fires
an event, and counts the call:

```go
func (m *manager) HelloWorld(ctx context.Context, message string) (*entity.HelloWorld, error) {
	ctx, span := traceutil.Start(ctx, m.name, "HelloWorld")

	credential, ok := ctx.Value(api.ContextKeyCredential).(*entity.Credential)
	if !ok {
		return nil, traceutil.Fail(span, errors.Unauthorized.Newf("credential not found in context"))
	}
	greeting := m.greeting
	if greeting == "" {
		greeting = "hello world!"
	}
	m.log.Infof("%s %s from %s", greeting, message, credential.UserName)

	ormModel, err := m.repository.CreateHelloWorld(ctx, message)
	if err != nil {
		return nil, traceutil.Fail(span, errors.Wrap(err))
	}
	result := &entity.HelloWorld{
		ID:        ormModel.ID,
		Message:   ormModel.Message,
		CreatedAt: ormModel.CreatedAt,
		UpdatedAt: ormModel.UpdatedAt,
	}
	m.mb.SendRawMessage(ctx, m, "helloworld", result) // no return value — fire and forget
	m.metrics.hello.Add(ctx, 1)
	span.End()
	return result, nil
}
```
