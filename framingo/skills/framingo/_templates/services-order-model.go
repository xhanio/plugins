// Every service splits its interface across TWO files. Getting this wrong is
// the most common structural mistake in a framingo project, because both
// halves are called some variation of "the service interface".
//
//   pkg/types/model/order.go     — the BUSINESS contract. What consumers see.
//   pkg/services/order/model.go  — the service's Manager: business + lifecycle,
//                                  and its Repository. What the wiring sees.
//
// Routers and other services depend on model.Order. Only pkg/components/server
// (the wiring) touches order.Manager. That's what keeps the implementation
// package-private and stops services importing each other directly.
//

// THIS FILE -> pkg/services/order/model.go

package order

import (
	"github.com/xhanio/framingo/pkg/types/common"

	"myapp/pkg/types/model"
	"myapp/pkg/types/repo"
)

// Manager = the business contract + whichever lifecycle interfaces this
// service actually implements. Compose only what you need: a service with no
// background goroutine should not claim common.Daemon.
type Manager interface {
	model.Order
	common.Initializable
	common.Daemon
	common.Debuggable
}

// Repository is the slice of the shared repository this service may touch:
// the repo.* contracts it reads and writes through. The one repository
// instance implements them all and the wiring hands it over as is —
// New(platform, repo, opts...) — so this interface, not the instance, says
// what the service may touch. Never the database itself: only
// pkg/services/repository holds it.
type Repository interface {
	repo.Order
}
