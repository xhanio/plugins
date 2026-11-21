# Framingo Package Layout Reference

The required `pkg/` directory structure, category rules, type separation,
and import organization. The tree below is `example/pkg/` as it stands — new
projects mirror it.

## Package Organization

**IMPORTANT**: All application packages MUST follow the categorized
directory structure under `pkg/`. This is a strict convention — do NOT place
code outside these categories or flatten the hierarchy.

## The Three Layers

The categories arrange into three layers. Calls go strictly downward, one
layer at a time — and each layer owns its `types/` categories:

```
api level        routers/  middlewares/               owns: types/api
   │  may call: services, via model.* interfaces
   ▼
service level    services/system/  services/example/  owns: types/entity  types/model
   │  may call: the repo level, via repo.* interfaces
   ▼
repo level       services/repository/                 owns: types/orm  types/repo
                 — the only code that touches the database / GORM
```

- **Routers and middlewares call services, nothing lower.** Every example
  router constructor takes `model.*` interfaces only (the health router a
  narrow `Supervisor` of its own); middlewares take the service they lean
  on (`authz` → the role service). None of them imports
  `services/repository/`, `types/orm/`, or `db`.
- **Services call the repo level, nothing lower.** They persist through
  their own `Repository` interface — the `repo.*` contracts declared in
  their `model.go` — and hand `entity` values upward. No system or business
  service holds `model.Database` or imports GORM — grep the example: only
  `services/repository/` does (and the platform bundle that carries the
  handle to it). Peer services compose
  through `model.*` interfaces (auth is built on `model.UserAuthN`), never
  by reaching past each other into the repo.
- **A check that spans two services is the router's.** A service keeps the
  rules about its own data; when a use case needs another service's answer
  — that the role a user is given exists — the handler asks it before
  calling the first. See
  [design/who-decides.md#a-check-that-spans-two-services-is-the-handlers](../design/who-decides.md#a-check-that-spans-two-services-is-the-handlers).
- **The repo level owns persistence.** `services/repository/` implements
  every `types/repo/` interface over `types/orm/` models; SQL, GORM, and
  transactions live there and nowhere else, and rows become `entity`
  values before they leave it. It decides nothing: what a list leaves out,
  what may not be deleted, how a password is hashed are the services' rules
  — the repository counts, filters by the options it is given, and stores
  what it is handed
  ([design/data-access.md](../design/data-access.md)).

Design in the same shape: the types are each layer's contract, so **write
`types/*` first** — the order is in [types.md](types.md).

## The `pkg/` Tree

```
pkg/
├── components/                  # application wiring — see components.md
│   ├── client/example/          #   the SDK component (components-client.md)
│   ├── cmd/app/  cmd/cli/       #   cobra trees: daemon / operator CLI (components-cmd.md)
│   └── server/example/          #   the daemon: model, manager, lifecycle, config,
│                                #   service, api, signal, migrate (components-server.md)
├── services/
│   ├── repository/              # data access — implements every types/repo interface
│   ├── system/                  #   auth/ certificate/ organization/ role/ user/, and
│   │                            #   platform/ (builds the model.Platform bundle, not a service)
│   └── example/                 # business tier
├── routers/                     # auth/ certificate/ example/ health/ messagebus/ role/ user/
│   └── example/                 #   router.go, router.yaml, handler.go, router_test.go
├── middlewares/                 # authnagent/ authnuser/ authz/ cors/ deflate/ feature/ throttle/
│   └── throttle/                #   middleware.go, option.go, middleware_test.go
├── types/
│   ├── api/ entity/ model/ orm/ repo/    # the five core categories
│   └── message/ preset/ rbac/            # grown as the app needed them
└── utils/
    ├── apitest/                 # test helpers for routers: CheckRouter
    └── infra/                   # process-wide state: Debug, StartTime, SerialNumber, ConfigDir, EnvPrefix
```

**IMPORTANT**: Every `pkg/` category directory is a grouping folder only —
NEVER place Go source files directly in a category root. Nested grouping
dirs are fine (`services/system/` holds six packages); the rule
still applies to them.

## Category Rules

| Category | Purpose | Key Rule |
|---|---|---|
| `components/cmd/` | Cobra command trees — one package per binary persona | One subdir per binary under `build/binary/`, names need not match: the example pairs `cmd/app` → `build/binary/exampleapp` and `cmd/cli` → `build/binary/examplecli`. No business logic — flag parsing and delegation only |
| `components/server/` | Application daemon — owns the supervisor, wires all services, handles signals | Only place that knows about ALL services; one file per concern ([components-server.md](components-server.md)) |
| `components/client/` | Go client SDK exposing typed methods over HTTP | Consumed by `components/cmd/cli/` and external callers; depends on `types/api/` + `types/entity/`, never on services |
| `services/` | Business logic, in tiers: `repository/` (data access) → `system/` → business (`example/`) | Each service declares dependencies via `Dependencies()` and never imports a sibling service package — it takes the peer's `types/model/` interface, and its own `Repository` (the `types/repo/` contracts it uses, in its `model.go`) for data |
| `routers/` | HTTP handlers — each router owns a `router.yaml` + `Handlers()` returning `api.DiscoverHandlers(r)` | Split per package into `router.go` (wiring) + `handler.go` (bodies taking the project `api.Context`); business logic delegates to services ([routers.md](routers.md)) |
| `middlewares/` | `fapi.Middleware` implementations, one package each | May depend on services (constructor + `Dependencies()`, like a router) — `authz` takes `model.Role`, never the service package ([middlewares.md](middlewares.md)) |
| `types/api/` | Request/response DTOs, middleware config blocks, the project `api.Context` | Tags: `json`, `form`, `query`, `validate`. NO gorm tags |
| `types/entity/` | Pure business domain models | Tags: `json` only. Returned from services to callers |
| `types/model/` | Service **business** interfaces (`example.go` declares `model.Example`) | Imported by routers and other services so implementations stay package-private. Pairs with the service's own `Manager` — see below |
| `types/orm/` | Database table models | Tags: `gorm` only. Must implement `TableName()`. Never exposed outside the repository: `types/repo` contracts speak `entity` |
| `types/repo/` | Repository interfaces, one file per domain, plus inputs only the repository takes (`repo.UserCreateOptions`, carrying the password hash) | Implemented by `services/repository/`; services depend on the interface, never a concrete repo. Each embeds `repo.Repository` — `common.Service` plus `Transaction` — so a service opens a unit of work through the contract, never through the database |
| `utils/` | Shared helpers | Stateless — with `utils/infra` as the one deliberate exception: process-wide facts (`Debug`, `StartTime`, `SerialNumber`, `ConfigDir`, `EnvPrefix`) set once at startup by the server component |

Categories are not a closed set: the example grew `types/message/`,
`types/preset/`, and `types/rbac/` as those domains emerged
([types.md](types.md)).

### `types/model/` vs the service's own `Manager` — both, not either

Every service declares its interface in two places. They are not duplicates:

| File | Declares | Consumed by |
|---|---|---|
| `types/model/example.go` | `model.Example` — business methods + `common.Service`, **no lifecycle** | Routers, other services |
| `services/example/model.go` | `example.Manager` = `model.Example` + the lifecycle interfaces it implements; `example.Repository` = the `repo.*` contracts it uses | Only `components/server/` wiring |

A router's constructor takes `model.Example`, never `example.Manager` —
that's what stops the router from seeing (or calling) the service's
`Start`/`Stop`. A router importing `pkg/services/...` has skipped the split.
Real pair + templates: [services.md](services.md).

## Type Separation

The same domain concept has one type per layer — wire (`api`), domain
(`entity`), persistence (`orm`), plus the `model`/`repo` interface pair.
The example's helloworld walks all of them in ~30 lines of real code:
[types.md](types.md).

**Data flows**: router binds `api.HelloWorldCreateRequest` → calls
`model.Example` → service persists via `repo.HelloWorld` → repository
writes `orm.HelloWorld` and returns `entity.HelloWorld` → service returns it
→ router sends it as JSON.

## Server Component — Application Daemon

All server implementations MUST follow the fixed file-per-responsibility
structure (`model.go` / `manager.go` / `lifecycle.go` / `config.go` /
`service.go` / `api.go` / `signal.go` / `migrate.go`), specified in full — tiered service
creation, registration order, signals — in
[components-server.md](components-server.md). The reference implementation is
[`example/pkg/components/server/example/`](https://github.com/xhanio/framingo/tree/main/example/pkg/components/server/example).

## Import Organization

**IMPORTANT**: All Go imports MUST be organized into exactly three groups,
separated by blank lines:

1. **Go standard library**
2. **Everything external to your module** — third-party libraries *and* `github.com/xhanio/framingo/*` / `github.com/xhanio/errors`
3. **Your own module** only

The framework is a dependency like any other, so it goes in group 2, **not**
with your own packages. Group 3 holds exclusively the current module. The
real `pkg/routers/example/router.go`:

```go
import (
	// group 1: Go standard library
	_ "embed"

	// group 2: third-party + framingo (external to this module)
	fapi "github.com/xhanio/framingo/pkg/types/api"
	"github.com/xhanio/framingo/pkg/types/common"
	"github.com/xhanio/framingo/pkg/utils/log"
	"github.com/xhanio/framingo/pkg/utils/nameutil"

	// group 3: this module (the example is its own Go module)
	"github.com/xhanio/framingo/example/pkg/types/api"
	"github.com/xhanio/framingo/example/pkg/types/model"
)
```

Never mix groups. Never use more than three groups. Each group is
alphabetically sorted.

This is what every file under
[`example/pkg/`](https://github.com/xhanio/framingo/tree/main/example/pkg)
does, and what the files in [`_templates/`](../_templates/) do — when in
doubt, copy their grouping.
