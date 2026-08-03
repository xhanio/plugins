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
- **Helpers have a home.** `BindAny`/`Validate` above, and `Credential()`/`Session()`/`TraceID()` where a handler needs auth state, live on the interface. Adding one later touches your `api.go` only — never every handler signature.
- **You own it.** The interface is project-side; extending it needs no framingo change.
- **Zero framework cost.** `api.DiscoverHandlers(r)` wraps each `func(api.Context) error` into the `echo.HandlerFunc` the server registers. Raw `echo.Context` handlers still work as the fallback for third-party code — not the pattern for new handlers.

**Routers stay at the api level.** The handler above touches exactly two
type categories: it binds a `types/api` DTO and returns what the service
hands back — a `types/entity` value, serialized as JSON. It calls the
service level through `model.Example` and nothing lower: no
`services/repository/`, no `types/orm/`, no `db`. The constructor is the
boundary — every example router takes only `model.*` interfaces and a
logger ([layout.md](layout.md)); the health router's is framingo's own
`model.Supervisor`. And the DTO and the `model.*` method are
designed before the handler is written — [types.md](types.md).

Same for WebSocket handlers: `func(c api.Context, conn *websocket.Conn) error`.

If a project has no `pkg/types/api/api.go` yet (i.e. it wasn't forked from `example/`), copy [`_templates/api-context.go`](../_templates/api-context.go) into it before writing handlers, and adjust the `entity` import to the project's own.

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
mounts this group; middleware names resolve against what the server
component registered ([components-server.md](components-server.md)).

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
skipped **silently**, so the count is your only startup signal. Each example
router also pins the yaml↔handler mapping in a `router_test.go` built on
`newRouter` — every `func:` in the embedded `router.yaml` must resolve to a
discovered handler.

## Wiring routers into the server

Routers are constructed with their model interfaces and registered in the
server component's `api.go` — middlewares first, then routers, both
error-checked. The example's full registration is in
[components-server.md](components-server.md); the contracts and YAML schema
are in [api.md](../pkgs/api.md).
