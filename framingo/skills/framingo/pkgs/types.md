# Framework Types — `pkg/types/common`, `fapi`, ORM Base

What framingo's own `pkg/types/` tree provides. The five `types/`
subdirectories a project keeps are the app half — [types.md](../app/types.md).

- `pkg/types/common` — the service/lifecycle interfaces and the identity trio `Named`/`Unique`/`Weighted` ([supervisor.md](supervisor.md)), message interfaces, context keys, `Pair`, and shared constants (`TimeFormat`, `RedactMask`).
- `pkg/types/api` (always imported aliased `fapi`) — `Router`, `Middleware`, `RequestInfo`, `Endpoint`, TLS types, `ErrorBody`, `WrapError`, `ContextKey*`. **No `Context`** — the handler context is project-owned ([routers.md](../app/routers.md)).
- `pkg/types/model` — framework service interfaces: `Supervisor`, `Database`, `Pubsub`, `MessageBus`, `Planner`.
- `pkg/types/entity` — framework entities (supervisor stats, task/plan records).
- `pkg/types/orm` — the ORM base interfaces below.

## ORM Base Types

Located in `pkg/types/orm` — consumed by project `types/orm/` records ([types.md](../app/types.md)):

```go
// Records must implement this generic interface
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
```

## Context Keys

Defined in `pkg/types/common/context.go`:
- `_config` - Viper config instance
- `_tx` - Database transaction (`*gorm.DB`)
- `_db` - Database reference
- `_logger` - Logger instance
- `_trace` - Trace context
- `_credential`, `_session`, `_namespace` - Auth context
- `_api_request_info`, `_api_response_info`, `_api_error` - API context

## Message Interfaces

`Message`, `MessageHandler`, `RawMessageHandler`, `MessageSender`,
`RawMessageSender` live in `pkg/types/common` too — signatures and usage in
[pubsub.md](pubsub.md).
