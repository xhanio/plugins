# Testing — Test Package Choice and File Naming

## Default: test from inside the package

Framingo services expose two constructors on purpose:

```go
func New(opts ...Option) Manager { return newManager(opts...) }

// newManager returns the concrete manager, the form package tests construct.
func newManager(opts ...Option) *manager { ... }
```

`newManager` exists **so that tests can hold the concrete type**. A test file therefore declares the package itself, not an external test package:

```go
package db          // ← not db_test

func TestAliveChecksOwnWiringOnly(t *testing.T) {
	m := newManager()
	...
}
```

One test file then sees both halves — the exported contract callers depend on, and the unexported state that decides whether the contract holds. Splitting those across two packages buys nothing here and costs a round of exported-shim writing whenever a probe needs to look at a private field.

This runs against the common Go advice of "external test package by default." That advice is aimed at tests that reach in through a crack and end up coupled to accidental details. In framingo the concrete constructor is a designed, documented seam that every service publishes; using it is walking through the front door.

The same pattern holds for routers (`newRouter`) and any other `newXxx` constructor — see [services.md](services.md). An app service's pair takes the same arguments as its `New` — `newManager(platform, repo, opts...)` — so a test passes `platform.New(...)` with nil for every handle the code under test never calls.

## Exception: registry packages force an external test package

A package whose subpackages import it back to self-register cannot be tested from the inside once the test needs one of those subpackages.

`pkg/services/db` is the case. Every driver calls `db.Register` in `init()`, so `db/drivers/sqlite` imports `db`. A test needs a driver blank-imported to reach a real database:

```go
_ "github.com/xhanio/framingo/pkg/services/db/drivers/sqlite"
```

Put that in a `package db` test file and the build fails:

```
package github.com/xhanio/framingo/pkg/services/db
	imports .../db/drivers/sqlite from db_test.go
	imports .../db from sqlite.go: import cycle not allowed in test
```

That is a compile error, not a style preference. The fix is `package db_test`, which is a separate package and breaks the cycle. Go allows `db` and `db_test` files side by side in one directory, so a package can use both — framingo's `db` does: five files external, `health_test.go` internal because the probes need unexported fields.

The same constraint applies to:

- `pkg/services/tracing` and `pkg/services/metrics` — their `exporters/*` call `tracing.Register` and `metrics.Register`
- **any helper package that imports the package under test** — `pkg/utils/testutil` imports `db`, so only an external test package can use `testutil.SetupDB` inside `db`'s own tests

Apply the same reasoning to an application's own registries.

## Deciding

| Situation | Package |
|---|---|
| Normal case — service, router, middleware, util | `package foo` |
| Test blank-imports a subpackage that registers back into `foo` | `package foo_test` (forced) |
| Test needs a helper package that imports `foo` | `package foo_test` (forced) |

Reach for `foo_test` only when the compiler makes you. Nothing else in framingo should be external.

## Naming the file

Mirror the source file: `health.go` → `health_test.go`, `migrate.go` → `migrate_test.go`, `middleware.go` → `middleware_test.go` (every example middleware), `router.go` → `router_test.go` (every example router's `TestRouter`, one call to `apitest.CheckRouter`). A test file whose name matches no source is renamed after the one it tests — db's `config_test.go` is `lifecycle_test.go`, api/server's `metrics_test.go` is `metric_test.go`.

When a feature spans several source files and none owns it, name the file after the **feature** instead:

- `transaction_test.go` — `Transaction` is implemented in `context.go`
- `plugin_test.go` — the plugin feature lives in `option.go` (`WithPlugins`), `manager.go` (the field) and `business.go` (the install loop)

The example's service tests sit in `manager_test.go`, beside the `newManager` they build on, whichever of the service's files the method under test lives in.

## The example's service tests

The pattern the role and user tests follow — [testify](https://github.com/stretchr/testify), real SQL, a setup that cleans up after itself:

```go
// pkg/services/system/user/manager_test.go
func setup(t *testing.T, mb model.MessageBus) *manager {
	t.Helper()
	db, err := testutil.SetupDB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Cleanup(true)) })
	require.NoError(t, db.ORM().AutoMigrate(&orm.Organization{}, &orm.Role{}, /* ... */))
	r := repository.New(platform.New(db, nil, nil, nil))
	m := newManager(platform.New(db, nil, nil, mb), r)
	require.NoError(t, m.Init(context.Background()))
	return m
}
```

- **The real repository over in-memory SQLite**, not a repository double, so a test exercises the query the service relies on.
- **A fake embeds the interface it stands in for** and implements only what the test drives — `type fakeRepo struct{ repo.HelloWorld }` in the example service's test; an unimplemented method hits the embedded nil interface and panics, which is the point.
- **Assert the error's category**, not just that one came back: `errors.Is(err, errors.NotFound)`. A check that passes on any error passes on the wrong one.

## Shared helpers — `pkg/utils/testutil`

| Helper | Gives you |
|---|---|
| `SetupDB()` | an initialized `db.Manager` on in-memory SQLite, so a test exercises real SQL instead of a repository double (needs `CGO_ENABLED=1`). Each call gets a database of its own, shared by every connection in its pool. The manager also carries `model.DatabaseDebugger` — `Cleanup`/`Reload` — which a test cleans up through; the services under test get it as a plain `model.Database` and cannot |
| `NewTracer(provider)` + `Swap(provider)` | a `model.Tracer` whose provider can be replaced mid-flight — the case `traceutil.LiveProvider` is for; the tracing service's own provider is never replaced |
| `NewRecorder()` | in-memory exporter + synchronous provider, so a span is readable the moment it ends |
| `NewMeter(provider)` | a `model.Meter` over the given provider |
| `NewMeterRecorder()` | a `ManualReader` + provider, so a test collects what was recorded when it asks |
| `SumInt64(rm, name)` / `Histogram[N](rm, name)` | read one metric out of collected `ResourceMetrics`; assert the `found` bool, so a missing or misnamed metric is not read as zero |

The example service's `TestHelloWorldIncrementsCounter` is the meter trio at work: `newManager(platform.New(nil, testutil.NewMeter(provider), nil, fakeMB{}), fakeRepo{})`, two calls, `reader.Collect`, then `SumInt64(rm, "example.hello.total")`. A test that needs *some* `model.Meter` with no assertions takes `metrics.New()`, which needs no setup: a platform handle is never nil, in a test as in the wiring.

Reach for `NewTracer` only when the test actually swaps a provider — the real tracing service never does: its `Init` replaces the pipeline behind a provider that stays the same. A test that merely needs *some* `model.Tracer` is better served by `tracing.New()`, which serves a noop provider with no `Init`, config or exporter. See [../pkgs/tracing.md](../pkgs/tracing.md).
