# Who decides what: routes, handlers, services

Three places can refuse a request. Each refuses for one kind of reason, and
the example keeps them apart.

## The route decides whether a caller may use an operation

**Decision.** Authorization is the route's: each handler in `router.yaml`
names a `permission:`, and the `authz` middleware checks the caller's role
against it. It **fails closed** — a handler under authz that names no
permission is refused, so every handler declares one or opts out with
`authz: false` (`PUT /user/passwords`, which a caller whose password must be
reset still has to reach).

**Why.** A permission is a fact about the route, and checking it once, in
one middleware, keeps it out of every handler and service. Failing closed
means a forgotten `permission:` locks a route instead of opening it.

## Whose account is the path's question, not the service's

**Decision.** An operation on *any* account and one on *the caller's own*
are different routes with different permissions: `/users/:id`
(`local_user_manage_*`) and `/user` (`local_user_read`/`write`). The `/user`
handlers resolve the account from the credential (`LocalUserID`); the
service is only ever handed an ID. Likewise *reset* (`PUT
/users/:id/passwords`, no old password — an administrator's act) and
*change* (`PUT /user/passwords`, the old one required) are two service
methods, `ResetPassword` and `ChangePassword`, not one with an `isOwn` flag.

**Why.** "Is this your own account?" was a branch in every handler and a
flag on the service; as two routes it is the path plus the permission,
which authz already checks. The service does not ask who is calling, so a
CLI or a test drives it with nothing but IDs.

**Cost.** Rules that only made sense per caller go. Through `/users/:id`, a
caller holding `local_user_manage_*` may delete or demote their own account,
and resets their own password without the old one — the permission says
they manage accounts. The built-in admin stays protected by the service's
own rules below.

## A service keeps the rules about its own data

**Decision.** What no caller may lift lives in the service: the built-in
admin is neither deleted nor demoted, a password changes only on the old
one, the admin role is neither modified, deleted nor given a permission
list, a permission name must be one rbac defines.

**Why.** Wherever the service is called from, these hold; a handler that
forgot one would open a hole.

## A check that spans two services is the handler's

**Decision.** Giving a user a role requires the role to exist. The user
router takes `model.User` and `model.Role`, asks the role service
(`rm.GetByName`), and only then calls the user service — which holds no
role service.

```go
// pkg/routers/user/handler.go
if _, err := r.rm.GetByName(c, body.Role); err != nil {
	return errors.Wrap(err)
}
user, err := r.um.Create(c, opts)
```

**Why.** That a role exists is the role service's fact, not a rule about
users. Putting two services in order for one use case is orchestration —
the handler's job. Had the user service called the role service, it would
gain a peer dependency (wiring order, `Dependencies()`, test setup) for a
check that is not about users. *Composing* is different: a service whose own
function is built on another's takes it as a dependency, as auth is built on
`model.UserAuthN`.

**Cost.** A caller that bypasses the router has to make the check itself;
the example has none. Should every path have to hold such an invariant,
enforce it in the schema — a foreign key from `users.role` to `roles.name` —
rather than wire the services together.

## Bad input is a 400

**Decision.** The server's validator reports a failed `validate` tag as
`InvalidArgument`, which the error handler turns into a 400. Validating
something that is not a struct stays uncategorized — a 500 — because that
is the handler's bug, not the caller's.

**Why.** An uncategorized error is a 500, which blamed the server for a
missing required field in the caller's request.

See also [app/layout.md](../app/layout.md) (the layer rules) and
[app/routers.md](../app/routers.md) (handlers).
