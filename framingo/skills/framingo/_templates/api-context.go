// Package api defines this project's request-context wrapper around
// echo.Context and the glue that lets routers declare handlers as
// `func(c Context) error` instead of `func(c echo.Context) error`.
//
// COPY THIS FILE into your project as pkg/types/api/api.go, then change the
// package's module path in any imports. It is self-contained: it compiles
// with no project-specific types. See "Extending Context" at the bottom for
// how to add Credential()/Session() accessors over your own entity package.
//
// Why this layer exists:
//   - One context value satisfies both echo.Context (request/response,
//     binding) and context.Context (deadline/cancellation propagation into
//     services), so handlers can pass `c` straight through.
//   - Project-wide accessors and custom binders live here, so adding one
//     doesn't ripple through every handler signature.
//
// The framework's API server only accepts echo.HandlerFunc /
// func(echo.Context, *websocket.Conn) error. WrapHandler / WrapWebSocket /
// DiscoverHandlers bridge the richer signatures back to those at router
// registration time.
package api

import (
	"context"
	"reflect"
	"time"

	"github.com/coder/websocket"
	"github.com/labstack/echo/v4"

	fapi "github.com/xhanio/framingo/pkg/types/api"
)

// Context is the per-request value passed to router handlers. It embeds
// echo.Context (so handlers keep all of Echo's API) and context.Context (so
// handlers can hand `c` to any context-aware service call without unwrapping
// to c.Request().Context() first).
type Context interface {
	echo.Context
	context.Context

	// Thin helpers over Echo's parameter binders.
	BindQuery() *echo.ValueBinder
	BindPath() *echo.ValueBinder
	BindForm() *echo.ValueBinder
	BindAny(i any) error

	// TraceID returns the request's trace id if middleware set one.
	// Returns ("", false) when absent or the wrong type, so handlers
	// don't repeat the assertion dance.
	TraceID() (string, bool)
}

// ctx is the default Context implementation: an echo.Context with
// context.Context semantics layered on by deferring to the underlying
// *http.Request's context for Deadline/Done/Err, and routing Value lookups
// through echo's request-scoped Get store.
type ctx struct {
	echo.Context
}

func (c *ctx) Deadline() (time.Time, bool) {
	return c.Request().Context().Deadline()
}

func (c *ctx) Done() <-chan struct{} {
	return c.Request().Context().Done()
}

func (c *ctx) Err() error {
	return c.Request().Context().Err()
}

// Value routes string keys through echo's request-scoped Get store (that's
// where framingo's ContextKey* values land), and falls through to the
// underlying request context for everything else — so typed context keys set
// by stdlib/third-party middleware still resolve.
func (c *ctx) Value(key any) any {
	if k, ok := key.(string); ok {
		return c.Get(k)
	}
	return c.Request().Context().Value(key)
}

func (c *ctx) TraceID() (string, bool) {
	v := c.Get(fapi.ContextKeyTrace)
	if v == nil {
		return "", false
	}
	tid, ok := v.(string)
	return tid, ok
}

func (c *ctx) BindQuery() *echo.ValueBinder {
	return echo.QueryParamsBinder(c.Context)
}

func (c *ctx) BindPath() *echo.ValueBinder {
	return echo.PathParamsBinder(c.Context)
}

func (c *ctx) BindForm() *echo.ValueBinder {
	return echo.FormFieldBinder(c.Context)
}

func (c *ctx) BindAny(i any) error {
	return c.Context.Bind(i)
}

// HandlerFunc / WebSocketHandlerFunc are the recommended handler signatures
// for routers in this project. WrapHandler / WrapWebSocket adapt them into
// the echo signatures the framework's server registers.

type HandlerFunc func(Context) error

func WrapHandler(hf HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		return hf(&ctx{c})
	}
}

type WebSocketHandlerFunc func(Context, *websocket.Conn) error

func WrapWebSocket(wf WebSocketHandlerFunc) func(echo.Context, *websocket.Conn) error {
	return func(c echo.Context, conn *websocket.Conn) error {
		return wf(&ctx{c}, conn)
	}
}

// DiscoverHandlers reflects over r's methods and returns those matching a
// known handler signature, keyed by method name. It lets a router expose
//
//	func (r *router) Handlers() map[string]any { return api.DiscoverHandlers(r) }
//
// instead of hand-listing every method, and transparently wraps
// `func(Context) error` / `func(Context, *websocket.Conn) error` methods into
// the echo-compatible signatures the framework expects. Methods named
// "Handlers" are skipped so the router's own Handlers() doesn't recurse.
//
// Methods that match no known signature are skipped SILENTLY — log the
// returned count at startup so a mistyped handler is visible.
func DiscoverHandlers(r any) map[string]any {
	handlers := make(map[string]any)
	rv := reflect.ValueOf(r)
	rt := reflect.TypeOf(r)
	for i := 0; i < rt.NumMethod(); i++ {
		method := rt.Method(i)
		if method.Name == "Handlers" {
			continue
		}
		switch fn := rv.Method(i).Interface().(type) {
		case func(echo.Context) error:
			handlers[method.Name] = echo.HandlerFunc(fn)
		case func(Context) error:
			handlers[method.Name] = WrapHandler(fn)
		case func(echo.Context, *websocket.Conn) error:
			handlers[method.Name] = fn
		case func(Context, *websocket.Conn) error:
			handlers[method.Name] = WrapWebSocket(fn)
		}
	}
	return handlers
}

// --- Extending Context -------------------------------------------------
//
// Add project accessors by (1) adding the method to the Context interface
// above and (2) implementing it on *ctx. Nothing else changes — handler
// signatures stay `func(c api.Context) error`.
//
// Example, over your own pkg/types/entity package:
//
//	// in the Context interface:
//	Credential() (*entity.Credential, bool)
//
//	// implementation:
//	func (c *ctx) Credential() (*entity.Credential, bool) {
//	    v := c.Get(fapi.ContextKeyCredential)
//	    if v == nil {
//	        return nil, false
//	    }
//	    cred, ok := v.(*entity.Credential)
//	    return cred, ok
//	}
//
// The value is whatever your authentication middleware stored under
// fapi.ContextKeyCredential. Framingo defines the key; the type is yours.
// Session() over fapi.ContextKeySession follows the identical shape.
