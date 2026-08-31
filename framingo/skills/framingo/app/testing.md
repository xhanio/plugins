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

The same pattern holds for routers (`newRouter`) and any other `newXxx` constructor — see [services.md](services.md).

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

- `pkg/services/tracing` — `tracing/exporters/*` call `tracing.Register`
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

Mirror the source file: `health.go` → `health_test.go`, `migrate.go` → `migrate_test.go`.

When a feature spans several source files and none owns it, name the file after the **feature** instead:

- `transaction_test.go` — `Transaction` is implemented in `context.go`
- `plugin_test.go` — the plugin feature lives in `option.go` (`WithPlugins`), `manager.go` (the field) and `business.go` (the install loop)

## Shared helpers — `pkg/utils/testutil`

| Helper | Gives you |
|---|---|
| `SetupDB()` | an initialized `db.Manager` on in-memory SQLite, so a test exercises real SQL instead of a repository double (needs `CGO_ENABLED=1`) |
| `NewTracer(provider)` + `Swap(provider)` | a `model.Tracer` whose provider can be replaced mid-flight, standing in for a tracing-service restart |
| `NewRecorder()` | in-memory exporter + synchronous provider, so a span is readable the moment it ends |

Reach for `NewTracer` only when the test actually swaps a provider — the real tracing service only does that through `Init`. A test that merely needs *some* `model.Tracer` is better served by `tracing.New()`, which serves a noop provider with no `Init`, config or exporter. See [../pkgs/tracing.md](../pkgs/tracing.md).
