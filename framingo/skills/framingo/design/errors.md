# Errors: what an error carries, and who reads it

An error from `github.com/xhanio/errors` (v1.3) carries a category, which
decides the status the server answers with; maybe a code and details, for
programs; messages, for people; and a stack, for whoever diagnoses it. Each
decision below is about who reads which part. How to call the package:
[pkgs/errors.md](../pkgs/errors.md).

## Errors come from xhanio/errors, wrapped where they enter

**Decision.** Every error is made and wrapped with `xhanio/errors`: `Newf`,
`Wrap`/`Wrapf`, a category's `Newf`/`Wrap`. Not `fmt.Errorf`, not the
standard `errors` — tests aside, for their stub errors. An error is wrapped
where it enters from another package — the standard library, a driver, a
client library — by the first of the server's functions to get it. One from
the server's own code is returned as it is, or `Wrapf`'d with what the level
returning it knows: which user, which step.

**Why.** The category decides the response's status, and the stack is caught
where the failure began: an error from another package carries neither until
it is wrapped. One of ours already carries both. `StackTrace` and `%+v` report
the deepest stack in the chain, through any wrapper, so another wrap adds no
stack — `Wrap(err)` returns an error that has one as it is — and a rule that
every return wraps only makes call sites noisier. No check holds it either:
the line between "from another package" and "ours" is the reviewer's.

**Cost.** A helper that passes another package's error up unwrapped moves its
stack to the caller that wraps it, one frame off. And a library's error comes
back wrapped, so whatever reads it matches it by `Is` and `As`, never by
identity — the next decision.

## An error matches by Is and As, never by ==

**Decision.** An error is tested with `errors.Is` — against a sentinel, or a
category — and `errors.As` for a concrete type: never `==`, `!=` or a
`switch err` on its value, never a type assertion or a type switch. A sentinel
switch reads `switch { case errors.Is(err, gorm.ErrRecordNotFound): }`. A
category is no error value: `errors.CategoryOf(err) == errors.NotFound`, or a
`switch` on it, is fine. A category or a code is read with `CategoryOf` and
`CodeOf`, never off an asserted `errors.Error`.

**Why.** An error stops being `==` to itself the moment anything wraps it, and
`xhanio/errors` does not promise to return the error it was given: wrapping a
package-level sentinel catches a fresh stack, in a new layer. `switch err {
case gorm.ErrDuplicatedKey: }` passes its tests, then quietly takes the default
branch once a plugin or a helper wraps the driver's error — the example's
repositories did this. An assertion fails the same way, and an
`err.(errors.Error)` is lost behind any `fmt.Errorf("%w")`, where
`CategoryOf` and `CodeOf` walk through.

**Cost.** Code that looks at an error's outermost value on purpose says so:
`fapi.WrapError` passes an `*api.ErrorBody` through only when the handler
returned it as is — wrapped, it may be another service's — and
`fapi.PublicMessage` says nothing past a bare category's name; each carries a
`//nolint:errorlint` with that reason.

## A category is declared once, outermost

**Decision.** The outermost category in the chain is the error's, so the code
that knows what a failure means declares it: the repository turns
`gorm.ErrRecordNotFound` into `NotFound`, a handler a bind failure into
`BadRequest`. A context's error is wrapped as it is, `errors.Wrap(ctx.Err())`:
it resolves to `Canceled` (499) or `DeadlineExceeded` (504) by itself.

Where no category was declared (`errors.Categorized`), echo's own verdict on
the request being served — an `*echo.HTTPError` anywhere in the chain, a
failed `c.Bind`'s 400 — answers with its code; a declared category, `Internal`
included, wins over it.

**Why.** A bare `errors.Canceled` returned when `ctx` ended — `ioutil` did —
called a deadline a cancellation and lost `context.DeadlineExceeded` from the
chain, so `errors.Is` found neither. And `errors.Wrap(c.Bind(&req))` answered
500 for a client's malformed body: `CategoryOf` alone cannot tell the
`Internal` it assumes from one declared. Only a classification made for this
request is honoured: another service's status is absorbed (next decision).

## What another service failed with is absorbed, not passed on

**Decision.** An error from a service you call — a framingo client's
`*api.ErrorBody`, its category recovered from `Kind` — is handled where it is
met, then absorbed with `errors.Handled` under the category it means to you:
`errors.Unavailable.Wrapf(errors.Handled(err), "call users service")`.

**Why.** A category travels with its error: returned as it came, the users
service's 404 becomes yours, and says your resource is missing. `Handled` hides
the error from `Is`, `As`, categories and codes, and keeps it for the stack
trace and `%+v`, so the log still tells the whole story.

**Cost.** Past `Handled` the body's fields are out of reach: read its `Code`
and `Details` first.

## A combined error is declared

**Decision.** `Combine` is for an operation that failed as a whole for several
reasons, and whoever combines declares what the failure is:
`errors.BadRequest.Wrapf(errors.Combine(fieldErrs...), "invalid request")`.
Undeclared, it is `Combined`, a 500. Where the reasons are about alike items,
each names its item. A batch whose items fail one by one returns a result per
item instead.

**Why.** Several `BadRequest`s are not a `BadRequest` until someone says so:
`Combined` is an archive judged only once unpacked, with `Any`, `All` or
`ErrorsOf`. The framework's own combinations — the supervisor's passes and
probes, the server's `Stop` — are whole operations whose reasons name their
service, with the per-service result in `Stats`.

## An error says what failed

**Decision.** Every error has a message: never `Newf("")`, `Wrapf(err, "")`,
`WithMessage("")` or a bare `errors.New()`. With nothing to add, the call is
`Wrap(err)`; a status alone is the bare category, `return errors.Unauthorized`.

**Why.** `Newf("")` is not nil, so its caller takes the error branch, and logs
nothing; inside a `Combine` it leaves a stray `"; "`. `Wrapf(err, "")` adds a
layer no reader sees.

## A client reads what the server wrote, never what it wrapped

**Decision.** A response carries the category's status and name, the code and
details, and one message: the outermost one the server's own code wrote
(`errors.MessageOf`), as `Wrapf(err, "failed to load user")` does, never the
chain. Where the code wrote none, a 5xx says its status text, and a 4xx the
text it received — an echo error's message, a failed validation's fields
(`fapi.PublicMessage`). A WebSocket's internal-error close reason follows the
same rule. `Origin` keeps the whole chain for the log.

**Why.** `Error.Message()` falls back to the wrapped error's text when no layer
wrote one, so `errors.Wrap(dbErr)` — the wrapping the first decision asks for
— answered `pq: password authentication failed for user "app"
(host=db.internal:5432)`. A 4xx is the caller's to fix, and a binding or
validation failure says how.

**Cost.** A 5xx a client should learn something from takes a `Wrapf`.
`MessageOf` keeps out the text the server received, not the text it wrote:
`Wrapf(err, "connect to %s", dsn)` goes out as written.

## A stack is logged where an error ends as the server's fault

**Decision.** `%s` and `%v` print messages; `%+v` adds the code, the details
and the stack. An error is logged with `%+v` where it is reported for the last
time and is the server's own fault: a request answered 500 (every failed
request at debug level), a WebSocket handler's unexpected error, a subscriber's
error the message bus ends, the daemon's exit. Elsewhere `%s` or `%v`: a 503 or
504, a peer gone, network I/O, bad external input, an OS error, anything in a
probe or retry loop. Production binaries build with `-trimpath`.

**Why.** A stack locates a fault in the server's code, and once is enough. On a
dependency down or a deadline passed it adds kilobytes and nothing else, and an
outage repeats it on every request. Without `-trimpath` the frames carry the
build machine's paths.

**Cost.** A 503 with a bug behind it logs no stack outside debug level.

## What holds these

errorlint (`comparison`, `asserts`) reports an error matched by `==`, a
`switch` or an assertion; depguard and forbidigo the standard `errors` and
`fmt.Errorf`; `framingo vet`'s `errmsg` an error given no message;
staticcheck's deprecation check `errors.Cancaled` and `errors.Has`
([checks.md](checks.md)). Declaring a combination, absorbing a downstream
error, keeping a context's error as it is and choosing `%+v` stay with review.
