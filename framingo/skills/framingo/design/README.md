# Design Decisions

The reasoning behind framingo's and the example's shapes: each file is one
area the project met design questions in — what was chosen, why, and what
it costs. The `app/` and `pkgs/` references say *how* things are done;
these say *why*, so a change can tell a principle from an accident.

| File | In short |
|---|---|
| [who-decides.md](who-decides.md) | The route decides who may (path + permission, authz fails closed); the service keeps the rules about its own data and does not ask who calls; the handler orchestrates checks that span two services; a failed `validate` tag is a 400 |
| [narrow-contracts.md](narrow-contracts.md) | Every dependency is the smallest interface: a service's own `Repository`, `common.RawMessageSender` over the bus, message handlers as `Named` |
| [data-access.md](data-access.md) | orm types never leave the repository; `types/repo` carries the contracts and repo-only inputs; the repository decides nothing |
| [service-wiring.md](service-wiring.md) | Platform first in every `New`, and every platform handle always there, never nil; `Dependencies` lists optional ones as is, nil skipped; one instance per name; the api server starts last by ordering; a restart is invisible to the dependents, what `Init` replaces swapped whole, and a dependent `Reconcile`s what it registered with a dependency that stopped or started alone |
| [lifecycle.md](lifecycle.md) | `Start`'s and `Stop`'s contexts bound their call alone, so `Stop` alone ends what a start runs, and the supervisor bounds each `Start`; a service starts once its `Init` succeeded and its dependencies run; `Stop` waits with no deadline for what takes new work, so two starts never both take it; the rest a start began is left, ended hard, or waited for on the one resource it holds, by what it holds |
| [config.md](config.md) | A setting is construction, fixed at `New`, or reloadable, its option the default of a key `Init` re-reads, and `option.go` says which; a service reads what it can reload in `Init`, from the context, so a restart is the reload; what one `Init` reads is published whole, never field by field under a reader; the file is re-read on SIGHUP alone, services stopped first |
| [websockets.md](websockets.md) | `wsutil.Bridge` as a session utility, ending one that lost its subscription, shaped by options — filter, frame, inbound, done — and `ReadFirst` for a handshake; `wsutil.Close` by the handler's result, a `CloseWith` its status and reason; the server closes its connections on stop |
| [certificates.md](certificates.md) | A requester keeps its key and the request, not the CSR, decides its identity; one CA for the manager's certificates and one for agents'; CA keys in the database alone, their certificates from `GET /certificates/ca`; a download is encoded in the format and with the password the caller asks, nothing stored; each TLS side verifies the other against the CAs it is given, the server asking for a client certificate, the route requiring one |
| [request-context.md](request-context.md) | A middleware puts what downstream reads through a `context.Context`, settled before the handler, in the request's context (`fapi.SetContext`), and the HTTP layer's own bookkeeping, or what the handler writes, in echo's store; `api.Context` answers from the request's context as the handler got it, so a goroutine the request left reads it after the handler returned |
| [errors.md](errors.md) | Errors come from `xhanio/errors`, wrapped where they enter from another package, our own returned as they are; matched by `Is`/`As`, never `==` or an assertion; a category declared once, outermost, a context's error kept as it is, echo's own status honoured where none was declared; another service's error absorbed with `Handled`; a `Combine` declared; never an empty message; a client reads only what the server wrote, a 5xx's message only one its code wrote; a stack logged where an error ends as the server's fault |
| [checks.md](checks.md) | A decision a machine can check is checked, its message naming the design note: the lint config's import boundaries and forbidden calls, for the repository and as a template for apps; `framingo vet`'s analyzers; tests for router config, docs and templates; an exception carries its reason; `framingo verify` gives one verdict |
| [comments.md](comments.md) | A comment says what the code cannot — a doc what a caller may count on, a body comment why, an option its key and built-in default — points at a design note instead of holding its reasoning, and says how it is, never how it was |
| [migrations.md](migrations.md) | Locally the daemon migrates on start; in prod a Job runs `exampleapp migrate` once per release; every key overridable from the environment |

When a design question is settled, add it to the file of its area — or a
new file when no area fits — and a row above.
