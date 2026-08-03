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
├── service.go   # initServices() — creates every service instance, tier by tier
├── lifecycle.go # Init/Start/Stop/Info — registration order, pprof, shutdown control
├── api.go       # initAPI() — registers middlewares and routers with the API server
└── signal.go    # listenSignals() — SIGINT/SIGTERM/SIGHUP/SIGUSR1/SIGUSR2
```

The daemon binary constructs this component and runs it — see
[components-cmd.md](components-cmd.md).

## `model.go` — Server Interface

```go
type Server interface {
	common.Named
	common.Daemon        // Start(ctx) / Stop(wait)
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
	db         db.Manager
	pubsub     pubsub.Manager
	messagebus messagebus.Manager
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
	if m.name == "" {
		m.name = nameutil.Name(m) // components/server/example/manager
	}
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
supervisor, infra, system, business, api. Condensed to the real shape:

```go
func (m *manager) initServices() error {
	// init logger
	m.log = log.New(
		log.WithLevel(m.config.GetInt("log.level")),
		log.WithFileWriter(m.config.GetString("log.file") /* , log.rotation.* keys */),
	)
	infra.Debug = (m.log.Level() == zapcore.DebugLevel)

	// init service manager
	m.services = supervisor.New(m.config, supervisor.WithLogger(m.log))

	/* init infra level services */

	m.db = db.New(
		db.WithType(m.config.GetString("db.type")),
		db.WithDataSource(db.Source{
			// every field falls back: config key → env-style key → default
			Host: sliceutil.First(
				m.config.GetString("db.source.host"),
				m.config.GetString("DB_HOST"),
				"127.0.0.1",
			),
			Port: sliceutil.First(m.config.GetUint("db.source.port"), m.config.GetUint("DB_PORT"), 5432),
			/* User, Password, DBName likewise */
		}),
		db.WithMigration(m.config.GetString("db.migration.dir"), m.config.GetUint("db.migration.version")),
		db.WithConnection( /* the five db.connection.* keys */ ),
		db.WithLogger(m.log),
	)

	m.pubsub = pubsub.New(driver.NewMemory(m.log), pubsub.WithLogger(m.log))
	m.messagebus = messagebus.New(m.pubsub, messagebus.WithLogger(m.log))
	m.repository = repository.New(m.db, repository.WithLogger(m.log))

	/* init system level services */

	m.user = user.New(m.repository, user.WithLogger(m.log))
	m.role = role.New(m.repository, role.WithLogger(m.log))
	m.organization = organization.New(m.repository, organization.WithLogger(m.log))
	m.certificate = certificate.New(m.repository, certificate.WithLogger(m.log))
	m.auth = auth.New(
		m.user,
		nil, // LDAPAuthN is optional
		nil, // APITokenAuthN is optional
		auth.WithLogger(m.log),
	)

	/* init business level components */

	m.example = example.New(m.repository, m.messagebus, example.WithLogger(m.log))

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
			// cors must see every request - a preflight OPTIONS matches no
			// route - so it rides the server-level slot. The
			// api.<name>.middlewares mapping activates it under "cors" and
			// carries its policy; with no entry it stays dormant, so the
			// health listener serves without it.
			server.WithMiddlewares(corsmw.New()),
		}
		// Per-server middleware configs: a plain mapping of middleware name
		// to its default config, re-marshaled to raw YAML for the server.
		if mws := m.config.GetStringMap(fmt.Sprintf("api.%s.middlewares", name)); len(mws) > 0 {
			raw, err := yaml.Marshal(mws)
			if err != nil {
				return errors.Wrap(err)
			}
			opts = append(opts, server.WithMiddlewareConfigs(raw))
		}
		// add TLS if configured
		if m.config.IsSet(fmt.Sprintf("api.%s.cert", name)) {
			opts = append(opts, server.WithTLS(certutil.MustCAFromFile( /* ca.cert, api.<name>.cert/key */ ), true))
		}
		if err := m.api.Add(name, opts...); err != nil {
			return errors.Wrap(err)
		}
	}
	return nil
}
```

Two details that matter:

- **Blank-import the db drivers in this file.** The real one pulls in all
  four `pkg/services/db/drivers/*` packages with `_` imports; a binary that
  skips one fails at Init with "driver not registered".
- **The repository service sits in the infra tier.** System and business
  services take `repository.Repository`, never `db.Manager` — only the
  repository touches the database.

## `lifecycle.go` — Registration Order in `Init()`

```go
func (m *manager) Init(ctx context.Context) error {
	if err := m.initConfig(); err != nil {
		return errors.Wrap(err)
	}
	if err := m.initServices(); err != nil {
		return errors.Wrap(err)
	}

	// register basic services
	m.services.Register(m.db)
	// register system services
	m.services.Register(m.pubsub, m.messagebus, m.repository,
		m.user, m.role, m.organization, m.certificate, m.auth)
	// register business services
	m.services.Register(m.example)

	// perform a topo sort to ensure the dependencies
	if err := m.services.TopoSort(); err != nil {
		return errors.Wrap(err)
	}
	// append api after topo sort to ensure the latest start
	m.services.Register(m.api)

	// register all services with messagebus; modules without MessageHandler /
	// RawMessageHandler are skipped automatically.
	for _, svc := range m.services.Services() {
		m.messagebus.Register(svc)
	}

	// init all services — a failure is logged, not fatal: from here the
	// supervisor's liveness monitoring and restart policy own unhealthy
	// services.
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

`Stop(wait)` stops the graph and cancels the context; `Info` delegates to
`m.services.Info`.

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
		userRouter.New(m.user, m.role, m.auth, m.log),
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
| SIGINT, SIGTERM | `m.Stop(true)` — graceful shutdown |
| SIGHUP | `m.services.Restart(ctx)` — restart the whole service graph |
| SIGUSR1 | `m.Info(os.Stdout, true)` — dump service info |
| SIGUSR2 | dump a full goroutine stack trace |

```go
func (m *manager) listenSignals(ctx context.Context) {
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGUSR1, syscall.SIGUSR2)
	defer signal.Stop(signalCh)
	for {
		select {
		case <-ctx.Done():
			m.log.Info("gracefully shutdown manager")
			return
		case sig := <-signalCh:
			switch sig { /* the table above */ }
		}
	}
}
```
