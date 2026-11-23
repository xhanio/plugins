# Framingo Errors Reference

Full reference for `github.com/xhanio/errors` (v1.3): import, creating errors, wrapping, combining, absorbing a downstream error, checking, formatting, the category table, and custom categories.

**IMPORTANT**: All errors in framingo MUST use the `github.com/xhanio/errors` package. Do NOT use the standard `fmt.Errorf` or `errors.New` from the Go stdlib. The `xhanio/errors` package provides categorized errors with HTTP status codes, stack traces, and error wrapping — the API server's error handler relies on error categories to return correct HTTP responses.

## Import

```go
import "github.com/xhanio/errors"
```

## Creating Errors

**IMPORTANT**: Use `Newf()` to create errors with a message, NOT `New()`. The `New()` function takes functional `Option` arguments, not a message string. Using `New("some message")` will NOT compile.

```go
// Uncategorized errors (maps to 500 Internal Server Error)
errors.Newf("unsupported db type: %s", dbtype)

// Categorized errors (maps to specific HTTP status codes)
errors.BadRequest.Newf("page size %d over the limit of %d", size, limit)
errors.NotFound.Newf("user %s not found", id)
errors.Unauthorized.Newf("invalid token")
errors.Conflict.Newf("resource %s already exists", name)
errors.NotImplemented.Newf("handler %s not found", funcName)

// A bare category is itself an error (status only, no message)
return errors.Unauthorized

// New() takes options — a code and details for the API's ErrorBody among them
return errors.TooManyRequests.New(
    errors.WithMessage("you have been rate limited"),
    errors.WithCode("RATE_LIMIT", map[string]string{"ip": ip}), // -> ErrorBody.Code / .Details
)
```

Never `Newf("")`, `WithMessage("")` or a message-less `errors.New()`: the
error is non-nil but its `Error()` is empty, so a caller takes its error
branch and logs nothing. A status-only signal is the bare category. Never
`Wrapf(err, "")` either — it adds a layer no reader sees; `Wrap(err)` is the
call with nothing to add. `framingo vet` reports all four
([design/errors.md](../design/errors.md#an-error-says-what-failed)).

## Wrapping Errors

Wrap an error **where it enters from another package** — the standard
library, a driver, a client library: `errors.Wrap(err)` catches the stack
there, and a category's `Wrap` says what the failure means. An error from your
own code already carries its stack, so return it as it is, or `Wrapf` it when
you have something to add — which user, which step. `Wrap(err)` on it adds
nothing.

```go
// another package's error: wrap it where it enters, so its stack starts here
if err := m.db.FromContext(ctx).Create(record).Error; err != nil {
    return errors.Wrapf(err, "failed to create user %s", name)
}
if err := json.Unmarshal(data, &cfg); err != nil {
    return errors.Wrap(err)
}

// with a category: the outermost one is the error's
return errors.BadRequest.Wrap(err)
return errors.DBFailed.Wrapf(err, "query failed for user %s", id)

// your own code's error: as it is, or with what this level knows
if err := m.users.Get(ctx, id); err != nil {
    return err
}
if err := m.initConfig(); err != nil {
    return errors.Wrapf(err, "init %s", m.Name())
}
```

`errors.StackTrace` and `%+v` report the deepest stack in the chain, where the
failure began, through wrappers from any package; what a wrap adds is context,
not a stack.

What a client reads of a 5xx is only a message you wrote: a handler returning
`errors.Wrap(dbErr)` answers `Internal Server Error`, and the driver's text
stays in the server's log. Use `Wrapf` with a message where the client should
learn more than the status ([api.md](api.md#error-response-format)).

## Combining Multiple Errors

`Combine` reports that an operation failed **as a whole** for several reasons
(a request with several invalid fields). It returns nil for no reasons and the
reason itself for one. Several reasons resolve to the `Combined` category, a
500, even when they all share a category: declare what the failure is by
wrapping the result with a category, which wins over anything inside.

```go
return errors.BadRequest.Wrapf(errors.Combine(fieldErrs...), "invalid request") // 400

// unpack an undeclared combination
errors.Any(err, errors.NotFound) // at least one reason is NotFound
errors.All(err, errors.NotFound) // every reason is
for _, reason := range errors.ErrorsOf(err) {
    code, details := errors.CodeOf(reason)
    // ...
}
```

`errors.Is` with a sentinel and `errors.As` still search every reason. Don't
use `Combine` for partial failures, such as a batch in which some items
failed: it keeps no record of which item each reason belongs to, so return a
result per item instead.

## Absorbing a Downstream Error

A category travels with its error, so a `NotFound` from another service you
call becomes your own 404 when you return it. `Handled` puts a barrier in
front of it — `Is`, `As`, categories and codes do not see past it, the stack
trace does — so you say what the failure means to you:

```go
return errors.Unavailable.Wrapf(errors.Handled(err), "call users service") // 503
```

## Checking Errors

```go
// Check the category: an error resolves to ONE, the outermost one set in its chain
if errors.Is(err, errors.NotFound) { /* ... */ }

// Any other target is the stdlib search of the tree (sentinel errors)
if errors.Is(err, ErrConnection) { /* ... */ }

// Recover a concrete type — the package re-exports the stdlib's As
var eb *api.ErrorBody
if errors.As(err, &eb) { /* ... */ }

// A sentinel switch matches by Is, case by case
switch {
case errors.Is(err, gorm.ErrRecordNotFound):
    return errors.NotFound.Wrapf(err, "user %s not found", id)
default:
    return errors.DBFailed.Wrap(err)
}

// WRONG — == and switch err compare identity, which one wrap breaks
if err == io.EOF { /* ... */ }
switch err { case gorm.ErrRecordNotFound: /* ... */ }
if e, ok := err.(errors.Error); ok { /* ... */ } // lost behind any fmt.Errorf("%w")

// Read the category, the code and details, the message your code wrote
status := errors.CategoryOf(err).StatusCode()
code, details := errors.CodeOf(err)
msg := errors.MessageOf(err) // "" when your code wrote none: never another package's text

// Whether a category was declared, rather than Internal assumed
if !errors.Categorized(err) { /* ... */ }
```

`CategoryOf` and `CodeOf` walk through wrappers from any package, so a
category survives `fmt.Errorf("%w")`; don't assert `err.(errors.Error)`,
which fails as soon as anything wraps the error. errorlint reports an error
matched by `==`, `!=`, a `switch` on its value or a type assertion, tests
included. A category is no error value: `errors.CategoryOf(err) ==
errors.NotFound`, or a `switch` on it, is fine. An error without a category
resolves to `Internal`, and a `context.Canceled` / `context.DeadlineExceeded`
anywhere in the chain to `Canceled` / `DeadlineExceeded`, so return a
context's error wrapped as it is rather than recategorising it.

`MessageOf` is the message for a reader outside the server: unlike
`Error.Message()` and `%m`, it never falls back to the text of the error the
chain wraps — `errors.Wrap(dbErr).Message()` is the driver's message,
`errors.MessageOf(errors.Wrap(dbErr))` is `""`. `Categorized` tells a declared
category, `Internal` included, from the `Internal` `CategoryOf` assumes; the
API server uses it to honour echo's own status where nothing declared one
([api.md](api.md#error-response-format)).

`errors.Has` is the package's deprecated spelling of the sentinel search; use
`Is`. Finding a category by its name is `errors.LookupCategory(name)`, which
is how a client recovers the category from an `ErrorBody`'s `Kind`
([api.md](api.md#consuming-the-server-with-pkgservicesapiclient)).

## Formatting

`%s` and `%v` print the message chain only, so an error reads like any other
in a log line. `%+v` adds the code and details before it and the stack trace
after it — each reason's too, for a combination. Log with `%+v` where the
stack is wanted: the server's own fault, reported once, where nothing else
will. The API server does for a 500, and for every failed request at debug
level; a 503 or 504 it logs with `%s`.

## Available Error Categories

| Category | HTTP Status | Use For |
|---|---|---|
| `BadRequest` | 400 | Invalid input, malformed requests |
| `InvalidArgument` | 400 | Invalid function arguments |
| `Unauthorized` | 401 | Missing or invalid authentication |
| `Forbidden` | 403 | Authenticated but not authorized |
| `PermissionDenied` | 403 | Insufficient permissions |
| `NotFound` | 404 | Resource not found |
| `Conflict` | 409 | Resource already exists, concurrent modification |
| `AlreadyExist` | 409 | Duplicate resource |
| `TooManyRequests` | 429 | Rate limit exceeded |
| `ResourceExhausted` | 429 | Out of resources (gRPC `RESOURCE_EXHAUSTED`) |
| `Canceled` | 499 | Operation canceled; `context.Canceled` resolves to it |
| `Internal` | 500 | Unexpected internal errors; what an uncategorised error resolves to |
| `DBFailed` | 500 | Database operation failures |
| `Combined` | 500 | Several reasons no wrapper declared (see [Combining](#combining-multiple-errors)) |
| `NotImplemented` | 501 | Unimplemented functionality |
| `Unavailable` | 503 | Service unavailable |
| `DeadlineExceeded` | 504 | Timeout; `context.DeadlineExceeded` resolves to it |

`Cancaled` is a deprecated alias of `Canceled`, the same category.
`LookupCategory("Cancelled")` finds `Canceled` too: it is the `Kind` a server
on errors before v1.2 sends.

The API server turns a handler's error into the response by this table —
status from the category the error resolves to, `Kind` from its name
([api.md](api.md#error-response-format)).

## Custom Categories

```go
var ErrPaymentFailed = errors.NewCategory("PaymentFailed", 402) // also registers the name for LookupCategory

// Then use like built-in categories:
return ErrPaymentFailed.Newf("charge declined for order %s", orderID)
```
