// COPY THIS FILE to pkg/routers/<domain>/handler.go — same package as
// router.go. Handler bodies only; no wiring, no Handlers() map.
//
// Every handler is `func(c api.Context) error` where api is YOUR project's
// pkg/types/api (see api-context.go), never echo.Context and never
// framingo's pkg/types/api (which is aliased fapi and has no Context type).
//
// The method name is what router.yaml's `func:` refers to. EVERY `func:` in
// router.yaml must have a matching method here — a name with no method (or a
// method whose signature DiscoverHandlers doesn't recognise) fails router
// registration at startup with:
//
//	handler function <Name> not found in router.Handlers()
package order

import (
	"net/http"

	"github.com/coder/websocket"
	"github.com/xhanio/errors"

	"myapp/pkg/types/api"
)

// GetOrder handles `method: GET, path: /:id, func: GetOrder` in router.yaml.
func (r *router) GetOrder(c api.Context) error {
	id := c.Param("id")
	if id == "" {
		return errors.BadRequest.Newf("missing order id")
	}
	// c IS a context.Context — pass it straight to the service. No
	// c.Request().Context() unwrap, and cancellation propagates for free.
	order, err := r.svc.GetOrder(c, id)
	if err != nil {
		// Always wrap. The server's error middleware maps the xhanio/errors
		// category to the HTTP status, so returning errors.NotFound from the
		// service yields a 404 here without any status plumbing.
		return errors.Wrap(err)
	}
	return c.JSON(http.StatusOK, order)
}

// CreateOrder shows request binding + validation.
func (r *router) CreateOrder(c api.Context) error {
	var req api.CreateOrderRequest // your DTO, defined in pkg/types/api
	if err := c.BindAny(&req); err != nil {
		return errors.BadRequest.Newf("invalid request: %v", err)
	}
	if err := c.Validate(&req); err != nil {
		return errors.Wrap(err)
	}
	created, err := r.svc.CreateOrder(c, req.Item, req.Quantity)
	if err != nil {
		return errors.Wrap(err)
	}
	return c.JSON(http.StatusCreated, created)
}

// Health backs the `poll: true` route — same plain signature, the polling
// behaviour is declared in router.yaml, not in the handler.
func (r *router) Health(c api.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// Events backs `method: WS`. WebSocket handlers take the extra *websocket.Conn
// (github.com/coder/websocket, not gorilla). The middleware stack still runs
// before the upgrade; once upgraded, returning nil closes the connection
// normally and returning an error closes it with StatusInternalError.
func (r *router) Events(c api.Context, conn *websocket.Conn) error {
	return conn.Write(c, websocket.MessageText, []byte(`{"hello":"world"}`))
}
