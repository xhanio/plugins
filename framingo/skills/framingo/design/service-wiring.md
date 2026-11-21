# Wiring services

## The platform comes first

**Decision.** Every example service's `New` takes the platform first —
`New(platform model.Platform, ...)` — whether it uses it or not
(`_ model.Platform` in `newManager` then). The platform bundles the
framework plumbing a service may reach: database, meter, pubsub, messagebus.
`Platform` is a `types/model` contract like any other a service consumes;
`services/system/platform` only builds one, for the wiring and for tests, so
no service imports a service package, the platform's included.

**Why.** One shape for every constructor; a service that later needs the
meter or the bus takes it from what it already holds, and no call site
changes.

## The platform is infrastructure: never nil

**Decision.** Every handle the platform carries - database, meter, pubsub,
messagebus - is always there. The wiring passes each one to `platform.New`;
none is ever nil, so a service calls through them without checking. An app
that wants no metrics still wires the metrics service, disabled.

**Why.** A nil handle costs nothing when wired and panics far from the
cause, inside whichever service first calls through it; nil checks spread
through every consumer to prevent that would be boilerplate one forgets.
Infrastructure that is simply always present needs neither.

**Cost.** An app without a component drops the getter from its bundle
instead of passing nil; a test passes a fake for what the code under test
reaches.

## Dependencies lists what it holds, nil included

**Decision.** `Dependencies()` returns the services the manager holds, as
they are: `[]common.Service{m.repository, m.mb}`, and the auth service's
optional LDAP and API-token sources too. Platform handles are never nil -
the platform is infrastructure - but an optional source the app did not
wire is, and the supervisor skips a nil entry everywhere it walks
`Dependencies` — registering, the init gate, the health monitor.

The meter is the one handle held and not listed. A dependency gates `Init`
and rolls its health up, and telemetry never gates a service: recording
works whatever the collector does, before the metrics service starts as
after. Being always there and being depended on are two things. The
`depslist` analyzer holds the rule, and the meter's field carries
`//framingo:ignore depslist` with that reason ([checks.md](checks.md)).

**Why.** A filter in every `Dependencies()` (type assertions, `if x != nil`)
was boilerplate that could be forgotten. Listing an optional source also
orders it: a wired LDAP source is initialized and started before auth,
which it was not while left out.

## A restart is invisible to the dependents

**Decision.** A dependent keeps its dependency's handle and asks it each
time, never keeping what the handle hands out: the repository calls
`db.FromContext(ctx)` for every query and stores no `*gorm.DB`. The
supervisor restarts the failing service alone, never its dependents. So
whatever a restart's `Init` replaces is swapped under the readers whole:
`db.Manager`'s pool is one value behind an `atomic.Pointer`, and the
configuration `Init` reloads is published whole ([config.md](config.md)).

**Why.** A restart runs while requests are in flight. A dependent keeping
what the handle handed out goes on using a pool the restart closed, and a
field rewritten under a reader is a data race. Restarting the dependents too
would drop their work for a fault that was not theirs.

**Cost.** A query that took the old pool just before the swap may find it
closed and fail once. Tests restart a service under load, under `-race`:
`TestReInitUnderQueries` for the database, `TestReInitUnderRequests` for the
example service.

## A name stands for one instance

**Decision.** `Supervisor.Register` returns a `Conflict` for a second
instance under a name already taken; the same instance again is a no-op.
Registration is wiring, so the error fails startup.

**Why.** Stats, the dependency graph and by-name operations all key on the
name; a second instance under it would never run.

## The api server starts last by ordering, not by depending

**Decision.** The api server declares no dependencies; the wiring registers
it after `TopoSort`, so it starts after every service.

**Why.** A dependency gates `Init` and rolls health up: depending on the
services would keep the listener — the health listener with it — down
whenever one of them failed, while `/healthz` and `/readyz` should report
exactly that.
