// Every service splits its interface across TWO files. Getting this wrong is
// the most common structural mistake in a framingo project, because both
// halves are called some variation of "the service interface".
//
//   pkg/types/model/order.go     — the BUSINESS contract. What consumers see.
//   pkg/services/order/model.go  — the service's Manager: business + lifecycle.
//                                  What the supervisor wiring sees.
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
)

// Manager = the business contract + whichever lifecycle interfaces this
// service actually implements. Compose only what you need: a service with no
// background goroutine should not claim common.Daemon.
type Manager interface {
	// business.go
	model.Order
	// lifecycle.go
	common.Initializable
	common.Daemon
	common.Debuggable
}
