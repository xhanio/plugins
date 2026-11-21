# Routers — Authoring HTTP Handlers

The project-side router pattern: the `router.go`/`handler.go`/`router.yaml`
triple, the project `api.Context`, and handler discovery. For the server that
consumes routers — registration flow, YAML schema, middleware resolution,
WebSockets — see [api.md](../pkgs/api.md).

Routes are defined declaratively: each `fapi.Router` ships an embedded
`router.yaml` plus a `Handlers()` map; the server manager binds them at
registration time. Each package splits into `router.go` (wiring) and
`handler.go` (handler bodies). The example ships seven —
`auth`, `certificate`, `example`, `health`, `messagebus`, `role`, `user` —
and `routers/example` is the smallest business one, so it is quoted below in
full. (`routers/health` mounts `/healthz` + `/readyz` on the dedicated
`health` listener: `Healthz` follows `supervisor.Alive()` — red only when
recovery is spent — and `Readyz` follows `supervisor.Ready()`, itemizing the
not-ready services. Since the probes live on `supervisor.Manager` — model
interfaces stay lifecycle-free — the router declares its own narrow
`Supervisor` interface (the two verdicts + `Stats`), which
`supervisor.Manager` satisfies. It takes no middlewares and declares no
dependencies — the supervisor can't be a node in its own graph.) Templates:
[`_templates/router.go`](../_templates/router.go),
[`_templates/handler.go`](../_templates/handler.go),
[`_templates/router.yaml`](../_templates/router.yaml).

## Two `api` packages — don't confuse them

Router code imports both. The example project's convention (follow it):

```go
import (
    fapi "github.com/xhanio/framingo/pkg/types/api"  // FRAMEWORK: Router, Middleware, ContextKey*, ErrorBody
    "myapp/pkg/types/api"                            // PROJECT (yours): Context, DiscoverHandlers, DTOs
)
```

- `fapi` — **framingo's** `pkg/types/api`. Has `Router`, `Middleware`, `RequestInfo`, `ErrorBody`, `ContextKeyCredential`. It has **no `Context` type**.
- `api` — **your project's** `pkg/types/api`, which you own and can extend. Defines `Context`, `DiscoverHandlers`, `WrapHandler`, `WrapWebSocket`, and request/response DTOs.

`api.Context` below always means the **project** one. Referring to it as a framingo type is a mistake — framingo ships no such interface.

## `handler.go` — use the project `api.Context`

**Declare every handler as `func(c api.Context) error` — not
`func(c echo.Context) error`.** `api.Context` is the interface *your
project* defines (canonical version:
[`_templates/api-context.go`](../_templates/api-context.go)), embedding
`echo.Context` **and** `context.Context` plus project helpers. The example's
smallest handler file, complete:

```go
// pkg/routers/example/handler.go
package example

import (
	"net/http"

	"github.com/xhanio/errors"

	"github.com/xhanio/framingo/example/pkg/types/api"
)

func (r *router) Example(c api.Context) error {
	var req api.HelloWorldCreateRequest
	if err := c.BindAny(&req); err != nil {
		return errors.BadRequest.Newf("invalid request: %v", err)
	}
	if err := c.Validate(&req); err != nil {
		return errors.Wrap(err)
	}
	body, err := r.em.HelloWorld(c, req.Message)
	if err != nil {
		return errors.Wrap(err)
	}
	return c.JSON(http.StatusOK, body)
}
```

Why this is the pattern:

- **One value, both contracts.** `c` satisfies `echo.Context` (bind/respond) *and* `context.Context` — `r.em.HelloWorld(c, ...)` passes it straight through, so cancellation/deadlines propagate for free, no `c.Request().Context()` unwrap.
- **Helpers have a home.** `BindAny`/`Validate` above; `BindPath()`/`BindQuery()`/`BindForm()` for typed parameters (`c.BindPath().MustInt32("id", &userID).BindError()` in the user router); `Credential()`/`Session()`/`TraceID()` where a handler needs auth state — `Credential()` is `entity.CredentialFrom(c)`, the same reader services use. All live on the interface. Adding one later touches your `api.go` only — never every handler signature.
- **Bad input is a 400.** A failed bind is `errors.BadRequest`; `c.Validate` reports a failed `validate` tag as `InvalidArgument`, which the server answers with a 400 — so wrapping its error is enough ([design/who-decides.md](../design/who-decides.md#bad-input-is-a-400)).
- **You own it.** The interface is project-side; extending it needs no framingo change.
- **Zero framework cost.** `api.DiscoverHandlers(r)` wraps each `func(api.Context) error` into the `echo.HandlerFunc` the server registers. Raw `echo.Context` handlers still work as the fallback for third-party code — not the pattern for new handlers.

**Routers stay at the api level.** The handler above touches exactly two
type categories: it binds a `types/api` DTO and returns what the service
hands back — a `types/entity` value, serialized as JSON. It calls the
service level through `model.Example` and nothing lower: no
`services/repository/`, no `types/orm/`, no `db`. The constructor is the
boundary — every example router takes only `model.*` interfaces and a
logger ([layout.md](layout.md)) — the messagebus router framingo's own
`model.MessageBus`, the health router the narrow `Supervisor` it declares. And the DTO and the `model.*` method are
designed before the handler is written — [types.md](types.md).

**A handler may orchestrate services, never decide for one.** When a
request needs two services' answers — the user router checks with the role
service that a role exists before the user service assigns it — the handler
holds both `model.*` interfaces and puts them in order; the rules about a
service's own data stay in that service. Why, and where the line falls:
[design/who-decides.md#a-check-that-spans-two-services-is-the-handlers](../design/who-decides.md#a-check-that-spans-two-services-is-the-handlers).

Same for WebSocket handlers: `func(c api.Context, conn *websocket.Conn) error`,
declared `method: WS`. The messagebus router's `Stream` hands the connection
to `wsutil.Bridge` and returns; the server closes the connection by what the
handler returned (`wsutil.Close`), and with `StatusGoingAway` when it stops
([design/websockets.md](../design/websockets.md)).

If a project has no `pkg/types/api/api.go` yet (i.e. it wasn't forked from `example/`), copy [`_templates/api-context.go`](../_templates/api-context.go) into it before writing handlers. It compiles with no project types; add `Credential()`/`Session()` over your own `entity` package as its closing "Extending Context" note shows.

## `router.go` — the wiring file

The complete real one. Wiring lives here, never in `handler.go`:

```go
// pkg/routers/example/router.go
package example

import (
	_ "embed"

	fapi "github.com/xhanio/framingo/pkg/types/api"
	"github.com/xhanio/framingo/pkg/types/common"
	"github.com/xhanio/framingo/pkg/utils/log"
	"github.com/xhanio/framingo/pkg/utils/nameutil"

	"github.com/xhanio/framingo/example/pkg/types/api"
	"github.com/xhanio/framingo/example/pkg/types/model"
)

var _ fapi.Router = (*router)(nil)

//go:embed router.yaml
var config []byte

type router struct {
	name string
	log  log.Logger

	em model.Example // the model interface — never the service package
}

func New(em model.Example, log log.Logger) fapi.Router {
	return newRouter(em, log)
}

// newRouter returns the concrete router, the form package tests construct.
func newRouter(em model.Example, log log.Logger) *router {
	r := &router{
		em:  em,
		log: log,
	}
	r.name = nameutil.Name(r) // project-relative, layout root stripped: routers/example/router
	return r
}

func (r *router) Name() string {
	return r.name
}

func (r *router) Dependencies() []common.Service {
	return []common.Service{
		r.em,
	}
}

func (r *router) Config() []byte {
	return config
}

func (r *router) Handlers() map[string]any {
	handlers := api.DiscoverHandlers(r)
	r.log.Debugf("router %s parsed %d handler(s)", r.Name(), len(handlers))
	return handlers
}
```

And the `router.yaml` it embeds:

```yaml
server: http # server name
prefix: /example # group prefix
middlewares:
  - throttle # group level — applies to every handler below
handlers:
  - method: POST
    path: /helloworld
    func: Example # key of Handlers() == the method name
    middlewares:
      - authnuser # handler level
      - deflate
```

`server:` names which API server (an `api.<name>` entry in config.yaml)
mounts this group — it is required; middleware names resolve against what
the server component registered ([components-server.md](components-server.md)).
A route declared twice — in any group of any router — fails registration.
The full schema is in [api.md](../pkgs/api.md#router-yaml-config-format-routeryaml).

### Several groups, permissions, and fail-closed authz

The user router shows the rest of the schema. Its `router.yaml` holds two
handler groups, one YAML document each, both resolved against the one
`Handlers()` map — `/users` (any account by ID, `local_user_manage_*`) and
`/user` (the caller's own, resolved from the credential, `local_user_*`):

```yaml
# authz sits at group level, after authnuser: a handler-level entry wraps outermost
# and would run before the credential is set.
server: http
prefix: /users
middlewares:
  - throttle
  - authnuser
  - authz
handlers:
  - method: GET
    path: /:id
    func: Get
    permission: local_user_manage_read
  # ...
---
server: http
prefix: /user
middlewares:
  - throttle
  - authnuser
  - authz
handlers:
  - method: GET
    path: /
    func: GetCurrent
    permission: local_user_read
  # authz refuses a caller whose password must be reset, and this is where it is reset
  - method: PUT
    path: /passwords
    func: ChangePassword
    middlewares:
      - authz: false
```

- **`permission:`** names the `types/rbac` constant the `authz` middleware
  checks the caller's role against; the server only carries it to the
  request. It means nothing to a route without `authz`.
- **authz fails closed.** Under `authz`, a handler that declares no
  `permission:` is refused, so a forgotten declaration locks the route
  rather than opening it. A route every signed-in caller may reach opts out
  with `- authz: false`. Why the route, not the service, decides:
  [design/who-decides.md](../design/who-decides.md).
- **Two routes, not a flag.** Whose account an operation touches is the
  path's question: the `/user` handlers read the account from
  `c.Credential()` and hand the service an ID; the service never asks who
  is calling.

## `Handlers()` — always `DiscoverHandlers`

Every router's `Handlers()` has the same three lines, verbatim across all
seven example routers. It reflects over the receiver, keys handlers by method
name (matching `func:` in that package's `router.yaml`), and wraps each
`func(api.Context) error` into `echo.HandlerFunc`. Adding a handler = write
the method in `handler.go` + add a `func:` entry to `router.yaml`. Nothing
else.

Don't hand-write the map (`map[string]any{"Example": r.Example}`) — it
forces `echo.HandlerFunc` signatures, defeating `api.Context`, and rots on
rename. Keep the debug line: methods that don't match a known signature are
skipped **silently** — a `func:` naming one fails registration with
`handler function <Name> not found in router.Handlers()`, at startup. Each
example router catches that before startup, in a one-line `router_test.go`
built on `newRouter` with nil services:

```go
func TestRouter(t *testing.T) {
	apitest.CheckRouter(t, newRouter(nil, log.Default))
}
```

The framework's `server.CheckRouter(router)` runs the registration checks
with no server — every `func:` resolves to a discovered handler whose
signature fits its method, no route is declared twice — and returns the
routes declared. The example's `apitest.CheckRouter` wraps it for a test and
adds the app's own rule: a `permission:` is one `rbac` defines, since a
misspelled one would lock out every role but admin. A multi-group router
asserts its groups from the routes returned (`routers/user/router_test.go`).

## Wiring routers into the server

Routers are constructed with their model interfaces and registered in the
server component's `api.go` — middlewares first, then routers, both
error-checked. The example's full registration is in
[components-server.md](components-server.md); the contracts and YAML schema
are in [api.md](../pkgs/api.md).
