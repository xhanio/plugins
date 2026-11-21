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
`Dependencies` — registering, the init and start gates, the health
monitor, the reconciles.

The meter is the one handle held and not listed. A dependency gates `Init`
and `Start` and rolls its health up, and telemetry never gates a service: recording
works whatever the collector does, before the metrics service starts as
after. Being always there and being depended on are two things. The
`depslist` analyzer holds the rule — a held service listed, the list not
filtered — and the meter's field carries
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
What a handle hands out to be kept stays the same across its restart, the
restart swapping what is under it: the metrics service's provider outlives
its exporter, and the tracing service's provider follows the pipeline its
`Init` rebuilds, so a library that keeps its `Tracer` survives the restart.

What a dependent registered with a dependency — a subscription, a task, a
plan — is
the dependent's to put back, since the dependency's state lasts no longer
than its run: a stop forgets it. A dependent that registers is a
`common.Reconcilable`, and when the supervisor stops or starts the
dependency alone it calls `Reconcile(ctx, dep, state)` on each such
dependent that runs on — `DependencyStopped` once the stop is done,
`DependencyRunning` or `DependencyFailed` once the start is — and the
dependent brings what it holds there in line: the plan manager adds its
tasks again, the messagebus subscribes its modules again.

**Why.** A restart runs while requests are in flight. A dependent keeping
what the handle handed out goes on using a pool the restart closed, and a
field rewritten under a reader is a data race. Restarting the dependents too
would drop their work for a fault that was not theirs. The dependency's own
state is the dependency's, and what was registered with it the dependent's:
the one that holds a thing is the one that keeps it. Reconcile is a direct
call, not an event on a bus: it is sent through no transport the restart
may have taken down, is never dropped, and every dependent is in line before
the restart is done. It names a state, never a moment — a stop is told once
it is done, not while it is coming — so a dependent acts on what the
dependency is, and a call that finds nothing to do changes nothing.

**Cost.** A query that took the old pool just before the swap may find it
closed and fail once, and a call to a restarting dependency fails until the
dependent has reconciled. The rule is held by review, `gormfield` aside: a check
for every value a dependent keeps would follow it through helpers and into
goroutines, and would have to tell a handout that may be kept by the
handle's promise, not by its type. Tests restart a service under load, under
`-race` — `TestReInitUnderQueries` for the database, `TestReInitUnderRequests`
for the example service — and a dependency under its dependents:
`TestATaskManagerRestartUnderThePlans`, `TestAPubsubRestartUnderTheMessageBus`.

## A name stands for one instance

**Decision.** `Supervisor.Register` returns a `Conflict` for a second
instance under a name already taken; the same instance again is a no-op.
Registration is wiring, so the error fails startup.

**Why.** Stats, the dependency graph and by-name operations all key on the
name; a second instance under it would never run.

## The api server starts last by ordering, not by depending

**Decision.** The api server declares no dependencies; the wiring registers
it after `TopoSort`, so it starts after every service.

**Why.** A dependency gates `Init` and `Start` and rolls health up:
depending on the services would keep the listener — the health listener with it — down
whenever one of them failed, while `/healthz` and `/readyz` should report
exactly that.
