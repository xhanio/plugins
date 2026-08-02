# Framingo Errors Reference

Full reference for `github.com/xhanio/errors`: import, creating errors, wrapping, combining, checking, the category table, and custom categories.

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
errors.BadRequest.Newf("invalid request: %v", err)
errors.NotFound.Newf("user %s not found", id)
errors.Unauthorized.Newf("invalid token")
errors.Conflict.Newf("resource %s already exists", name)
errors.NotImplemented.Newf("handler %s not found", funcName)

// Category as bare sentinel (no message needed) — this is the ONLY valid use of New()
return errors.NotImplemented.New()
```

## Wrapping Errors

**IMPORTANT**: ALWAYS use `errors.Wrap(err)` or `errors.Wrapf(err, msg, ...)` when returning errors from called functions. NEVER return a raw `err` directly — this loses the stack trace. Every error must be wrapped to maintain the full call chain for debugging.

```go
// CORRECT — wrap to maintain error stack
if err := m.initConfig(); err != nil {
    return errors.Wrap(err)
}

// CORRECT — wrap with additional context message when helpful
if err := m.db.FromContext(ctx).Create(record).Error; err != nil {
    return errors.Wrapf(err, "failed to create user %s", name)
}

// CORRECT — wrap with a category (overrides the wrapped error's category)
return errors.BadRequest.Wrap(err)
return errors.DBFailed.Wrapf(err, "query failed for user %s", id)

// WRONG — never return raw err, stack trace is lost
if err := doSomething(); err != nil {
    return err  // DO NOT DO THIS
}
```

## Combining Multiple Errors

```go
// Combine multiple errors (uses uber/multierr under the hood)
return errors.Combine(errs...)
```

## Checking Errors

```go
// Check if error belongs to a category
if errors.Is(err, errors.NotFound) { /* ... */ }

// Check if error wraps a specific cause
if errors.Has(err, ErrConnection) { /* ... */ }
```

## Available Error Categories

| Category | HTTP Status | Use For |
|---|---|---|
| `BadRequest` | 400 | Invalid input, malformed requests |
| `InvalidArgument` | 400 | Invalid function arguments |
| `Unauthorized` | 401 | Missing or invalid authentication |
| `Forbidden` | 403 | Authenticated but not authorized |
| `PermissionDenied` | 403 | Insufficient permissions |
| `NotFound` | 404 | Resource not found |
| `DeadlineExceeded` | 408 | Timeout |
| `Conflict` | 409 | Resource already exists, concurrent modification |
| `AlreadyExist` | 409 | Duplicate resource |
| `TooManyRequests` | 429 | Rate limit exceeded |
| `Cancaled` | 499 | Operation cancelled |
| `Internal` | 500 | Unexpected internal errors |
| `NotImplemented` | 501 | Unimplemented functionality |
| `Unavailable` | 503 | Service unavailable |
| `ResourceExhausted` | 503 | Out of resources |
| `DBFailed` | 500 | Database operation failures |

## Custom Categories

```go
var ErrPaymentFailed = errors.NewCategory("PaymentFailed", 402)

// Then use like built-in categories:
return ErrPaymentFailed.Newf("charge declined for order %s", orderID)
```
