# Server Component — The Application Daemon

`pkg/components/server/<app>/` builds the service graph, hands it to the
supervisor, registers routers and middlewares, and owns signals. The
reference implementation is
[`example/pkg/components/server/example/`](https://github.com/xhanio/framingo/tree/main/example/pkg/components/server/example),
and every snippet below is lifted from it. All server implementations MUST
follow this file structure — one file per responsibility:

```
components/server/example/
├── model.go     # Server interface (Named + Daemon + Initializable + Debuggable)
├── manager.go   # the manager struct and New() — fields and construction only
├── config.go    # newConfig (viper + env prefix) and initConfig (read + watch)
├── service.go   # newLogger/newDatabase + initServices() — every service instance, tier by tier
├── lifecycle.go # Init/Start/Stop/Info — registration order, pprof, shutdown control
├── api.go       # initAPI() — registers middlewares and routers with the API server
├── signal.go    # listenSignals() — SIGINT/SIGTERM/SIGHUP/SIGUSR1/SIGUSR2
└── migrate.go   # Migrate() — the one-off schema run behind `exampleapp migrate`
```

The daemon binary constructs this component and runs it, and its `migrate`
subcommand calls `Migrate` — see [components-cmd.md](components-cmd.md).

## `model.go` — Server Interface

```go
type Server interface {
	common.Named
	common.Daemon        // Start(ctx) / Stop(ctx)
	common.Initializable // Init(ctx)
	common.Debuggable    // Info(w, debug)
}
```

## `manager.go` — Struct and Construction

Fields group by tier — the same tiers `service.go` creates and `lifecycle.go`
registers. Lifecycle methods never live here.

```go
type manager struct {
	name   string
	config *viper.Viper
	// util services
	log log.Logger

	// infra services
	tracing    tracing.Manager
	db         db.Manager
	metrics    metrics.Manager
	pubsub     pubsub.Manager
	messagebus messagebus.Manager
	// platform bundle for business constructors
	platform platform.Platform
	// data access
	repository repository.Repository

	// system services
	user         user.Manager
	role         role.Manager
	organization organization.Manager
	certificate  certificate.Manager
	auth         auth.Manager

	// business services
	example example.Manager

	// api related services
	api server.Manager

	// service controller
	services supervisor.Manager

	// shutdown control
	ctx    context.Context
	cancel context.CancelFunc
}

func New(configPath string) Server {
	m := &manager{
		config: newConfig(configPath),
	}
	m.name = nameutil.Name(m) // components/server/example/manager
	return m
}
```

## `config.go` — Viper and Env

`newConfig` builds the viper instance at construction; `initConfig` reads it
during Init. The env prefix derives from the build-injected product name, and
process-wide facts land in the project's `pkg/utils/infra`:

```go
func newConfig(configPath string) *viper.Viper {
	conf := viper.New()
	conf.SetConfigFile(configPath)
	infra.EnvPrefix = envutil.EnvPrefix(info.ProductName)
	conf.SetEnvPrefix(infra.EnvPrefix)
	// db.source.host is <PREFIX>_DB_SOURCE_HOST; without the replacer viper
	// looks for <PREFIX>_DB.SOURCE.HOST, which nothing can set
	conf.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	conf.AutomaticEnv()
	return conf
}

func (m *manager) initConfig() error {
	infra.StartTime = time.Now()
	configFile := m.config.ConfigFileUsed()
	if err := m.config.ReadInConfig(); err != nil {
		return errors.Wrapf(err, "failed to read config file %s", configFile)
	}
	m.config.WatchConfig()
	absPath, err := filepath.Abs(configFile)
	if err != nil {
		return errors.Wrapf(err, "failed to resolve config path %s", configFile)
	}
	infra.ConfigDir = filepath.Dir(absPath)
	return nil
}
```

## `service.go` — Service Creation, Tier by Tier

`initServices` creates every instance in dependency order: logger,
supervisor, infra, system, business, api. The logger and the database are
built by two helpers, so [`Migrate`](#migratego--the-one-off-schema-run)
constructs them exactly as the daemon does. Condensed to the real shape:

```go
func (m *manager) newLogger() log.Logger {
	return log.New(
		log.WithLevel(m.config.GetInt("log.level")),
		log.WithFileWriter(m.config.GetString("log.file") /* , the three log.rotation.* keys */),
	)
}

func (m *manager) newDatabase(opts ...db.Option) db.Manager {
	base := []db.Option{
		db.WithType(m.config.GetString("db.type")),
		// each key is overridable as <PREFIX>_DB_SOURCE_HOST and so on (the
		// key replacer in config.go), and viper ranks the environment above
		// the file; the fallbacks apply only when neither sets a value
		db.WithDataSource(db.Source{
			Host:     sliceutil.First(m.config.GetString("db.source.host"), "127.0.0.1"),
			Port:     sliceutil.First(m.config.GetUint("db.source.port"), 5432),
			User:     m.config.GetString("db.source.user"),
			Password: m.config.GetString("db.source.password"),
			DBName:   m.config.GetString("db.source.dbname"),
		}),
		// empty dir: the daemon never touches the schema (the migrate Job does)
		db.WithMigration(m.config.GetString("db.migration.dir"), m.config.GetUint("db.migration.version")),
		db.WithConnection( /* the five db.connection.* keys */ ),
		db.WithLogger(m.log),
	}
	return db.New(append(base, opts...)...)
}

func (m *manager) initServices() error {
	m.log = m.newLogger()
	infra.Debug = (m.log.Level() == zapcore.DebugLevel)

	// services see the viper only as the read-side Config the supervisor carries into Init
	m.services = supervisor.New(confutil.New(m.config),
		supervisor.WithLogger(m.log),
		// within the platform's stop timeout, so shutdown ends here, not in a SIGKILL
		supervisor.WithStopPolicy(20*time.Second),
		// probes feed /healthz and /readyz: without a sweep they would report
		// only what Init and Start left behind
		supervisor.WithMonitorPolicy(
			m.config.GetDuration("supervisor.monitor.interval"),
			m.config.GetInt("supervisor.monitor.max_retries"),
			m.config.GetDuration("supervisor.monitor.restart_delay"),
		),
	)

	/* init infra level services */

	// Init re-reads these keys, so a restart picks up changes
	m.tracing = tracing.New(
		tracing.WithEnabled(m.config.GetBool("tracing.enabled")),
		tracing.WithExporter(m.config.GetString("tracing.exporter"), m.config.GetStringMap("tracing.config")),
		tracing.WithSampleRatio(m.config.GetFloat64("tracing.sample_ratio")),
		tracing.WithLogger(m.log),
	)
	// likewise metrics.*; the meter's provider is stable for the life of the
	// process, so services create their instruments once, at their first Init
	m.metrics = metrics.New(
		metrics.WithEnabled(m.config.GetBool("metrics.enabled")),
		metrics.WithExporter(m.config.GetString("metrics.exporter"), m.config.GetStringMap("metrics.config")),
		metrics.WithInterval(m.config.GetDuration("metrics.interval")),
		metrics.WithLogger(m.log),
	)

	// query spans through the tracing service's own handle; bound values are
	// left out of db.query.text - they carry credentials and PII
	dbTracing, err := otelgorm.New(m.tracing, gormtracing.WithoutQueryVariables())
	if err != nil {
		return errors.Wrap(err)
	}
	// a second database is newDatabase with db.WithName("clickhouse") and
	// its keys read under clickhouse.* - the name is the config root
	m.db = m.newDatabase(db.WithPlugins(dbTracing))

	m.pubsub = pubsub.New(driver.NewMemory(m.log), pubsub.WithLogger(m.log))
	m.messagebus = messagebus.New(m.pubsub, messagebus.WithLogger(m.log))

	// combine the services business constructors consume
	m.platform = platform.New(m.db, m.metrics, m.pubsub, m.messagebus)

	// one instance implements every repo contract; each service sees it
	// through its own Repository interface, declared in its model.go
	m.repository = repository.New(m.platform, repository.WithLogger(m.log))

	/* init system level services */

	// user publishes the account changes auth ends sessions on
	m.user = user.New(m.platform, m.repository, user.WithLogger(m.log))
	m.role = role.New(m.platform, m.repository, role.WithLogger(m.log))
	m.organization = organization.New(m.platform, m.repository, organization.WithLogger(m.log))
	m.certificate = certificate.New(m.platform, m.repository, certificate.WithLogger(m.log))
	m.auth = auth.New(
		m.platform,
		m.user,
		nil, // LDAPAuthN is optional
		nil, // APITokenAuthN is optional
		auth.WithLogger(m.log),
	)

	/* init business level components */

	m.example = example.New(m.platform, m.repository, example.WithLogger(m.log))

	/* init api level components */

	m.api = server.New(server.WithLogger(m.log))
	servers := m.config.GetStringMap("api")
	for name := range servers {
		opts := []server.ServerOption{
			server.WithEndpoint(
				m.config.GetString(fmt.Sprintf("api.%s.host", name)),
				m.config.GetUint(fmt.Sprintf("api.%s.port", name)),
				m.config.GetString(fmt.Sprintf("api.%s.prefix", name)),
			),
			// server-level so they see every request (a preflight OPTIONS
			// matches no route), trace first so cors runs inside the span;
			// each stays dormant unless the api.<name>.middlewares mapping
			// enables it by name - the health listener has no entry, so its
			// probe polls go untraced
			server.WithMiddlewares(tracemw.New(m.tracing), corsmw.New()),
			// every listener records HTTP metrics through the metrics service;
			// series are keyed by listener, so health probes never distort
			// the public one's
			server.WithMeter(m.metrics),
		}
		// per-server middleware defaults: a plain mapping of middleware name
		// to its default config, re-marshaled to raw YAML for the server -
		// unordered; order lives in router.yaml
		if mws := m.config.GetStringMap(fmt.Sprintf("api.%s.middlewares", name)); len(mws) > 0 {
			raw, err := yaml.Marshal(mws)
			if err != nil {
				return errors.Wrap(err)
			}
			opts = append(opts, server.WithMiddlewareConfigs(raw))
		}
		// add TLS if configured
		if m.config.IsSet(fmt.Sprintf("api.%s.cert", name)) {
			opts = append(opts, server.WithTLS(certutil.MustCertFromFile( /* api.<name>.cert, "", api.<name>.key */ )))
		}
		if err := m.api.Add(name, opts...); err != nil {
			return errors.Wrap(err)
		}
	}
	return nil
}
```

Four details that matter:

- **Blank-import the db drivers and telemetry exporters in this file.** The
  real one pulls in all four `pkg/services/db/drivers/*` packages plus the
  three `pkg/services/tracing/exporters/*` and three
  `pkg/services/metrics/exporters/*` packages (`otlpgrpc`, `otlphttp`,
  `stdout`) with `_` imports; a binary that skips one fails at Init with
  "not registered".
- **The repository service sits in the infra tier.** System and business
  services take their own `Repository` interface — the `repo.*` contracts
  they use, declared in their `model.go` — never `db.Manager`: only the
  repository touches the database.
- **The platform bundle (`services/system/platform`) is a combiner, not a
  builder.** It holds the `model.*` handles business constructors consume —
  `DB() model.Database`, `Meter() model.Meter`, `PubSub() model.Pubsub`,
  `MessageBus() model.MessageBus` — so a new cross-cutting handle later
  means one new getter, not a parameter added to every constructor. The
  wiring layer still constructs every service, holds each as its own field,
  and registers them individually. Tracing stays out of the bundle: no
  business service touches the tracer — spans arrive through the trace
  middleware and propagate via context. The repository stays out too: it is
  a business dependency, passed explicitly. Consumers import the bundle and
  take `platform.Platform` as is — see [services.md](services.md).
- **Combine the bundle to fit the app — never wire a nil into it.** The
  bundle lives in the app tree precisely so its shape tracks what *this*
  app constructs: an app without pubsub drops the `PubSub()` getter; it
  does not pass nil to
  `platform.New`. A nil handle costs nothing at wiring time and detonates
  later, inside whichever service first calls through it, as an interface
  panic far from the cause — and nothing downstream can guard it: a
  nil-receiver check on a getter never sees a nil *interface*, and
  substituting a noop would silently swallow a wiring bug the way the
  fail-fast Init doctrine forbids. Every argument to `platform.New` is a
  service constructed a few lines above in `initServices`, so the
  invariant stays checkable by eye. There is no exception: the platform is
  infrastructure, and every handle — db, meter, pubsub, messagebus — is
  always there, so consumers call through them unchecked. An app that
  wants no metrics still constructs the metrics service, disabled
  (`metrics.enabled: false`): its meter then records into nothing.

## `lifecycle.go` — Registration Order in `Init()`

```go
func (m *manager) Init(ctx context.Context) error {
	if err := m.initConfig(); err != nil {
		return errors.Wrap(err)
	}
	if err := m.initServices(); err != nil {
		return errors.Wrap(err)
	}

	// register system services; a name another instance holds fails here
	if err := m.services.Register(m.repository,
		m.user, m.role, m.organization, m.certificate, m.auth); err != nil {
		return errors.Wrap(err)
	}
	// register business services
	if err := m.services.Register(m.example); err != nil {
		return errors.Wrap(err)
	}
	// register infra last, telemetry at the end (see below)
	if err := m.services.Register(m.db, m.pubsub, m.messagebus, m.metrics, m.tracing); err != nil {
		return errors.Wrap(err)
	}

	// perform a topo sort to ensure the dependencies
	if err := m.services.TopoSort(); err != nil {
		return errors.Wrap(err)
	}
	// append api after topo sort to ensure the latest start - an ordering,
	// not a dependency: a failed dependency would keep the api's Init from
	// running, and the health listener must come up regardless
	if err := m.services.Register(m.api); err != nil {
		return errors.Wrap(err)
	}

	// register all services with messagebus; modules without MessageHandler /
	// RawMessageHandler are skipped automatically.
	for _, svc := range m.services.Services() {
		m.messagebus.Register(svc)
	}

	// init all services — a failure is logged, not fatal, so the listeners
	// still come up and /readyz reports what failed; from then on the
	// monitor (supervisor.monitor.*) keeps the probes current and restarts
	// a service whose liveness fails
	if err := m.services.Init(ctx); err != nil {
		m.log.Error(err)
	}

	// wire routes after services are initialized
	if err := m.initAPI(); err != nil {
		return errors.Wrap(err)
	}
	return nil
}
```

**Telemetry registers last — the order is load-bearing.** `TopoSort` pops
dependency-less services in *reverse* registration order, so the
last-registered ones sort **first**. Registering telemetry (metrics, then
tracing) at the very end therefore puts it at the head of the sorted list:
it initializes before everything that emits telemetry, and — with stop
running in reverse — flushes last, after the draining api server's final
requests and every other service's `Stop`. Register telemetry first and you
get the opposite: it flushes *before* business services stop, silently
dropping anything emitted on a Stop path. Metrics before tracing keeps
tracing the very last to flush. Services with declared dependencies
(repository, business services) are ordered by the graph itself, so only
the dependency-less tail needs this care.

`Start` enables pprof when `pprof.port` is set, starts the graph, then holds
the shutdown context and blocks in the signal loop:

```go
func (m *manager) Start(ctx context.Context) error {
	if m.cancel != nil {
		m.log.Warn("manager already started, skipping")
		return nil
	}
	pport := m.config.GetUint("pprof.port")
	if pport != 0 {
		go func() { /* http.ListenAndServe on localhost:pport — net/http/pprof is blank-imported in manager.go */ }()
	}
	if err := m.services.Start(ctx); err != nil {
		return err
	}
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.listenSignals(m.ctx)
	return nil
}
```

`Stop(ctx)` stops the graph, within `ctx` and the supervisor's stop policy, and cancels the
context; `Info` delegates to `m.services.Info`. Give the supervisor a `WithStopPolicy` shorter
than the platform's own stop timeout, so the shutdown ends there rather than in a SIGKILL —
the example's 20s sits inside Kubernetes' default 30s termination grace period.

## `api.go` — Middleware and Router Registration

The complete real wiring — middlewares before routers, because router.yaml
references them by name ([middlewares.md](middlewares.md)):

```go
func (m *manager) initAPI() error {
	middlewares := []api.Middleware{
		deflate.New(),
		authnuser.New(m.auth),
		authz.New(m.role),
		// Routers opt in through router.yaml, where a handler may also carry
		// its own limit under the middleware's name; the server's middleware
		// defaults (api.<name>.middlewares in config.yaml) cover the rest.
		throttle.New(),
	}
	routers := []api.Router{
		exampleRouter.New(m.example, m.log),
		healthRouter.New(m.services, m.log), // /healthz + /readyz on the health listener
		authRouter.New(m.auth, m.role, m.log),
		userRouter.New(m.user, m.role, m.log),
		roleRouter.New(m.role, m.log),
		certRouter.New(m.certificate, m.log),
		messagebusRouter.New(m.messagebus, m.log),
	}
	if err := m.api.RegisterMiddlewares(middlewares...); err != nil {
		return errors.Wrap(err)
	}
	if err := m.api.RegisterRouters(routers...); err != nil {
		return errors.Wrap(err)
	}
	return nil
}
```

## `signal.go` — OS Signal Handling

The supervisor deliberately installs no signal handlers
([supervisor.md](../pkgs/supervisor.md)); this file owns them:

| Signal | Action |
|---|---|
| SIGINT, SIGTERM | first: `m.Stop(stopCtx)` in a goroutine — graceful shutdown; second: cancel `stopCtx`, which forces it; third: `os.Exit(1)` |
| SIGHUP | `m.services.Restart(ctx)` — restart the whole service graph; ignored while shutting down |
| SIGUSR1 | `m.Info(os.Stdout, true)` — dump service info |
| SIGUSR2 | dump a full goroutine stack trace, a stuck shutdown's included |

The stop runs beside the loop, so the loop keeps hearing signals while it goes on: a stop
called in the loop itself would leave the next SIGTERM to `signal.Notify`, which swallows it.

```go
func (m *manager) listenSignals(ctx context.Context) {
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGUSR1, syscall.SIGUSR2)
	defer signal.Stop(signalCh)
	done := ctx.Done()
	stopCtx, force := context.WithCancel(context.Background())
	defer force()
	var stopped chan error
	var forced bool
	for {
		select {
		case <-done:
			m.log.Info("gracefully shutdown manager")
			return
		case err := <-stopped:
			// log err, then
			return
		case sig := <-signalCh:
			switch sig {
			case syscall.SIGINT, syscall.SIGTERM:
				switch {
				case stopped == nil:
					stopped = make(chan error, 1)
					done = nil // the stop ends ctx, and stopped tells how it went
					go func() { stopped <- m.Stop(stopCtx) }()
				case !forced:
					force()
					forced = true
				default:
					os.Exit(1)
				}
			/* the rest of the table above */
			}
		}
	}
}
```

## `migrate.go` — The One-Off Schema Run

`Migrate` is a package function, not a `Server` method: it builds a bare
manager over the same config, reuses `newLogger` and `newDatabase`, and
initializes only the database — which migrates in `Init` — then returns.
No supervisor, no other service, no listener, so the prod migrate Job can
run it once per release while the replicas, whose config leaves
`db.migration` out, never touch the schema:

```go
// Migrate is the one-off schema run (the prod Job) so replicas never race over
// it; db.migration.version 0 means latest.
func Migrate(ctx context.Context, configPath string) error {
	m := &manager{config: newConfig(configPath)}
	if err := m.initConfig(); err != nil {
		return errors.Wrap(err)
	}
	if m.config.GetString("db.migration.dir") == "" {
		return errors.InvalidArgument.Newf("db.migration.dir is not set: no migrations to apply")
	}
	m.log = m.newLogger()
	database := m.newDatabase()
	// db reads its settings from the config in ctx, as under the supervisor
	ctx = confutil.WrapContext(ctx, confutil.New(m.config))
	if err := database.Init(ctx); err != nil {
		return errors.Wrapf(err, "failed to migrate the database")
	}
	m.log.Info("database migrated")
	return nil
}
```

The Job supplies the directory through the environment
(`<PREFIX>_DB_MIGRATION_DIR=/app/migrations`, where the image carries them)
— see [design/migrations.md](../design/migrations.md).
