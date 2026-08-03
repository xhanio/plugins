// COPY THIS FILE to pkg/routers/<domain>/router.go and rename the package.
// Wiring only — handler bodies go in handler.go, routes in router.yaml.
//
// Replace `myapp` with your module path and `order` with your domain.
package order

import (
	_ "embed"

	fapi "github.com/xhanio/framingo/pkg/types/api"
	"github.com/xhanio/framingo/pkg/types/common"
	"github.com/xhanio/framingo/pkg/utils/log"
	"github.com/xhanio/framingo/pkg/utils/nameutil"

	"myapp/pkg/types/api"
	"myapp/pkg/types/model"
)

// Compile-time check that *router satisfies the framework's Router contract.
var _ fapi.Router = (*router)(nil)

//go:embed router.yaml
var config []byte

// Unexported struct, exported factory returning the interface — the
// convention throughout framingo.
//
// NOTE the dependency type: `model.Order`, the narrow business contract from
// pkg/types/model — NOT `order.Manager` from pkg/services/order. Routers and
// services depend on types/model so the implementation stays package-private
// and nothing outside the service sees its lifecycle methods. Importing the
// service package here is the mistake this layer exists to prevent.
type router struct {
	name string
	log  log.Logger

	svc model.Order
}

func New(svc model.Order, log log.Logger) fapi.Router {
	return newRouter(svc, log)
}

// newRouter returns the concrete router, the form package tests construct.
// The name is fixed here at construction; Name() stays a plain getter.
func newRouter(svc model.Order, log log.Logger) *router {
	r := &router{
		svc: svc,
		log: log,
	}
	r.name = nameutil.Name(r) // project-relative, layout root stripped: routers/order/router
	return r
}

func (r *router) Name() string {
	return r.name
}

// Dependencies drives supervisor ordering: this router won't start before
// the services it lists here.
func (r *router) Dependencies() []common.Service {
	return []common.Service{
		r.svc,
	}
}

func (r *router) Config() []byte {
	return config
}

// Handlers is identical in every router package — do not hand-write the map.
// DiscoverHandlers reflects over this router's methods and keys them by
// method name, matching the `func:` values in router.yaml.
//
// Keep the debug log: methods whose signature doesn't match are skipped
// SILENTLY, so this count is the only startup signal that a handler was
// mistyped.
func (r *router) Handlers() map[string]any {
	handlers := api.DiscoverHandlers(r)
	r.log.Debugf("router %s parsed %d handler(s)", r.Name(), len(handlers))
	return handlers
}
