# Data access: orm stays in the repository

**Decision.** Three type categories meet at the data layer, each with one
owner:

| Category | Owner | Crosses the repo boundary? |
|---|---|---|
| `types/orm` — GORM rows | `services/repository/` only | never |
| `types/repo` — the data-access contracts, and inputs only the repository needs (`repo.UserCreateOptions`, which carries `PasswordHash`) | the contracts | yes: services call them |
| `types/entity` — the shared vocabulary | everyone | yes: what the repository returns |

The repository converts rows to entities before they leave it, and decides
nothing: hashing a password, refusing to delete a role still held, which
organization a new user joins are the services' rules. The service builds
the repository's input — the hash included — and the repository stores what
it is handed. Transactions are the repository's (`Transaction(ctx, fn)`),
so a service opens one without touching the database.

**Why.** With orm types out of the services, the schema can change without
touching business code, and a business rule cannot hide in a query. A
repo-specific input type (rather than an orm struct built in the service)
keeps the service off the storage shape while still letting it hand over
what only it computes.

**Cost.** A conversion per type (`repository/convert.go`) and a second
options type where the service's and the repository's inputs differ.

Tables that fit the framework's `orm.Record` declare it beside their own
struct — imported as `form`, since the project's package is `orm` too
(`var _ form.Record[int32] = User{}`); a table that does not fit simply
doesn't (`orm.HelloWorld`, without `BaseModel`'s getters) — there is no
central list.
