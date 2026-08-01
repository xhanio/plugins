# Framingo API Server Reference

Full reference for the Framingo API server (`pkg/services/api/server`): the declarative routing model, registration flow, router and middleware contracts, YAML config format, handler keys, route mapping, WebSocket handlers, and middleware resolution.

## Naming: `fapi` vs `api`

Two different packages are in play throughout this document. Router code imports both, so the example project aliases the framework one:

| Import | Alias | Owned by | Provides |
|---|---|---|---|
| `github.com/xhanio/framingo/pkg/types/api` | `fapi` | framingo | `Router`, `Middleware`, `RequestInfo`, `ErrorBody`, `WrapError`, `ContextKey*`, `Endpoint` |
| `<project>/pkg/types/api` | none (`api`) | **you** | `Context`, `HandlerFunc`, `WebSocketHandlerFunc`, `WrapHandler`, `WrapWebSocket`, `DiscoverHandlers`, request/response DTOs |

**`api.Context` in this document is always the project's**, e.g. [`example/pkg/types/api/api.go`](_templates/api-context.go). Framingo does **not** define a `Context` interface — don't go looking for one in `fapi`, and don't import both packages unaliased (compile error).

## Route Registration Flow

```
1. Server instances created    →  srvMgr.Add("http", ...)
2. Middlewares registered      →  srvMgr.RegisterMiddlewares(authMW, ...)
3. Routers registered         →  srvMgr.RegisterRouters(myRouter)
   a. Router.Config() called  →  returns embedded YAML ([]byte)
   b. YAML unmarshaled        →  produces the server-private group config
   c. Router.Handlers() called →  returns map[string]any
      - Recommended: `return api.DiscoverHandlers(r)` — reflects over the router's
        `func(api.Context) error` methods (project api.Context) and wraps each into
        the echo signature below. The wrapping happens HERE, at registration time,
        so handler bodies never mention echo.Context.
   d. For each YAML handler:
      - Func name looked up in Handlers() map
      - Type asserted based on method (WS → func(echo.Context, *websocket.Conn) error, others → echo.HandlerFunc)
      - a server-private handler key generated: {Server, Method, Path}
      - Handler func stored in manager.handlerFuncs[key] or wsHandlerFuncs[key]
   e. Server matched by the group's server field
   f. Echo group created at endpoint.Path + group.Prefix
   g. For each handler:
      - Handler-specific + group-level middlewares collected
      - Route registered: group.Add(method, path, handlerFunc, middlewares...)
      - Handler metadata stored on server for runtime lookup
```

## Creating a Server Manager

```go
import "github.com/xhanio/framingo/pkg/services/api/server"

srvMgr := server.New(
    server.WithLogger(logger),
    server.WithDebug(true),
)

// Step 1: Add named server instances (each gets its own echo.Echo)
srvMgr.Add("http", server.WithEndpoint("0.0.0.0", 8080, "/"))
srvMgr.Add("admin", server.WithEndpoint("0.0.0.0", 9090, "/admin"),
    // Per-server middleware configs: a plain name-to-config mapping, since
    // defaults carry no order. A middleware's block is what its Func receives
    // wherever nothing more specific is configured; "cors" is the server's
    // built-in CORS — the one name it claims — enabled here ahead of routing.
    // A router.yaml entry with its own block wins, handler over group over
    // server.
    server.WithMiddlewareConfigs([]byte("cors: true\nthrottle:\n  rps: 100.0\n  burst_size: 200\n")),
)

// Step 2: Register middlewares BEFORE routers (routers reference them by name)
srvMgr.RegisterMiddlewares(authMiddleware, deflateMiddleware)

// Step 3: Register routers (triggers the full wiring flow above)
srvMgr.RegisterRouters(myRouter)
```

## Implementing a Router

A Router provides two things: a YAML config declaring routes, and a map of handler function implementations. The YAML `func` field is the lookup key into the `Handlers()` map.

### Handler signature: use the project `api.Context`, not `echo.Context`

**Declare every handler as `func(c api.Context) error`** — where `api.Context` is the interface **your project** defines (`<project>/pkg/types/api`, *not* framingo's `fapi`), embedding `echo.Context` + `context.Context`. Canonical implementation to copy: [`example/pkg/types/api/api.go`](_templates/api-context.go).

What you get over a bare `echo.Context`:

| | `echo.Context` | project `api.Context` |
|---|---|---|
| Call a context-aware service | `svc.Get(c.Request().Context(), id)` | `svc.Get(c, id)` — `c` *is* a `context.Context` |
| Read the credential | `v := c.Get(fapi.ContextKeyCredential)` + nil check + type assert | `cred, ok := c.Credential()` |
| Add a new per-request helper | touch every handler signature | add one method to your `api.go` |

The framework still accepts raw `echo.HandlerFunc` / `func(echo.Context) error`, and such handlers compile and serve traffic — nothing will flag them. That path exists for third-party/legacy handlers; **new handlers should not use it**. Let `api.DiscoverHandlers` wrap the richer signature into the echo one at registration time.

Because `api.Context` is project-side, a project that wasn't forked from `example/` may not have it yet. Copy `example/pkg/types/api/api.go` in (repointing its `entity` import) before writing handlers, rather than falling back to `echo.Context`.

**File layout convention** (used throughout `example/pkg/routers/`): split each router into two files in the same package — `router.go` for wiring (factory, `Name`/`Dependencies`/`Config`/`Handlers`) and `handler.go` for the actual handler method bodies. Within a router package, import the framework `api` package as `fapi` and the project `api.Context` wrapper unaliased as `api`.

```go
// pkg/routers/user/router.go
package user

import (
    _ "embed"
    "path"

    fapi "github.com/xhanio/framingo/pkg/types/api"
    "github.com/xhanio/framingo/pkg/types/common"
    "github.com/xhanio/framingo/pkg/utils/log"
    "github.com/xhanio/framingo/pkg/utils/reflectutil"

    "myapp/pkg/services/user"
    "myapp/pkg/types/api"
)

var _ fapi.Router = (*router)(nil) // compile-time interface check

//go:embed router.yaml
var config []byte

// Unexported struct — returns fapi.Router interface via factory
type router struct {
    name string
    log  log.Logger
    svc  user.Manager
}

func New(svc user.Manager, log log.Logger) fapi.Router {
    return &router{svc: svc, log: log}
}

func (r *router) Name() string {
    if r.name == "" {
        r.name = path.Join(reflectutil.Locate(r))
    }
    return r.name
}

func (r *router) Dependencies() []common.Service { return []common.Service{r.svc} }

// Config returns the embedded YAML that declares handler groups
func (r *router) Config() []byte { return config }

// Handlers returns func-name → implementation mapping.
// Keys MUST match the "func" field in router.yaml. DiscoverHandlers reflects
// over the router's methods and wraps any `func(api.Context) error` into
// echo.HandlerFunc automatically (and similarly for WS handlers).
// The debug log makes route registration visible during startup.
func (r *router) Handlers() map[string]any {
    handlers := api.DiscoverHandlers(r)
    r.log.Debugf("router %s parsed %d handler(s)", r.Name(), len(handlers))
    return handlers
}
```

### `Handlers()`: every `router.go` calls `DiscoverHandlers` on itself

**Each router package's own `router.go` implements `Handlers()` with this exact body — the four lines are identical in all six of `example/pkg/routers/` (auth, certificate, example, messagebus, role, user). Copy it verbatim; only the receiver differs.**

```go
func (r *router) Handlers() map[string]any {
    handlers := api.DiscoverHandlers(r)                                    // r = this router — discovers ITS methods
    r.log.Debugf("router %s parsed %d handler(s)", r.Name(), len(handlers))
    return handlers
}
```

Rules this encodes:

- **Per router, not shared.** `DiscoverHandlers(r)` reflects over the receiver you hand it, so each router discovers only its own methods. There is no central registry to update — a new router gets its own `Handlers()`; a new *handler* needs nothing beyond the method plus a `func:` entry in that package's `router.yaml`.
- **In `router.go`, never `handler.go`.** This is wiring. `handler.go` holds only handler bodies and imports no framework types.
- **Don't hand-write the map.** `return map[string]any{"ListUsers": r.ListUsers, ...}` forces `echo.HandlerFunc` signatures (losing `api.Context`), and silently rots when a handler is renamed — `DiscoverHandlers` picks up renames automatically, and a stale `func:` in the YAML fails loudly at `RegisterRouters`.
- **Keep the debug line.** It's how you confirm at startup that a handler was actually discovered; a method that doesn't match a known signature is skipped silently, and the count is the only signal.

The method name is the map key, so it must match `func:` in `router.yaml` exactly. `Handlers` itself is skipped during reflection, so it can't recurse.

```go
// pkg/routers/user/handler.go
package user

import (
    "myapp/pkg/types/api"
)

// Recommended: handlers take the project-defined api.Context (which embeds
// echo.Context + context.Context). Pass `c` directly to any context-aware API.
// No registration needed — router.go's DiscoverHandlers finds these by name.
func (r *router) ListUsers(c api.Context) error  { /* ... */ }
func (r *router) CreateUser(c api.Context) error { /* ... */ }
func (r *router) GetUser(c api.Context) error    { /* ... */ }
```

## Router YAML Config Format (`router.yaml`)

The `server` field targets a named server instance created via `srvMgr.Add()`. The `func` field maps to keys in `Handlers()`.

**IMPORTANT**: The `prefix` MUST be a conventional REST resource path that matches the semantic meaning of the router package. The router package name and the prefix should describe the same domain concept:

| Router package | Prefix | Why |
|---|---|---|
| `routers/user/` | `/users` | Manages user resources |
| `routers/order/` | `/orders` | Manages order resources |
| `routers/auth/` | `/auth` | Authentication endpoints |
| `routers/example/` | `/example` | Example endpoints |

Do NOT use arbitrary or mismatched prefixes (e.g., a `routers/user/` package with prefix `/api/accounts`). The prefix is the public API contract — it must be intuitive and consistent with the package that owns it.

```yaml
server: http              # MUST match a server name from srvMgr.Add("http", ...)
prefix: /users            # MUST match the router package's domain (e.g., routers/user/ → /users)
middlewares: [auth]        # group-level middlewares (applied to ALL handlers in group)
handlers:
  - method: GET
    path: /
    func: ListUsers       # looked up in Router.Handlers()["ListUsers"]
  - method: POST
    path: /
    func: CreateUser      # looked up in Router.Handlers()["CreateUser"]
    middlewares: [validate]  # handler-specific middlewares (applied before group middlewares)
  - method: GET
    path: /:id
    func: GetUser
    throttle:              # per-handler rate limiting
      rps: 10
      burst_size: 20
  - method: WS            # WebSocket handler — registered as GET, server upgrades automatically
    path: /feed
    func: Feed            # write as func(api.Context, *websocket.Conn) error;
                          # DiscoverHandlers wraps it to the echo signature
```

## Handler Key Format

The server uses a struct-based key to uniquely identify each handler:

```go
type handlerKey struct { // private to the server package
    Server string
    Method string
    Path   string
}
```

For example, the key for `ListUsers` is `{Server: "http", Method: "GET", Path: "/users"}`. Keys are used as map keys for direct lookups. The `matchHandler` logic falls back through: exact match → WS fallback (for GET requests) → ANY fallback → wildcard path matching.

## How Routes Map to Echo

The final Echo route path is: `server.endpoint.Path` + `group.Prefix` + `handler.Path`.

For a server added as `srvMgr.Add("http", server.WithEndpoint("0.0.0.0", 8080, "/"))` with the YAML above:
- `GET /users` → `ListUsers`
- `POST /users` → `CreateUser`
- `GET /users/:id` → `GetUser`

Root paths (`/`) are normalized by trimming the trailing slash, so both `/api/v1` and `/api/v1/` resolve to the same handler (via `RemoveTrailingSlash` pre-middleware).

## Router Interface (types)

These are framingo's, in `pkg/types/api` (the `fapi` alias) — note there is no `Context` here:

```go
// framingo pkg/types/api/model.go
type Router interface {
    common.Service
    Config() []byte                    // YAML config bytes (typically //go:embed)
    Handlers() map[string]any          // func name → echo.HandlerFunc or func(echo.Context, *websocket.Conn) error
}

type Middleware interface {
    common.Service
    // config is the raw YAML under the middleware's name, nil when there is
    // none. Called once per attachment at registration - and again on restart,
    // when routes are rebuilt - so per-route state lives in the returned
    // closure. An error fails registration; returning no function and no error
    // declines the attachment, and the server skips it.
    Func(config []byte) (func(echo.HandlerFunc) echo.HandlerFunc, error)
}
```

This is the *framework boundary*, and it speaks echo. The server type-switches over the values in `Handlers()` and accepts:

- `echo.HandlerFunc` / `func(echo.Context) error` — for HTTP methods
- `func(echo.Context, *websocket.Conn) error` — for `method: WS`

**That boundary is not your handler signature.** Write handlers against the project `api.Context` (`func(c api.Context) error`, and `func(c api.Context, conn *websocket.Conn) error` for WS), and let `api.DiscoverHandlers` convert them to the echo signatures above inside `Handlers()`. The echo types belong in the one line of `router.go` that calls `DiscoverHandlers` — not in `handler.go`.

Framingo exports no alias for the WS signature; the project wrapper defines `WebSocketHandlerFunc` over the project `Context` for you. Mismatched method/signature combinations fail at `RegisterRouters` time.

## WebSocket Handlers

Declare WebSocket routes with `method: WS` in YAML. The server automatically upgrades the connection (uses `github.com/coder/websocket`) and invokes the handler with the live `echo.Context` and the upgraded `*websocket.Conn`.

The recommended signature is `func(c api.Context, conn *websocket.Conn) error` (using your project-defined `api.Context`); `DiscoverHandlers` (called from `router.go`'s `Handlers()`) wraps it into the framework-expected `func(echo.Context, *websocket.Conn) error`. The method body lives in the package's `handler.go`.

```go
// pkg/routers/feed/handler.go — Feed is picked up automatically by
// DiscoverHandlers and routed wherever router.yaml maps func: Feed.
//
// Take the project api.Context — c is also a context.Context, so you
// can pass it straight to conn.Read / conn.Write.
func (r *router) Feed(c api.Context, conn *websocket.Conn) error {
    for {
        typ, msg, err := conn.Read(c)
        if err != nil {
            return nil // client disconnected
        }
        if err := conn.Write(c, typ, msg); err != nil {
            return err
        }
    }
}
```

**Lifecycle**: Middleware stack runs before upgrade (auth, logging, throttle all apply). Once upgraded, the server invokes the handler; on return, it closes the connection with the appropriate status (normal close on `nil`, `StatusGoingAway` if the request context was cancelled, `StatusInternalError` otherwise). Errors are logged via the server logger.

## Middleware Resolution

Middlewares are registered by name via `RegisterMiddlewares()`. The YAML config references them by the name returned from `Middleware.Name()`. During route registration:

1. Handler-specific middlewares are resolved first (from `handler.middlewares`)
2. Group-level middlewares are resolved next (from `group.middlewares`)
3. If any referenced middleware name is not found, registration fails with `NotImplemented` error

Built-in server middlewares (applied to all routes automatically):
`Request → Recover → CORS → [server-level via WithMiddlewares] → Logger → Info → Error → [Custom middlewares] → Handler → Response`

Recover, Logger, Info and Error are plain lifecycle functions — no config, no names. CORS and the `WithMiddlewares` additions are standard `api.Middleware`s, each built from the server's middleware configs under its own name. Recover sits outermost so even a panicking server-level middleware yields a structured error; CORS answers preflight before any user code; server-level middlewares run ahead of Info so they see every request, matched or not. Returning no function from `Func` declines attachment — CORS does exactly that until configured.

A router.yaml middleware entry is a bare name or a single-key map carrying that middleware's config for the route:

```yaml
middlewares:
  - authnuser          # bare — Func(nil)
  - throttle:          # configured — Func receives this block as raw YAML
      rps: 1
      burst_size: 3
```

The same name at handler and group level resolves once, the handler's entry winning; config resolves most-specific-first — the handler entry's block, else the group entry's, else the server's middleware config (`WithMiddlewareConfigs`), else nil. A null entry (`- name:`) carries no block and inherits the next level; an empty block (`- name: {}`) shadows it — the way a handler opts out of a group's or the server's config without detaching the middleware. CORS is one of the server's built-ins — browser protocol rather than app policy, sitting ahead of routing to answer preflight requests that match no route; `cors: true` in the middleware configs enables it, an `fapi.CORSConfig` policy block tightens it, `false` or absent keeps it off. `cors` is the one name the server claims: a registered middleware reusing it would share its config block, so pick a different one. Rate limiting stays app-side: the example ships `example/pkg/middlewares/throttle`, attached through router.yaml with its limit per route via a config block or server-wide via the middleware configs.

## Error Response Format

The server's built-in error middleware runs every handler error through [`api.WrapError`](https://github.com/xhanio/framingo/blob/main/pkg/types/api/error.go) and emits the wire-level [`api.ErrorBody`](https://github.com/xhanio/framingo/blob/main/pkg/types/api/error.go):

```go
type ErrorBody struct {
    Source  string     `json:"source,omitempty"`  // server instance name (from srvMgr.Add("<name>", ...))
    Status  int        `json:"status,omitempty"`  // HTTP status
    Code    string     `json:"code,omitempty"`    // app-defined error code (optional)
    Kind    string     `json:"kind,omitempty"`    // xhanio/errors category name (e.g. "NotFound")
    Message string     `json:"message,omitempty"` // human-readable; handlers may sanitize for security
    Details labels.Set `json:"details,omitempty"`
}
```

Returning `errors.NotFound.Newf(...)` from a handler sets `Status: 404` and `Kind: "NotFound"`. The `Kind` field is the wire-level signal that lets Go clients recover the original `xhanio/errors` category without rebuilding it from `Status` — see the next section.

A bare `errors.Category` returned from a handler (e.g. `return errors.NotImplemented.New()`) emits the status + `Kind` with no message. Non-`xhanio/errors` errors fall through to `500` with the raw `Error()` string as `Message`.

`Source` is populated automatically by the server's built-in error middleware with the name passed to `srvMgr.Add("<name>", ...)` (e.g. `"http"`, `"admin"`). It is informational — useful when a caller talks to multiple framingo servers or when a gateway aggregates errors from several upstreams. Clients SHOULD NOT branch logic on `Source` value (the server name is a deployment detail, not part of the API contract). Its main practical use is the client-side check at [`pkg/services/api/client/client.go`](https://github.com/xhanio/framingo/blob/main/pkg/services/api/client/client.go) where an empty `Source` after a successful JSON parse signals "this response did not originate from a framingo server" — for example, a load balancer's 503 page or an HTML error from an upstream proxy.

## Consuming the Server with `pkg/services/api/client`

The framingo HTTP client decodes any 4xx/5xx response into a `*api.ErrorBody` and returns it as the error from `Do`/`Send`. On the error path the response body is replaced with an `io.NopCloser`, so callers can `defer resp.Body.Close()` unconditionally — there is no separate cleanup branch.

**Recommended error-handling pattern** (requires `github.com/xhanio/errors` v1.0.3+ for `LookupCategory`):

```go
import (
    stderrors "errors"

    "github.com/labstack/echo/v4"
    "github.com/xhanio/errors"
    "github.com/xhanio/framingo/pkg/services/api/client"
    "github.com/xhanio/framingo/pkg/types/api"
)

func (c *myClient) doJSON(ctx context.Context, method, path string, in, out any) error {
    resp, err := c.cli.Send(ctx, &client.Request{
        Method:      method,
        Path:        path,
        ContentType: echo.MIMEApplicationJSON,
        Body:        in,
    })
    defer resp.Body.Close()
    if err != nil {
        var eb *api.ErrorBody
        if stderrors.As(err, &eb) {
            // Recover the category from the wire's Kind so callers can use errors.Is.
            if cat := errors.LookupCategory(eb.Kind); cat != nil {
                return cat.Wrap(eb)
            }
            return eb // unknown category — preserve the structured payload
        }
        return errors.Wrap(err) // transport / non-API error
    }
    if out != nil {
        return json.NewDecoder(resp.Body).Decode(out)
    }
    return nil
}
```

Callers then get both checking idioms for free:

```go
// Category check — the Kind came from the server, not a client-side guess.
if errors.Is(err, errors.Conflict) {
    return nil // e.g. duplicate, swallow
}

// Structured access — full server payload.
var eb *api.ErrorBody
if stderrors.As(err, &eb) {
    log.Printf("backend status=%d kind=%s code=%s details=%v",
        eb.Status, eb.Kind, eb.Code, eb.Details)
}
```

Note `errors.As` is `stdlib`-only; alias the stdlib package (e.g. `stderrors "errors"`) when you also import `github.com/xhanio/errors`, since the two packages cannot share the bare `errors` name.

**Anti-patterns**:

- **Don't write `IsNotFound` / `IsConflict` wrappers** around `errors.As` + status compare. They add a per-client vocabulary without buying anything over the standard `errors.Is(err, errors.NotFound)` / `errors.As(err, &eb)` idioms.
- **Don't rebuild the category from `Status`** with a hardcoded `switch` (`case 404: return errors.NotFound.Wrap(err)`). The server already wrote the category name in `Kind`; `LookupCategory(eb.Kind)` is authoritative. Status-based reconstruction is cargo-cult and loses precision when multiple categories share a status (e.g. `Conflict` vs `AlreadyExist` both 409).
- **Don't reformat or drop `Message`** when wrapping. Servers often sanitize it for security; the durable signal is in `Status`/`Kind`/`Code`/`Details`. Preserve `*api.ErrorBody` (via `cat.Wrap(eb)` or by returning it directly) so callers can still read those fields via `errors.As`.
