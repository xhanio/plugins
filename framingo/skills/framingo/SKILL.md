---
name: framingo
description: Use when working with Framingo (`github.com/xhanio/framingo`) Go code — bootstrapping a new framingo backend project, creating services, registering with the supervisor, configuring HTTP servers/routers, the database service, pub/sub messaging, or implementing service lifecycle interfaces. Triggers on mentions of framingo, "new Go backend", service lifecycle, supervisor, handler groups, or any framingo package imports.
compatibility: Requires Go 1.24+. Documents github.com/xhanio/framingo v0.6.4 — where the repo's go.mod pins a different framingo version, trust the code over this prose.
metadata:
  author: xhanio
  version: "1.3.2" # mirrors plugin.json; bump both together
  framingo: v0.6.4 # the framework version these docs describe
---

# Framingo - Service-Oriented Go Framework

## Overview

Framingo is a modular, production-ready Go framework for building HTTP API applications with service lifecycle management, database integration, pub/sub messaging, and health monitoring.

## When to Use

- **Bootstrapping a new framingo backend project** (see [Starting a New Backend](#starting-a-new-backend) below)
- Creating a new framingo service or `Manager` interface
- Registering services with the supervisor or wiring service dependencies
- Implementing the lifecycle interfaces (`Service`, `Initializable`, `Daemon`, `Liveness`, `Readiness`, `Debuggable`)
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
| [pkgs/supervisor.md](pkgs/supervisor.md) | Lifecycle interfaces, supervisor orchestration, config-from-context |
| [pkgs/api.md](pkgs/api.md) | The API server: registration flow, router.yaml schema, middleware model + configs, WebSockets, error format |
| [pkgs/client.md](pkgs/client.md) | The framework HTTP client (`api/client`): requests, encoding, global headers |
| [pkgs/db.md](pkgs/db.md) | `db.Manager`: drivers, options, transactions, dynamic pool config |
| [pkgs/pubsub.md](pkgs/pubsub.md) | Pub/sub primitive, message bus, message interfaces, slow subscribers |
| [pkgs/planner.md](pkgs/planner.md) | Scheduled/ad-hoc task execution: plans, jobs, stats selectors |
| [pkgs/types.md](pkgs/types.md) | Framework types: `common` interfaces, the `fapi` surface, ORM base types, context keys |
| [pkgs/config.md](pkgs/config.md) | The annotated config.yaml template, env layering, dynamic keys |
| [pkgs/errors.md](pkgs/errors.md) | `xhanio/errors`: categories, wrapping, checking |
| [pkgs/utils.md](pkgs/utils.md) | Logging, `pkg/structs/` data structures, notable `pkg/utils/` helpers |

**Writing the application — `app/`**

| File | Covers |
|---|---|
| [app/layout.md](app/layout.md) | The categorized `pkg/` layout, the three-layer access rule, import order |
| [app/services.md](app/services.md) | Writing a service: interface-in-two-places, `New`/`newManager`, options |
| [app/routers.md](app/routers.md) | Authoring routers: the `router.go`/`handler.go`/`router.yaml` triple, the two `api` packages, project `api.Context`, `DiscoverHandlers` |
| [app/middlewares.md](app/middlewares.md) | Authoring `api.Middleware`: the `Func(config)` contract, config-free vs configured, decline, attachment |
| [app/types.md](app/types.md) | The project's `types/` categories, which layer owns each, the types-first design order, the two `api` packages |
| [app/components.md](app/components.md) | The `components/` wiring category and how its three subtrees fit together |
| [app/components-server.md](app/components-server.md) | The application daemon: file structure, layered service creation, registration order, signals |
| [app/components-cmd.md](app/components-cmd.md) | Cobra CLI wiring: thin `main`s, the daemon subcommand, the operator CLI over the SDK |
| [app/components-client.md](app/components-client.md) | The app SDK component: typed operations, credential/session handling over the HTTP client |

## Starting a New Backend

Two routes. **Check first whether the framingo repo is on this machine** (`example/` present, or the module in the Go module cache) — that decides which one you can actually run.

### Route A — fork `example/` (preferred, needs the repo)

`example/` is a self-contained Go module shipping a production-shaped service: supervisor wiring, PostgreSQL + migrations, pub/sub + message bus, WebSocket stream, RBAC (auth/user/role/organization/certificate), Echo router with auth & throttle middlewares, structured logging, pprof, signal handling, plus a CLI client.

Its build layer — `project.yaml`, `build/`, `env/`, `dist/`, the Docker image and Kubernetes manifests — belongs to [GoPro](https://github.com/xhanio/gopro), a separate tool, **not to framingo**. Don't treat GoPro as a framingo requirement or reproduce its config when scaffolding: the entries under `build/binary/` are ordinary `main` packages that `go build` handles. Two things GoPro otherwise does for you: it generates the gitignored `dist/` tree that the example's `db.migration.dir` points into (repoint it at `env/...` if you skip GoPro), and it injects `pkg/types/info` build metadata at link time, without which `info.ProductName` is empty and the env-var prefix is blank.

Get it, then follow its QUICKSTART:

```bash
git clone https://github.com/xhanio/framingo
# fork-and-rename recipe, "Keep vs. rip out" pruning table:
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
# replace the `myapp` import paths with your module path.
mkdir -p pkg/routers/order pkg/services/order
cp <skill>/_templates/{router.go,handler.go,router.yaml} pkg/routers/order/

# The service's two interface halves (see app/services.md)
cp <skill>/_templates/types-model-order.go    pkg/types/model/order.go
cp <skill>/_templates/services-order-model.go pkg/services/order/model.go
```

All five `types/` subdirs are part of the layout ([app/layout.md](app/layout.md)); `model/` in particular is **not optional** — the router template imports `myapp/pkg/types/model`.

`_templates/api-context.go` is self-contained (no project types). Its trailing comment shows how to add `Credential()`/`Session()` accessors over your own `entity` package once you have one.

Then: write services ([app/services.md](app/services.md)), wire them in `pkg/components/server/` ([app/components-server.md](app/components-server.md)), and register routers with the API server ([app/routers.md](app/routers.md), [pkgs/api.md](pkgs/api.md)).

Either route, the reference map above is the per-concept guide for the work inside the new project.

## Quick Reference

| Concern | Package | Interface / Key Type | Docs |
|---|---|---|---|
| Service orchestration | `pkg/services/supervisor` | `supervisor.Manager` | [pkgs/supervisor.md](pkgs/supervisor.md) |
| Database | `pkg/services/db` (+ `db/drivers/`) | `db.Manager`; blank-import a driver subpackage (sqlite/mysql/postgres/clickhouse); sqlite needs `CGO_ENABLED=1` | [pkgs/db.md](pkgs/db.md) |
| HTTP API server | `pkg/services/api/server` + `pkg/types/api` (alias `fapi`) | `server.Manager`, `fapi.Router`, `fapi.Middleware` | [pkgs/api.md](pkgs/api.md) |
| Handler request context | **project's own** `<project>/pkg/types/api` (unaliased `api`) | `api.Context` — use as the handler ctx instead of `echo.Context`; not a framingo type, you own it | [app/routers.md](app/routers.md) |
| Middlewares | project `pkg/middlewares/<name>/` | `fapi.Middleware` — `Func(config []byte)` | [app/middlewares.md](app/middlewares.md) |
| App wiring | project `pkg/components/{cmd,server,client}/` | the server daemon + CLI + SDK components | [app/components.md](app/components.md) and its `components-*.md` files |
| HTTP client | `pkg/services/api/client` | `client.Client` | [pkgs/client.md](pkgs/client.md) |
| Pub/Sub primitive | `pkg/services/pubsub` (+ `pubsub/driver/`) | `pubsub.Manager`; Memory/Redis/Kafka drivers | [pkgs/pubsub.md](pkgs/pubsub.md) |
| Message bus (on top of pubsub) | `pkg/services/messagebus` | `messagebus.Manager`, `model.MessageBus`, `model.Messenger` | [pkgs/pubsub.md](pkgs/pubsub.md) |
| Task planner | `pkg/services/planner` | `planner.Manager`, `model.Planner` | [pkgs/planner.md](pkgs/planner.md) |
| Message interfaces | `pkg/types/common` | `Message`, `MessageSender`, `MessageHandler`, `RawMessageHandler` | [pkgs/pubsub.md](pkgs/pubsub.md) |
| Service interfaces | `pkg/types/model` | `Supervisor`, `Database`, `Pubsub`, `MessageBus`, `Planner` | [pkgs/types.md](pkgs/types.md) |
| Logging | `pkg/utils/log` | `log.Logger` | [pkgs/utils.md](pkgs/utils.md) |
| Errors | `github.com/xhanio/errors` | `errors.Newf`, `errors.Wrap`, category sentinels | [pkgs/errors.md](pkgs/errors.md) |
| Config | `pkg/utils/confutil` | `confutil.FromContext(ctx)` | [pkgs/supervisor.md](pkgs/supervisor.md), [pkgs/config.md](pkgs/config.md) |

## Architecture

```
CLI / Config (cobra + viper)
    |
Supervisor (lifecycle orchestration, dependency resolution, health monitoring)
    |
Services (DB, API Server, PubSub, MessageBus, Planner, custom services)
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
- A router's `prefix` MUST match the package's domain (e.g., `routers/user/` → `/users`), not an arbitrary path.
- Don't declare handlers as `func(c echo.Context) error` — use the project's `api.Context` (`<project>/pkg/types/api`). Handlers that take `echo.Context` compile and run, so nothing tells you it's wrong; you just lose `context.Context` (forcing `c.Request().Context()` at every service call) and the `Credential()`/`Session()`/`TraceID()` helpers.
- Don't hand-write the `Handlers()` map — each router's `router.go` returns `api.DiscoverHandlers(r)` (plus the debug log). Listing `map[string]any{"ListUsers": r.ListUsers}` by hand forces `echo.HandlerFunc` signatures (so you lose `api.Context`) and breaks on rename. Keep it in `router.go`; `handler.go` holds bodies only.
- Don't look for `Context` in framingo's `pkg/types/api` — it isn't there. `Context`/`DiscoverHandlers` are **project**-side (`example/pkg/types/api/api.go`); framingo's package (aliased `fapi`) only has `Router`, `Middleware`, `ErrorBody`, `ContextKey*`. Importing both unaliased is a compile error — alias the framework one `fapi`.
- Don't hand-write `api.Context`/`DiscoverHandlers` from this skill's prose — copy [_templates/api-context.go](_templates/api-context.go). A reconstruction compiles but diverges, and every handler in the project then depends on the divergence.
- Don't give a router a `pkg/services/...` dependency — it takes the `model.X` business interface. Importing the service package leaks the implementation and its lifecycle methods.
- Don't skip a layer: routers and middlewares call services only — never `services/repository/`, `types/orm/`, or `db`; services persist through the repo level (`repository.Repository`), never `db.Manager`/GORM directly. `orm` types stop at the service boundary (mapped to `entity` there), `api` DTOs stop at the router.
- `SendMessage`/`SendRawMessage` return **nothing** and take the sender as the second arg: `mb.SendRawMessage(ctx, m, kind, payload)`. `if err := mb.SendMessage(...)` does not compile.
- `pubsub.Subscribe(name, topic)` returns a **channel**, not a handler registration — there is no `Subscribe(topic, handler)`. For handler-style dispatch use `messagebus`.
- Ports are `uint`, not `int`: `server.WithEndpoint(host, port uint, prefix)` and `db.Source.Port uint`. Feed them `config.GetUint(...)` — `GetInt` is a compile error.
- `db.WithConnection` takes **five** args (`maxOpen, maxIdle, maxLifetime, maxIdleTime, execTimeout`); the four-arg form doesn't compile.
- Every `func:` in `router.yaml` needs a matching method, or registration fails at startup with `handler function <Name> not found in router.Handlers()`. Middleware names must be registered first, or `middleware <name> not found`.
- Don't use the global Viper singleton — use the instance passed via `context.Context` (`confutil.FromContext(ctx)`).
- After `echo.Shutdown` is called, the same echo instance can't be reused — the framework's api server rebuilds it on `Init`, but custom services must do the same if they wrap net/http servers.

## Key Patterns

1. **Functional Options** - All services use `With*()` option functions for configuration
2. **Interface Composition** - Combine small interfaces (`Service` + `Initializable` + `Daemon`) as needed
3. **Context Propagation** - Config and transactions flow through `context.Context`
4. **Dependency Declaration** - Services declare dependencies via `Dependencies()`, manager resolves order
5. **Categorized Packages** - ALL code under `pkg/` must follow the category structure above
6. **Type Separation** - Each domain concept has `api/` (wire), `entity/` (domain), `orm/` (DB), plus `model/` (service interfaces) and `repo/` (repository interfaces)
7. **Concrete Constructors** - `New` returns the exported interface; unexported `newRouter`/`newManager` return the concrete type for package tests
8. **Error Handling** - ALWAYS use `github.com/xhanio/errors`, NEVER use `fmt.Errorf` or stdlib `errors`
9. **Import Order** - Three groups: stdlib, third-party, project — always separated by blank lines
10. **Layered Access** - the api level (routers, middlewares) calls services only; services persist through the repo level only; only `services/repository/` touches the database
11. **Types First** - for a new feature, design `types/*` (api → entity/model → orm/repo) before implementing the router, service, or repo
