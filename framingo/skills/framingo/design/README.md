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
| [service-wiring.md](service-wiring.md) | Platform first in every `New`, and every platform handle always there, never nil; `Dependencies` lists optional ones as is, nil skipped; one instance per name; the api server starts last by ordering; a restart is invisible to the dependents, what `Init` replaces swapped whole |
| [lifecycle.md](lifecycle.md) | `Start`'s and `Stop`'s contexts bound their call alone, so `Stop` alone ends what a start runs; `Stop` waits with no deadline for what takes new work, so two starts never both take it; the rest a start began is left, ended hard, or waited for on the one resource it holds, by what it holds |
| [config.md](config.md) | A service reads what it can reload in `Init`, from the context, so a restart is the reload; what one `Init` reads is published whole, never field by field under a reader; the file is re-read on SIGHUP alone, services stopped first |
| [websockets.md](websockets.md) | `wsutil.Bridge` as a session utility; `wsutil.Close` by the handler's result; the server closes its connections on stop |
| [certificates.md](certificates.md) | A requester keeps its key and the request, not the CSR, decides its identity; one CA for the manager's certificates and one for agents'; CA keys in the database alone, their certificates from `GET /certificates/ca`; a download is encoded in the format and with the password the caller asks, nothing stored; each TLS side verifies the other against the CAs it is given, the server asking for a client certificate, the route requiring one |
| [checks.md](checks.md) | A decision a machine can check is checked, its message naming the design note: the lint config's import boundaries and forbidden calls, for the repository and as a template for apps |
| [migrations.md](migrations.md) | Locally the daemon migrates on start; in prod a Job runs `exampleapp migrate` once per release; every key overridable from the environment |

When a design question is settled, add it to the file of its area — or a
new file when no area fits — and a row above.
