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

// THIS FILE -> pkg/types/model/order.go

package model

import (
	"context"

	"github.com/xhanio/framingo/pkg/types/common"

	"myapp/pkg/types/entity"
)

// Order is the business contract. It embeds common.Service so a consumer can
// list it in Dependencies() — but exposes NO lifecycle methods, so a router
// holding a model.Order cannot start or stop it.
type Order interface {
	common.Service
	GetOrder(ctx context.Context, id string) (*entity.Order, error)
	CreateOrder(ctx context.Context, item string, qty int) (*entity.Order, error)
}
