# Checks: what a machine holds a decision to

## A decision a machine can check is checked

**Decision.** Where a design decision can be checked mechanically it is, by
the cheapest means that can, and the check's message names the design note it
enforces. What cannot be — "the repository decides nothing" — stays with
review, and is said to. The first means is a golangci-lint config: the
repository's own `.golangci.yml`, and `_templates/golangci.yml` for an app
built on the layout. What is not Go source takes a test: `router.yaml`
against its handlers.

| Check | Holds | Decided in |
|---|---|---|
| depguard `routers` | a router or a middleware imports no service package | [narrow-contracts.md](narrow-contracts.md) |
| depguard `services` | services do not import each other: each holds `types/model` interfaces, `model.Platform` among them | [narrow-contracts.md](narrow-contracts.md) |
| depguard `orm` | `types/orm` is imported by the repository alone | [data-access.md](data-access.md) |
| depguard `errors`, forbidigo `fmt.Errorf` | errors come from `xhanio/errors`, which categorizes and wraps | — |
| forbidigo `otel.Get*`/`Set*` | no otel globals: a handle is passed | — |
| errcheck | a file's own `Close` is checked: a write the OS deferred fails there | — |
| `server.CheckRouter`, in each router's test | every `func:` in `router.yaml` is a discovered handler whose signature fits its method; no route twice | [who-decides.md](who-decides.md) |
| the example's `apitest.CheckRouter` | a declared `permission:` is one `rbac` defines | [who-decides.md](who-decides.md) |

The boundary rules bind non-test files: a service's test builds the real
repository, and migrates `types/orm` tables to do it, as a middleware's
builds the real service it checks against.

**Why.** A rule only a reviewer holds is held until the review that misses
it. The config costs nothing to run and catches at the import line what a
reviewer would catch a refactor later. Naming the note in the message tells
whoever trips a rule why it is there, not only that it is.

**Cost.** depguard names packages by full path, so an app edits its module
path into the template; and a rule keyed on the layout's directories checks
only code laid out that way.

## One command gives the verdict

**Decision.** `framingo verify` formats, builds, vets, lints and tests each
module it is given, runs every step even after one fails, and ends with
`verified` or the list of what failed, exiting non-zero. The skill tells an
agent to run it after any change. The command is the framework module's own
root package — `go install github.com/xhanio/framingo@latest` — so it is
versioned with the framework and its rules, by the one tag.

**Why.** Five commands are four chances to skip one, and a coding agent
needs a single answer to "is this change sound" that it cannot talk itself
past. A command, not a script copied into each app: a copy stays at the
version it was copied at, and a shell script runs on no Windows. It only
runs standard tools — gofmt, go, golangci-lint — so an editor and CI read
the same results without it.

**Cost.** The module's `go.mod` may carry no `replace` or `exclude`, which
`go install` refuses; and the framework takes cobra as a dependency.

## Not yet

What needs types rather than import paths — `Start` keeping its context,
`Dependencies` missing a held service, a `*gorm.DB` kept in a field — and
the docs' snippets against the code. The repository's `ROADMAP.md` plans both.
