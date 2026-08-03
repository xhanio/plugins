# Middlewares — Authoring `api.Middleware`

How to write a middleware and attach it. The server's side of the story —
the chain order, config resolution, the built-ins — is in
[api.md](../pkgs/api.md).

The example ships seven under `pkg/middlewares/`, one package each, a
`middleware.go` per package (plus `option.go` where it takes options):

| Package | Does |
|---|---|
| `cors` | Answers cross-origin requests; server-level, since preflights match no route |
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
    // enabled is the resolved switch: booleans written under the middleware's
    // name are switches, not config, most specific first (handler > group >
    // server mapping). With no boolean anywhere, a route attachment is enabled
    // by its router.yaml entry's presence; a server-level one by having an
    // entry in the server's middleware configs. Standard first line:
    // `if !enabled { return nil, nil }`.
    //
    // config is the most specific non-boolean YAML under the middleware's
    // name, nil when there is none - switch and config resolve independently.
    // Called once per attachment at registration - and again on restart, when
    // routes are rebuilt - so per-route state lives in the returned closure.
    // An error fails registration; returning no function and no error
    // declines the attachment, and the server skips it.
    Func(enabled bool, config []byte) (func(echo.HandlerFunc) echo.HandlerFunc, error)
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
func (m *middleware) Func(enabled bool, config []byte) (func(echo.HandlerFunc) echo.HandlerFunc, error) {
	if !enabled {
		return nil, nil
	}
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
carries config as raw bytes and never interprets it (booleans are the one
exception: they are switches, split off into `enabled` before `Func` runs).
The block's shape lives as an exported type in the project's `pkg/types/api`
(`api.ThrottleConfig`, in `types/api/middleware.go`). The example's
`throttle` is the canonical case; its package doc spells out the attachment
forms:

```yaml
middlewares:
  - throttle            # bare: the group's entry, else the server's default
  - throttle: false     # or switched off for this route - Func sees enabled=false
  - throttle:           # or this route's own limit, overriding both
      rps: 1
      burst_size: 3
```

```go
// Func builds the attachment for one route: its limit, and its own limiter
// table, live in the returned closure.
func (m *middleware) Func(enabled bool, raw []byte) (func(echo.HandlerFunc) echo.HandlerFunc, error) {
	if !enabled {
		return nil, nil
	}
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
- **Switching**: a boolean under the middleware's name is a switch, not
  config — it arrives as `Func`'s `enabled` parameter, so the standard
  guard makes any middleware opt-out-able: a handler's `- authz: false`
  frees that one route from the group's auth, a handler's `- name: true`
  forces it back on under a group's `false` (most specific wins both
  ways), and `name: false` in the server's mapping switches every bare
  attachment off. Switch and config resolve independently — a disabled
  entry still inherits the group's block, unused.
- **Server-level** (must see every request, before routing): pass to
  `server.Add(name, server.WithMiddlewares(mw))` — the roster and its
  order are code; the server's middleware configs activate each entry
  (`name:` or `name: true` enables, a block enables and configures, no
  entry leaves it dormant). The example's `cors` lives here: preflight
  `OPTIONS` match no route, so only this position can answer them, and
  the health listener — whose config has no cors entry — serves without
  it.

Registration order matters: a router.yaml name not yet registered fails
`RegisterRouters` with `middleware <name> not found`.
