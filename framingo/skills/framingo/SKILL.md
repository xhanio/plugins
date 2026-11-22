---
name: framingo
description: Use when working with Framingo (`github.com/xhanio/framingo`) Go code — bootstrapping a new framingo backend project, creating services, registering with the supervisor, configuring HTTP servers/routers, the database service, pub/sub messaging, or implementing service lifecycle interfaces. Triggers on mentions of framingo, "new Go backend", service lifecycle, supervisor, handler groups, or any framingo package imports.
compatibility: Requires Go 1.25+. Documents github.com/xhanio/framingo v0.9.10 — where the repo's go.mod pins a different framingo version, trust the code over this prose.
metadata:
  author: xhanio
  version: "1.15.2" # mirrors plugin.json; bump both together
  framingo: v0.9.10 # the framework version these docs describe
---

# Framingo - Service-Oriented Go Framework

## Overview

Framingo is a modular, production-ready Go framework for building HTTP API applications with service lifecycle management, database integration, pub/sub messaging, and health monitoring.

## When to Use

- **Bootstrapping a new framingo backend project** (see [Starting a New Backend](#starting-a-new-backend) below)
- Creating a new framingo service or `Manager` interface
- Registering services with the supervisor or wiring service dependencies
- Implementing the lifecycle interfaces (`Service`, `Initializable`, `Daemon`, `Liveness`, `Readiness`, `Debuggable`, `Reconcilable`)
- Configuring HTTP servers, routers, handler groups, middlewares, or WebSocket routes
- Working with the database service (`db.Manager`, transactions, migrations)
- Publishing or subscribing on the pub/sub bus
- Reading config via `confutil.FromContext(ctx)` or shaping the app's YAML config
- Touching any `github.com/xhanio/framingo/...` import

**When NOT to use**: generic Go questions, libraries unrelated to `xhanio/framingo`, or non-Go codebases.

## Reference Map

One file per concept, in two halves — `pkgs/` is how to **use framingo's packages**, `app/` is how to **write an application** shaped like `example/`. Load the one the task touches:

**Using the framework packages — `pkgs/`**

| File | Covers |
|---|---|
| [pkgs/supervisor.md](pkgs/supervisor.md) | Lifecycle interfaces, supervisor orchestration, health probes & escalation, config-from-context |
| [pkgs/api.md](pkgs/api.md) | The API server: registration flow, router.yaml schema, middleware model + configs, WebSockets, error format |
| [pkgs/client.md](pkgs/client.md) | The framework HTTP client (`api/client`): requests, encoding, global headers |
| [pkgs/db.md](pkgs/db.md) | `db.Manager`: drivers, options, transactions, dynamic pool config |
| [pkgs/pubsub.md](pkgs/pubsub.md) | Pub/sub primitive, message bus, message interfaces, slow subscribers |
| [pkgs/plan.md](pkgs/plan.md) | Scheduled/ad-hoc task execution: plans, the task manager, commands, stats selectors |
| [pkgs/tracing.md](pkgs/tracing.md) | OpenTelemetry tracing: the `model.Tracer` handle, exporter registry, the server's Trace middleware |
| [pkgs/metrics.md](pkgs/metrics.md) | OpenTelemetry metrics: the stable `model.Meter` provider, exporter registry, the export loop, instruments in services |
| [pkgs/types.md](pkgs/types.md) | Framework types: `common` interfaces, the `fapi` surface, ORM base types, context keys |
| [pkgs/config.md](pkgs/config.md) | The annotated config.yaml template, env layering, dynamic keys |
| [pkgs/errors.md](pkgs/errors.md) | `xhanio/errors`: categories, wrapping, checking |
| [pkgs/utils.md](pkgs/utils.md) | Logging, `pkg/structs/` data structures, notable `pkg/utils/` helpers |

**Writing the application — `app/`**

| File | Covers |
|---|---|
| [app/layout.md](app/layout.md) | The categorized `pkg/` layout, the three-layer access rule, import order |
| [app/services.md](app/services.md) | Writing a service: interface-in-two-places, `New`/`newManager`, options, health probes |
| [app/testing.md](app/testing.md) | Test package choice (`package foo` by default, `foo_test` only when a registry forces it), test file naming, `pkg/utils/testutil` |
| [app/routers.md](app/routers.md) | Authoring routers: the `router.go`/`handler.go`/`router.yaml` triple, the two `api` packages, project `api.Context`, `DiscoverHandlers` |
| [app/middlewares.md](app/middlewares.md) | Authoring `api.Middleware`: the `Func(enabled, config)` contract, switches, config-free vs configured, decline, attachment |
| [app/types.md](app/types.md) | The project's `types/` categories, which layer owns each, the types-first design order, the two `api` packages |
| [app/components.md](app/components.md) | The `components/` wiring category and how its three subtrees fit together |
| [app/components-server.md](app/components-server.md) | The application daemon: file structure, layered service creation, registration order, signals |
| [app/components-cmd.md](app/components-cmd.md) | Cobra CLI wiring: thin `main`s, the daemon subcommand, the operator CLI over the SDK |
| [app/components-client.md](app/components-client.md) | The app SDK component: typed operations, credential/session handling over the HTTP client |

**Why it is shaped this way — `design/`**

| File | Covers |
|---|---|
| [design/README.md](design/README.md) | Why things are shaped as they are — the index of design decisions, by area, each with its reason and its cost |
| [design/who-decides.md](design/who-decides.md) | Route vs service vs handler: authz fails closed, the service does not ask who calls, cross-service checks in the handler, bad input is a 400 |
| [design/narrow-contracts.md](design/narrow-contracts.md) | The smallest interface per dependency: a service's own `Repository`, `common.*` collaborators, message handlers as `Named` |
| [design/data-access.md](design/data-access.md) | orm stays in the repository; `types/repo` contracts and repo-only inputs; the repository decides nothing |
| [design/service-wiring.md](design/service-wiring.md) | Platform first, nil-tolerant `Dependencies`, one instance per name, the api server last by ordering, restarts invisible to dependents, a dependent `Reconcile`s what it registered with a dependency stopped or started alone |
| [design/lifecycle.md](design/lifecycle.md) | What a start waits for: each `Start` bounded, a service started once its `Init` succeeded and its dependencies run. What a stop may leave: the taker always awaited; other work left, cut, or waited for per resource, by what it holds |
| [design/config.md](design/config.md) | A setting is construction, fixed at `New`, or reloadable, its option the default of a key `Init` re-reads, the config winning where it sets the key; config read in `Init` from the context, a restart as the reload; a reload published whole; the file re-read on SIGHUP alone |
| [design/websockets.md](design/websockets.md) | `wsutil.Bridge` as a session utility, ending one that lost its subscription, shaped by options and `ReadFirst`; `wsutil.Close` by the handler's result, a `CloseWith` its own; the server closing its connections on stop |
| [design/certificates.md](design/certificates.md) | CSRs keep the requester's key; a server CA and an agent CA; CA keys in the database alone; downloads encoded as the caller asks; TLS verifies each side against the CAs it is given |
| [design/checks.md](design/checks.md) | Which decisions a machine checks, and by what: the lint config's import boundaries and forbidden calls, `framingo vet`'s analyzers, the router, docs and template tests; how an exception carries its reason; `framingo verify` |
| [design/comments.md](design/comments.md) | What a comment says — what the code cannot: a caller's contract, why, an option's key and built-in default — a design note named instead of its reasoning repeated, no history |
| [design/migrations.md](design/migrations.md) | Migrate on start locally, once per release in prod (`exampleapp migrate`), env-overridable config |

## Starting a New Backend

Two routes. **Check first whether the framingo repo is on this machine** (`example/` present, or the module in the Go module cache) — that decides which one you can actually run.

### Route A — fork `example/` (preferred, needs the repo)

`example/` is a self-contained Go module shipping a production-shaped service: supervisor wiring, PostgreSQL + migrations, pub/sub + message bus, WebSocket stream, RBAC (auth/user/role/organization/certificate), Echo router with auth & throttle middlewares, structured logging, pprof, signal handling, plus a CLI client.

Its build layer — `project.yaml`, `build/`, `env/`, `dist/`, the Docker image and Kubernetes manifests — belongs to [GoPro](https://github.com/xhanio/gopro), a separate tool, **not to framingo**. Don't treat GoPro as a framingo requirement or reproduce its config when scaffolding: the entries under `build/binary/` are ordinary `main` packages that `go build` handles. Two things GoPro otherwise does for you: it generates the gitignored `dist/` tree that the example's `db.migration.dir` points into (repoint it at `env/...` if you skip GoPro), and it injects `pkg/types/info` build metadata at link time, without which `info.ProductName` is empty and the env-var prefix is blank.

Get it, then follow its QUICKSTART:

```bash
git clone https://github.com/xhanio/framingo
# fork-and-rename recipe, "What to Keep, What to Rip Out" pruning table:
#   framingo/example/QUICKSTART.md → "Use This Folder as Your Starting Template"
```

Online reference: <https://github.com/xhanio/framingo/blob/main/example/QUICKSTART.md>

### Route B — scaffold from the bundled templates (no repo needed)

This skill ships the load-bearing files in [_templates/](_templates/). Use this route when `example/` isn't reachable — **do not hand-write `api.Context` or `DiscoverHandlers` from the prose in this skill; copy the template.** Reconstructing them by hand produces a subtly different `Context` that every handler then depends on.

```bash
go mod init github.com/yourorg/myapp
go get github.com/xhanio/framingo@latest

mkdir -p cmd/myapp \
         pkg/{components/{cmd,server},services,routers,middlewares,utils} \
         pkg/types/{api,entity,model,orm,repo}

# The project-owned api.Context + DiscoverHandlers. Compiles as-is.
cp <skill>/_templates/api-context.go pkg/types/api/api.go

# Canonical router triple — copy per domain, rename the package,
# replace github.com/yourorg/myapp with your module path.
mkdir -p pkg/routers/order pkg/services/order
cp <skill>/_templates/{router.go,handler.go,router.yaml} pkg/routers/order/

# The service's two interface halves (see app/services.md)
cp <skill>/_templates/types-model-order.go    pkg/types/model/order.go
cp <skill>/_templates/services-order-model.go pkg/services/order/model.go

# The lint config that keeps the layout's import boundaries, and the
# command that verifies a change (see "Verifying a Change" below)
cp <skill>/_templates/golangci.yml .golangci.yml
go install github.com/xhanio/framingo@latest
```

All five `types/` subdirs are part of the layout ([app/layout.md](app/layout.md)); `model/` in particular is **not optional** — the router template imports `github.com/yourorg/myapp/pkg/types/model`.

`_templates/api-context.go` is self-contained (no project types). Its trailing comment shows how to add `Credential()`/`Session()` accessors over your own `entity` package once you have one.

Then: write services ([app/services.md](app/services.md)), wire them in `pkg/components/server/` ([app/components-server.md](app/components-server.md)), and register routers with the API server ([app/routers.md](app/routers.md), [pkgs/api.md](pkgs/api.md)).

Either route, the reference map above is the per-concept guide for the work inside the new project.

## Verifying a Change

After any change, before calling it done, run the one command that checks
everything a machine can check:

```bash
framingo verify            # in an app; in the framingo repo: framingo verify . example
```

Install it once, at the framework version the project uses:
`go install github.com/xhanio/framingo@latest`. It needs golangci-lint v2 on
the PATH, built with the Go that builds the code
(`go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest`).

It formats, builds, vets, lints and tests (with `-race`) each module, runs
every step even after one fails, and ends with `verified` or the list of
what failed, exiting non-zero. Its `framingo vet` step — also a command of
its own, `framingo vet ./...` — runs the framework's analyzers: a `Start`
keeping its ctx, a held service missing from `Dependencies` or a
`Dependencies` that filters, a service's
`New` not taking the platform first, a `*gorm.DB` kept in a field, config
read outside `Init`, an option in an `option.go` marked neither construction
nor reloadable. A boundary or vet
failure names the design note it enforces — `(design/narrow-contracts.md)`
— so read that note before changing the code or opting out; a `//nolint`,
or a `//framingo:ignore <rule> <reason>` for an analyzer, carries its
reason. What each check holds: [design/checks.md](design/checks.md).

## Quick Reference

| Concern | Package | Interface / Key Type | Docs |
|---|---|---|---|
| Service orchestration | `pkg/services/supervisor` | `supervisor.Manager` | [pkgs/supervisor.md](pkgs/supervisor.md) |
| Database | `pkg/services/db` (+ `db/drivers/`, `db/plugins/`) | `db.Manager`; blank-import a driver subpackage (sqlite/mysql/postgres/clickhouse); sqlite needs `CGO_ENABLED=1`; per-statement spans via `otelgorm.New(tracer, gormtracing.WithoutQueryVariables())` (gorm's `tracing` plugin package, aliased; returns an error) → `db.WithPlugins(p)`; pass that option or bound values land in `db.query.text` | [pkgs/db.md](pkgs/db.md) |
| HTTP API server | `pkg/services/api/server` + `pkg/types/api` (alias `fapi`) | `server.Manager`, `fapi.Router`, `fapi.Middleware`; `server.WithMeter` turns on per-listener HTTP request metrics | [pkgs/api.md](pkgs/api.md) |
| Handler request context | **project's own** `<project>/pkg/types/api` (unaliased `api`) | `api.Context` — use as the handler ctx instead of `echo.Context`; not a framingo type, you own it | [app/routers.md](app/routers.md) |
| Middlewares | project `pkg/middlewares/<name>/` | `fapi.Middleware` — `Func(enabled bool, config []byte)` | [app/middlewares.md](app/middlewares.md) |
| App wiring | project `pkg/components/{cmd,server,client}/` | the server daemon + CLI + SDK components | [app/components.md](app/components.md) and its `components-*.md` files |
| HTTP client | `pkg/services/api/client` | `client.Client` | [pkgs/client.md](pkgs/client.md) |
| Pub/Sub primitive | `pkg/services/pubsub` (+ `pubsub/driver/`) | `pubsub.Manager`; Memory/Redis/Kafka drivers | [pkgs/pubsub.md](pkgs/pubsub.md) |
| Message bus (on top of pubsub) | `pkg/services/messagebus` | `messagebus.Manager`, `model.MessageBus`, `model.Messenger`; `Register(module common.Named)` delivers to whichever handler interface the module implements | [pkgs/pubsub.md](pkgs/pubsub.md) |
| WebSocket ↔ bus | `pkg/utils/wsutil` | `return wsutil.Bridge(conn, messenger, opts...)` (blocks; closes the messenger, returns how to close the conn); the api server closes the conn with `wsutil.Close`, a `wsutil.CloseWith(status, reason)` with its own | [pkgs/pubsub.md](pkgs/pubsub.md), [pkgs/utils.md](pkgs/utils.md) |
| Plan manager | `pkg/services/plan` (+ `pkg/utils/plans`) | `plan.Manager`, `model.Plan` | [pkgs/plan.md](pkgs/plan.md) |
| Task manager | `pkg/services/task` (+ `pkg/utils/jobs`) | `task.Manager`, `model.Task`, `entity.Task`, `jobs.Command` | [pkgs/plan.md](pkgs/plan.md) |
| Tracing (OpenTelemetry) | `pkg/services/tracing` (+ `tracing/exporters/`) and the `trace` middleware in `pkg/middlewares/trace` | `tracing.Manager`, `model.Tracer`; blank-import an exporter subpackage (otlpgrpc/otlphttp/stdout); attach the middleware via `WithMiddlewares`, activate with `trace: true` per server; db spans via `pkg/services/db/plugins/otelgorm` | [pkgs/tracing.md](pkgs/tracing.md) |
| Metrics (OpenTelemetry) | `pkg/services/metrics` (+ `metrics/exporters/`) | `metrics.Manager`, `model.Meter`; blank-import an exporter subpackage (otlpgrpc/otlphttp/stdout); the provider's identity never changes — build instruments once at `Init` and cache them | [pkgs/metrics.md](pkgs/metrics.md) |
| Message interfaces | `pkg/types/common` | `Message`, `MessageSender`, `MessageHandler`, `RawMessageHandler` | [pkgs/pubsub.md](pkgs/pubsub.md) |
| Service interfaces | `pkg/types/model` | `Supervisor`, `Database` (+ `DatabaseDebugger`), `Pubsub`, `MessageBus`, `Messenger`, `Plan`, `Task`, `Tracer`, `Meter` | [pkgs/types.md](pkgs/types.md) |
| Logging | `pkg/utils/log` | `log.Logger` | [pkgs/utils.md](pkgs/utils.md) |
| Errors | `github.com/xhanio/errors` | `errors.Newf`, `errors.Wrap`, category sentinels | [pkgs/errors.md](pkgs/errors.md) |
| Config | `pkg/utils/confutil` | `confutil.Config` via `confutil.FromContext(ctx)`; getters take format-string keys (`GetInt("%s.max_open", nameutil.ConfigRoot(m.name))`), a reloadable setting read over its default (`GetIntOr(c.MaxOpen, "%s.connection.max_open", root)`) | [pkgs/supervisor.md](pkgs/supervisor.md), [pkgs/config.md](pkgs/config.md) |
| Health probes | `pkg/types/common` | `Liveness`/`Readiness` — `Alive(ctx)`/`Ready(ctx)`; served at `/healthz` + `/readyz` by the example's health router | [pkgs/supervisor.md](pkgs/supervisor.md), [app/services.md](app/services.md) |

## Architecture

```
CLI / Config (cobra + viper)
    |
Supervisor (lifecycle orchestration, dependency resolution, health monitoring)
    |
Services (DB, API Server, PubSub, MessageBus, Plan, Task, Tracing, Metrics, custom services)
    |
Types (common interfaces, api types, model interfaces, entity data, orm base)
```

**Module**: `github.com/xhanio/framingo`

## Error Handling

All errors in framingo MUST use `github.com/xhanio/errors` — never `fmt.Errorf` or stdlib `errors`. The API server's error handler routes on `xhanio/errors` categories to set the response HTTP status.

```go
return errors.NotFound.Newf("user %s not found", id)
if err := s.db.FromContext(ctx).Create(u).Error; err != nil {
    return errors.Wrapf(err, "failed to create user %s", u.Name)
}
```

For the full category table, wrapping rules, combining, checking, and custom categories, see [pkgs/errors.md](pkgs/errors.md).

## Package Organization

All application packages MUST follow the categorized `pkg/` layout. Every category directory is a grouping folder only — Go source files always live in subdirectories, never at a category root.

Categories:
- `components/` — application wiring (cmd, server daemons, client SDKs)
- `services/` — business logic, one `Manager` interface per service
- `routers/` — HTTP route handlers (`router.go`, `router.yaml`, `handler.go`)
- `middlewares/` — `api.Middleware` implementations
- `types/api/`, `types/entity/`, `types/model/`, `types/orm/`, `types/repo/` — request DTOs, domain entities, service interfaces, DB models, and repo interfaces, kept strictly separate
- `utils/` — stateless shared helpers

For the full category rules, type-separation example, server component file structure, and import organization, see [app/layout.md](app/layout.md).

## Common Mistakes

- `errors.New("msg")` does not compile — use `errors.Newf("msg")`. `New` takes functional options, not a message.
- Never return raw `err` — always `errors.Wrap(err)` or `errors.Wrapf(err, "context")`. Raw returns drop the stack trace.
- Never use `fmt.Errorf` or stdlib `errors.New` — the API server's error handler routes on `xhanio/errors` categories to set HTTP status.
- Don't place `.go` files at a `pkg/` category root — every category is a grouping folder; code lives in subdirectories (`pkg/services/user/`, not `pkg/services/foo.go`).
- A router's `prefix` MUST match the package's domain (e.g., `routers/user/` → `/users`), not an arbitrary path. One router.yaml may hold several handler groups, one per `---`-separated document, each with its own `prefix` and group middlewares and all served by one `Handlers()` — for a resource with a second surface, such as `/users` and `/user`. A route declared twice — in one group, across groups or across routers — fails registration with a `Conflict`.
- Don't declare handlers as `func(c echo.Context) error` — use the project's `api.Context` (`<project>/pkg/types/api`). Handlers that take `echo.Context` compile and run, so nothing tells you it's wrong; you just lose `context.Context` (forcing `c.Request().Context()` at every service call) and the `Credential()`/`Session()`/`TraceID()` helpers.
- Don't hand-write the `Handlers()` map — each router's `router.go` returns `api.DiscoverHandlers(r)` (plus the debug log). Listing `map[string]any{"ListUsers": r.ListUsers}` by hand forces `echo.HandlerFunc` signatures (so you lose `api.Context`) and breaks on rename. Keep it in `router.go`; `handler.go` holds bodies only.
- Don't look for `Context` in framingo's `pkg/types/api` — it isn't there. `Context`/`DiscoverHandlers` are **project**-side (`example/pkg/types/api/api.go`); framingo's package (aliased `fapi`) only has `Router`, `Middleware`, `ErrorBody`, `ContextKey*`. Importing both unaliased is a compile error — alias the framework one `fapi`.
- Don't hand-write `api.Context`/`DiscoverHandlers` from this skill's prose — copy [_templates/api-context.go](_templates/api-context.go). A reconstruction compiles but diverges, and every handler in the project then depends on the divergence.
- Don't give a router a `pkg/services/...` dependency — it takes the `model.X` business interface. Importing the service package leaks the implementation and its lifecycle methods.
- Don't skip a layer: routers and middlewares call services only — never `services/repository/`, `types/orm/`, or `db`; services persist through their own `Repository` interface — the `repo.*` contracts declared in their `model.go` — never `db.Manager`/GORM directly. `orm` types stop at the repository boundary — the repository maps rows to `entity` and the `repo` contracts speak entity only — and `api` DTOs stop at the router.
- Don't give the api server dependencies to order its start. To the supervisor a dependency also gates `Init` and `Start` — a dependency that failed its init or start keeps the dependent's from running — and rolls health up, so the listener, health listener included, would stay down whenever any service behind a route failed. Register it after `TopoSort` instead: an ordering, not a dependency.
- `supervisor.Register` returns an error — handle it. It refuses another instance under a name already taken (two `db` managers without `WithName`, say), which used to drop the second one from the graph silently, never initialized or started.
- `SendMessage`/`SendRawMessage` return **nothing** and take the sender as the second arg: `mb.SendRawMessage(ctx, m, kind, payload)`. `if err := mb.SendMessage(...)` does not compile.
- `pubsub.Subscribe(name, topic)` returns a **channel**, not a handler registration — there is no `Subscribe(topic, handler)`. For handler-style dispatch use `messagebus`.
- Ports are `uint`, not `int`: `server.WithEndpoint(host, port uint, prefix)` and `db.Source.Port uint`. Feed them `config.GetUint(...)` — `GetInt` is a compile error.
- `db.WithConnection` takes **five** args (`maxOpen, maxIdle, maxLifetime, maxIdleTime, execTimeout`); the four-arg form doesn't compile.
- Every `func:` in `router.yaml` needs a matching method, or registration fails at startup with `handler function <Name> not found in router.Handlers()`. Middleware names must be registered first, or `middleware <name> not found`.
- Don't call `Cleanup`/`Reload` through a `model.Database` — they are not there. They live on `model.DatabaseDebugger`, which only the `db.Manager` itself carries: tests and dev tooling, never a repository.
- Don't write a `package db` test that blank-imports a driver — `db/drivers/*` import `db` to self-register, so an internal test package hits `import cycle not allowed in test`. Registry packages whose subpackages register back into them (framingo's `db` and its drivers) are the one place a `_test` package is required; everywhere else `package foo` is the default, since `newManager` exists to be used from inside.
- Don't read a reloadable setting in `Init` with a plain getter: a key the config does not set reads zero and wipes the default its option set. Resolve the start's settings from `m.defaults` with the `Get…Or` getters (`s.interval = config.GetDurationOr(s.interval, "%s.interval", root)`), publish them whole, and put each option of `option.go` under a `// Construction options:` or `// Reloadable options:` comment — `framingo vet` reports one under neither ([design/config.md](design/config.md)).
- Don't use the global Viper singleton — use the `confutil.Config` passed via `context.Context` (`confutil.FromContext(ctx)`), and read under the service's root (`"%s.key", nameutil.ConfigRoot(m.name)`) rather than a hardcoded one, or a second instance of the service reads the first one's config.
- Don't fail `Alive` on a dependency outage — restarting the service raises no database; that fails `Ready` only. And probes take the caller's ctx: derive (`context.WithTimeout(ctx, cap)`), never fabricate from `Background()`.
- Don't expect a query made through bare `db.ORM()` to appear inside the request's trace — it carries `context.Background()`, so with tracing wired its spans export as detached roots. Anything that gets a context to the statement parents correctly: `FromContext(ctx)`, `FromContextTimeout(ctx, d)`, or `ORM().WithContext(ctx)`. Prefer `FromContext(ctx)` — `ORM().WithContext(ctx)` traces the same but bypasses a transaction carried in the context, running on the base connection instead.
- Don't let `Stop(ctx)` return while anything the service began is still going and `ctx` has not ended, don't let it wait past `ctx` either, and don't let what a stop left reach into the `Start` after it: the supervisor restarts the same instance. Share across starts only what outlives one, such as a `syncutil.WaitGroup` of all the work and the `syncutil.Loop` that takes it; give a start its own context — `Start`'s values without its cancel, so `Stop` alone ends it — and its own of whatever hands out work or capacity. See the [`Daemon` contract](pkgs/supervisor.md#the-daemon-contract).
- Don't keep in a field what a dependency hands out — a `*gorm.DB`, the `*sql.DB` from `DB()`: the supervisor restarts a dependency alone, and what it handed out may be what the restart closed. Keep the handle and ask it each time ([design/service-wiring.md](design/service-wiring.md#a-restart-is-invisible-to-the-dependents)). What a handle promises to keep across its restart may be kept: the meter's provider and the instruments built from it, the tracing service's provider and a `Tracer` from it. `framingo vet` holds the `*gorm.DB` case alone; the rest is for review.
- A service that registers with a dependency — subscribes to pubsub, adds tasks to a task manager — implements `common.Reconcilable` and puts back what it holds on `DependencyRunning`: the dependency forgets it when it stops, and the supervisor restarts the dependency alone. `Reconcile` acts on the state it is given alone, registers and nothing slower, and never calls the supervisor ([pkgs/supervisor.md](pkgs/supervisor.md#reconciling-with-a-dependency)).
- Don't make a message handler a `Service` just to receive messages — `common.MessageHandler`/`RawMessageHandler` embed `common.Named`, and `MessageBus.Register` takes a `common.Named`: a name is all the bus keys the subscription on.
- There is no `messagebus.AttachWebSocket` — a WS handler gets a `Messenger` from `NewMessenger(name)` and returns `wsutil.Bridge(conn, messenger)`. Don't close the conn in the handler: return, and the api server closes it per the returned error with `wsutil.Close` (and closes every open one when it stops); for a close of your own — a refusal, a displacement — return `wsutil.CloseWith(status, reason)`.
- In the example, `authz` fails closed: a handler under it that names no `permission:` is refused with 403. Every handler declares one, or opts out with `- authz: false` in its own `middlewares`.
- Don't wrap a failed `c.Validate(&body)` into a new category — it is already `InvalidArgument`, a 400; `errors.Wrap` it. An uncategorized error answers 500.
- After `echo.Shutdown` is called, the same echo instance can't be reused — the framework's api server rebuilds it on `Init`, but custom services must do the same if they wrap net/http servers.

## Key Patterns

1. **Functional Options** - All services use `With*()` option functions for configuration
2. **Interface Composition** - Combine small interfaces (`Service` + `Initializable` + `Daemon`) as needed
3. **Context Propagation** - Config (`confutil.Config`, format-string keys read under `nameutil.ConfigRoot(m.name)`) and transactions flow through `context.Context`
4. **Dependency Declaration** - Services declare dependencies via `Dependencies()`, manager resolves order
5. **Categorized Packages** - ALL code under `pkg/` must follow the category structure above
6. **Type Separation** - Each domain concept has `api/` (wire), `entity/` (domain), `orm/` (DB), plus `model/` (service interfaces) and `repo/` (repository interfaces)
7. **Concrete Constructors** - `New` returns the exported interface; unexported `newRouter`/`newManager` return the concrete type for package tests, which is why test files default to `package foo`, not `package foo_test`
8. **Error Handling** - ALWAYS use `github.com/xhanio/errors`, NEVER use `fmt.Errorf` or stdlib `errors`
9. **Import Order** - Three groups: stdlib, third-party, project — always separated by blank lines
10. **Layered Access** - the api level (routers, middlewares) calls services only; services persist through the repo level only; only `services/repository/` touches the database
11. **Types First** - for a new feature, design `types/*` (api → entity/model → orm/repo) before implementing the router, service, or repo
12. **Probe Split** - `Alive(ctx)` fails only if a restart would fix it; `Ready(ctx)` means "can it serve right now"; a dependency outage fails readiness, never liveness
13. **Platform First** - every app service's constructor takes `model.Platform` first (`New(platform, ...)`), whether it uses it yet or not; framework services excepted ([app/services.md](app/services.md))
