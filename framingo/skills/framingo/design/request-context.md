# Request values: the request's context or echo's store

## What downstream reads goes in the request's context; the HTTP layer's own in echo's store

**Decision.** A middleware puts a value in the request's context —
`fapi.SetContext(c, ctx)` — when it is settled before the handler runs and
what reads it holds a `context.Context`: the trace span, the credential and
session authentication found, a transaction. It puts a value in echo's store
— `c.Set` — when it is the HTTP layer's own bookkeeping, or is written while
or after the handler runs: the request and response info, the API error, the
trace id the response carries, the credential the login handler sets.

`api.Context`'s `Value` answers a string key from echo's store first, then
from the request's context, so a reader through it — `entity.CredentialFrom(c)`
— finds either. A middleware holds an `echo.Context`, not an `api.Context`:
it reads the request's context, `entity.CredentialFrom(c.Request().Context())`,
and `c.Get` for the HTTP layer's values. A value has one writer helper beside
its reader, `entity.WithCredential` beside `entity.CredentialFrom`.

**Why.** echo pools its contexts: once the handler returns, the store is
reset for another request, while the request's context is a value nothing
reuses. A goroutine the request started — database/sql's, or work a service
detached with `context.WithoutCancel(c.Request().Context())` — may still read
it, and a value in it reaches every `context.Context` the request hands on,
where one in the store reaches only what holds the `echo.Context`. A value an
outer middleware reads after `next` returns — the response's status, its
error — goes where the inner layers write it, the store they share: a
request the handler swapped is seen neither by the outer layers' `r` nor by
the handler's own `c`.

**Cost.** Request values live in two places, and a reader holding an
`echo.Context` must know which. framingo's keys are plain strings, shared
by the two, so a string key in the request's context is answered only where
the store does not hold it, and each writer carries a `//nolint:staticcheck`
for SA1029.

## `api.Context` answers from the request's context as the handler got it

**Decision.** `WrapContext` reads `c.Request().Context()` once, when the
handler is called — after every middleware — and `Deadline`, `Done`, `Err`
and `Value`'s request half answer from it. A handler that needs another
context derives one and passes it on; it does not swap `c`'s request.

**Why.** database/sql asks `Done` and `Err` from a goroutine of its own once
a query's rows close, which may be after the handler returned and echo
reset its pooled `echo.Context` for the next request. Read through on every
call, the answer was the next request's, and a data race.

**Cost.** A request the handler swaps with `c.SetRequest` is not seen by its
own `c`. The echo half — `Param`, `Bind`, a string key's `Value` — still
must not outlive the handler: work that does takes what it needs as
arguments.
