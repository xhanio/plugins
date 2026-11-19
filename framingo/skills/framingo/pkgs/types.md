# Framework Types — `pkg/types/common`, `fapi`, `model`, `entity`, `info`, ORM Base

What framingo's own `pkg/types/` tree provides. The five `types/`
subdirectories a project keeps are the app half — [types.md](../app/types.md).

- `pkg/types/common` — the service/lifecycle interfaces and the identity trio `Named`/`Unique`/`Weighted` ([supervisor.md](supervisor.md)), message interfaces, context keys, `Pair`, and shared constants (`TimeFormat`, `RedactMask`).
- `pkg/types/api` (always imported aliased `fapi`) — `Router`, `Middleware`, `RequestInfo`/`ResponseInfo`, `Stats`, `Endpoint`, TLS types (`ServerTLS`, `ClientTLS`), `ErrorBody`, `WrapError`, `StatusCode`, `MethodAny`/`MethodWS`, and the `ContextKey*`/`HeaderKey*` constants. **No `Context`** — the handler context is project-owned ([routers.md](../app/routers.md)).
- `pkg/types/model` — framework service interfaces, what consumers hold: `Supervisor` ([supervisor.md](supervisor.md)), `Database` and `DatabaseDebugger` ([db.md](db.md)), `Task` and `Plan` ([plan.md](plan.md)), `Pubsub`, `MessageBus` and `Messenger` ([pubsub.md](pubsub.md)), `Tracer` and `Meter` (the explicit otel handles — framingo never uses the otel globals; [tracing.md](tracing.md), [metrics.md](metrics.md)).
- `pkg/types/entity` — framework entities: `SupervisorStats` and the supervisor's policy structs, `Task`/`TaskStats`, `Plan`/`PlanRun`/`PlanStats` and the `PlanRunStarted`/`PlanRunFinished` events, `PubsubMessage`.
- `pkg/types/info` — the build facts gopro injects with `-ldflags` (`ApplicationName`/`ApplicationVersion`, `ProductName`, `ProjectPath`, git and build fields); `GetBuildInfo()` returns them as a `Build`, `Version()` joins the product and build versions with the build type. `nameutil.Name` reads `ProjectPath`, the example's env prefix `ProductName`, the telemetry resource the application pair.
- `pkg/types/orm` — the ORM base interfaces below.

## ORM Base Types

Located in `pkg/types/orm`. No framework service consumes them yet: they are kept on purpose for a planned config cache fed by change data capture. Project `types/orm/` records may declare them, as the example's do with `var _ form.Record[int32] = User{}` ([types.md](../app/types.md)); nothing in the db manager requires it.

```go
const NoVersion int64 = 0

type Record[T comparable] interface {
    GetID() T
    GetErased() bool
    GetVersion() int64
    TableName() string
}

// For referential integrity tracking
type Referenced[T comparable] interface {
    References() []Reference[T]
}

type Reference[T comparable] struct {
    TableName string
    ID        T
}

func (r Reference[T]) Key() string // "<table>/<id>"
```

## Context Keys

Defined in `pkg/types/common/context.go`:
- `_config` - the `confutil.Config` the supervisor hands each `Init` (`confutil.WrapContext`/`FromContext`)
- `_tx` - Database transaction (`*gorm.DB`, `db.WrapContext`)
- `_db` - Database reference
- `_logger` - Logger instance
- `_trace` - Trace context
- `_credential`, `_session`, `_namespace` - Auth context
- `_api_request_info`, `_api_response_info`, `_api_error` - API context

## Message Interfaces

`Message`, `MessageHandler`, `RawMessageHandler`, `MessageSender`,
`RawMessageSender` live in `pkg/types/common` too — signatures and usage in
[pubsub.md](pubsub.md).
