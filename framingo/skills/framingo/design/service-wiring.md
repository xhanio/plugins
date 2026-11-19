# Wiring services

## The platform comes first

**Decision.** Every example service's `New` takes the platform first —
`New(platform platform.Platform, ...)` — whether it uses it or not
(`_ platform.Platform` in `newManager` then). The platform bundles the
framework plumbing a service may reach: database, meter, pubsub, messagebus.

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

**Why.** A filter in every `Dependencies()` (type assertions, `if x != nil`)
was boilerplate that could be forgotten. Listing an optional source also
orders it: a wired LDAP source is initialized and started before auth,
which it was not while left out.

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
