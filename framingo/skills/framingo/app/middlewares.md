# Middlewares — Authoring `api.Middleware`

How to write a middleware and attach it. The server's side of the story —
the chain order, config resolution, the built-ins — is in
[api.md](../pkgs/api.md).

The example ships six under `pkg/middlewares/`, one package each, a
`middleware.go` per package (plus `option.go` where it takes options):

| Package | Does |
|---|---|
| `deflate` | Inflates compressed request bodies, with a decompression-bomb cap |
| `authnuser` | Session auth — sets `fapi.ContextKeyCredential` / `ContextKeySession` |
| `authnagent` | Client-certificate auth for agents (via the certificate service) |
| `authz` | Permission check against the route's declared `permission:` |
| `feature` | License gating |
| `throttle` | Per-client-IP rate limiting, configurable per route |

## The Contract

One interface, one method (`pkg/types/api`, alias `fapi`):

```go
type Middleware interface {
    common.Service
    // config is the raw YAML written under the middleware's name, nil when
    // there is none. Called once per attachment at registration - and again
    // on restart, when routes are rebuilt - so per-route state lives in the
    // returned closure. An error fails registration; returning no function
    // and no error declines the attachment, and the server skips it.
    Func(config []byte) (func(echo.HandlerFunc) echo.HandlerFunc, error)
}
```

The name comes from the package path — that's what router.yaml refers to —
and every middleware pins the contract at compile time:

```go
var _ fapi.Middleware = (*middleware)(nil)

func (m *middleware) Name() string {
	pkg, _ := reflectutil.Locate(m)
	return path.Base(pkg) // package name == middleware name
}
```

A middleware is a `common.Service`: one that leans on services takes them in
its constructor and declares them, exactly like a router —

```go
// middlewares/authz/middleware.go
type middleware struct {
	role role.Manager
}

func New(role role.Manager) fapi.Middleware {
	return &middleware{
		role: role,
	}
}

func (m *middleware) Dependencies() []common.Service {
	return []common.Service{m.role}
}
```

Middlewares sit at the api level beside routers, and the same layer rule
applies ([layout.md](layout.md)): they may call services — as `authz` calls
the role service — but never the repo level, `types/orm/`, or `db`.

## Config-Free Middleware

Most middlewares take no config. Refuse a block rather than ignore it — a
typo'd router.yaml block should fail startup, not silently do nothing.
From `authz`:

```go
// Func implements api.Middleware. The middleware takes no router.yaml config,
// so a block under its name is a mistake worth failing startup for.
func (m *middleware) Func(config []byte) (func(echo.HandlerFunc) echo.HandlerFunc, error) {
	if config != nil {
		return nil, errors.Newf("%s takes no config", m.Name())
	}
	return m.handle, nil
}

func (m *middleware) handle(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		// ... work, then
		return next(c)
	}
}
```

## Configured Middleware

A middleware that takes config unmarshals its own block — the framework
carries it as raw bytes and never interprets it. The block's shape lives as
an exported type in the project's `pkg/types/api` (`api.ThrottleConfig`, in
`types/api/middleware.go`). The example's `throttle` is the canonical case;
its package doc spells out the attachment forms:

```yaml
middlewares:
  - throttle            # bare: the group's entry, else the server's default
  - throttle:           # or this route's own limit, overriding both
      rps: 1
      burst_size: 3
```

```go
// Func builds the attachment for one route: its limit, and its own limiter
// table, live in the returned closure.
func (m *middleware) Func(raw []byte) (func(echo.HandlerFunc) echo.HandlerFunc, error) {
	var cfg api.ThrottleConfig
	if raw != nil {
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return nil, errors.Wrapf(err, "invalid throttle config")
		}
	}
	if cfg.RPS == 0 || cfg.BurstSize == 0 {
		// No limit for this route: pass everything, so routers can attach it
		// unconditionally and leave the limit to configuration.
		return func(next echo.HandlerFunc) echo.HandlerFunc { return next }, nil
	}
	var mu sync.RWMutex
	limits := make(map[string]*rate.Limiter) // per-attachment table, keyed by client IP
	return func(next echo.HandlerFunc) echo.HandlerFunc { /* ... */ }, nil
}
```

## Reading the Request

Route-attached middlewares run after the built-in Info middleware, so
`fapi.RequestInfo` is on the context — client IP, path, trace ID, and the
route's declared metadata flattened onto it (`Permission`, `Poll`). From
`authz`:

```go
// The Info middleware runs upstream and resolves the matched route's
// metadata - its declared permission among it - onto the request context.
req, ok := c.Get(fapi.ContextKeyRequestInfo).(*fapi.RequestInfo)
if !ok || req == nil {
	return errors.Forbidden.Newf("no handler info for request %s %s", c.Request().Method, c.Request().URL.EscapedPath())
}
required := req.Permission // declared as `permission:` in router.yaml
if required == "" {
	return next(c) // public endpoint
}
allowed, err := m.role.HasPermission(c.Request().Context(), cred.Role, required)
```

The permission names themselves are constants in the project's `types/rbac/`
([types.md](types.md)).

Server-level middlewares (`WithMiddlewares`) run before routing —
`RequestInfo` does not exist there; a middleware that reads it belongs in
router.yaml.

## Attaching

- **Route-scoped** (the normal case): register with
  `RegisterMiddlewares(mw)` *before* routers — the example's server
  component does this in `api.go`
  ([components-server.md](components-server.md)) — then reference by name
  in router.yaml at group or handler level, bare or with a config block.
  Config resolves handler > group > server default > nil; see
  [api.md](../pkgs/api.md) for the entry forms and the server middleware
  configs mapping.
- **Server-level** (must see every request, before routing): pass to
  `server.Add(name, server.WithMiddlewares(mw))`. CORS is the built-in
  occupant of that position; `cors` is the one name the server claims.

Registration order matters: a router.yaml name not yet registered fails
`RegisterRouters` with `middleware <name> not found`.
