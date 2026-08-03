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
├── option.go    # Option, apply(), WithLogger, With<Dynamic>…
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

```go
type manager struct {
	log  log.Logger
	name string

	repository repository.Repository
	mb         common.RawMessageSender

	greeting string // dynamic config

	ctx    context.Context
	cancel context.CancelFunc
	wg     *sync.WaitGroup
}

func New(repo repository.Repository, mb common.RawMessageSender, opts ...Option) Manager {
	return newManager(repo, mb, opts...)
}

// newManager returns the concrete manager, the form package tests construct.
func newManager(repo repository.Repository, mb common.RawMessageSender, opts ...Option) *manager {
	m := &manager{
		log:        log.Default,
		repository: repo,
		mb:         mb,
		wg:         &sync.WaitGroup{},
	}
	m.apply(opts...)
	if m.name == "" {
		m.name = nameutil.Name(m)
	}
	m.log = m.log.By(m)
	if m.ctx == nil {
		m.ctx = context.Background()
	}
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

## `option.go`

```go
type Option func(*manager)

func (m *manager) apply(opts ...Option) {
	for _, opt := range opts {
		opt(m)
	}
}

func WithLogger(logger log.Logger) Option {
	return func(m *manager) {
		m.log = logger.By(m)
	}
}

func WithDynamicConfig(greeting string) Option {
	return func(m *manager) {
		m.greeting = greeting
	}
}
```

## `lifecycle.go`

`Init` re-reads dynamic config from the context viper — and routes it
through `apply`, so options remain the one mutation path. `Start` launches
the run loop:

```go
func (m *manager) Init(ctx context.Context) error {
	// dynamic config change
	config := confutil.FromContext(ctx)
	m.apply(
		WithDynamicConfig(config.GetString("example.greeting")),
	)
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

The business method reads auth state from the context, delegates
persistence to the repository, maps ORM → entity, and fires an event:

```go
func (m *manager) HelloWorld(ctx context.Context, message string) (*entity.HelloWorld, error) {
	credential, ok := ctx.Value(api.ContextKeyCredential).(*entity.Credential)
	if !ok {
		return nil, errors.Unauthorized.Newf("credential not found in context")
	}
	greeting := m.greeting
	if greeting == "" {
		greeting = "hello world!"
	}
	m.log.Infof("%s %s from %s", greeting, message, credential.UserName)

	ormModel, err := m.repository.CreateHelloWorld(ctx, message)
	if err != nil {
		return nil, errors.Wrap(err)
	}
	result := &entity.HelloWorld{
		ID:        ormModel.ID,
		Message:   ormModel.Message,
		CreatedAt: ormModel.CreatedAt,
		UpdatedAt: ormModel.UpdatedAt,
	}
	m.mb.SendRawMessage(ctx, m, "helloworld", result) // no return value — fire and forget
	return result, nil
}
```
