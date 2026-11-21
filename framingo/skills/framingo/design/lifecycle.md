# Lifecycle: what a start waits for, and what a stop may leave behind

## Stop alone ends what a start runs

**Decision.** `Start`'s context bounds the start and `Stop`'s the stop, and
neither outlives its call. What a start runs keeps `Start`'s context's
values but never its cancel or deadline —
`context.WithCancel(context.WithoutCancel(ctx))`, which `syncutil.Loop` does
for its loop — so it lives until `Stop` ends it.

The supervisor hands each `Start` the context its own `Start` was given, a
restart's the monitor's, and `WithStartPolicy(timeout)` bounds each one: a
`Start` that waits on something waits on that context.

**Why.** Two ways to end the same work were one too many: some daemons ended
when `Start`'s context did and others ignored it, and nothing said which a
caller could count on. With the cancel cut, the two contexts mean the same
thing, a bound on one call, so a start deadline ends a start that hangs and
never a service that runs. A restart's `Start` stuck on the monitor's
context ends when the supervisor stops, instead of holding the stop.

**Cost.** The deadline reaches a `Start` only through its context: one that
blocks on something deaf to it still holds the supervisor's start, and no
supervisor can end a goroutine from outside. Leaving it behind would let it
run beside the next start, so the contract asks `Start` to honor its
context instead.

## A service starts once what it depends on runs

**Decision.** The supervisor starts a service only when its own `Init`
succeeded and every dependency it lists is running — initialized, started,
not stopped, and neither its start nor a `Reconcile` since has failed. A
dependency that failed to start gates its dependents, through
one with no `Start` of its own, with "dependency X is not running", as a
failed `Init` fails its dependents fast; a service that does not depend on
it starts. What a pass left unstarted is the monitor's to restart, in
dependency order once the dependency runs, and a restart leaves a service as
it is while a dependency is not running, not counting the attempt against
it. A failed start still starts the supervisor's own loop, so its `Stop`
stops what did start; whether a failed start ends the process is the
caller's to decide.

**Why.** `Dependencies` orders the start so that a service finds what it
needs running. Starting it beside a dependency that failed sets it taking
work it cannot serve, and the start and the init gate disagreed on what a
dependency is for. A restart that could not hold only spent the service's
budget, for a fault that was not its own.

**Cost.** One service's failed start keeps everything that depends on it
down until the monitor restarts it, and with no monitor until a restart by
hand.

## Whatever takes new work has returned before Stop does

**Decision.** `Stop` waits, with no deadline, for what takes new work — a
run loop, a consumer, run by a `syncutil.Loop` — and that returns as soon as
its context is canceled. Anything slow it hands to work counted in a
`syncutil.WaitGroup`, which `Stop` waits for only until its own `ctx` ends.

**Why.** The supervisor restarts a service as `Stop`, `Init` and `Start` on
the same instance. Were the taker waited for only until the deadline, a
forced stop would let the next start's taker run beside it, and the two
would take the same work twice.

## The rest is the work's to decide

**Decision.** Whether work a start began may outlive a forced stop depends
on what it holds:

- **Nothing the next start needs:** it ends on its own. It takes its
  start's context and channels as arguments, never the manager's, and the
  next start goes ahead beside it — the example's `m.wg.Go`, the
  messagebus's handlers.
- **What can be cut:** it is ended hard once `ctx` ends — the api server
  closes the connections still open, the task manager sends its executions
  `job.SignalKill`.
- **What one holder at a time may have:** the next start goes ahead, and
  only what needs that resource waits for the old holder. A job still
  running from before a forced stop holds back its own next execution, not
  the task manager's start.

**Why.** No one answer fits every kind of work: waiting for all of it lets
one stuck job block every restart, and leaving all of it lets two holders of
one resource overlap. The wait goes where the exclusivity is — on the
resource, at the moment it is taken — never on `Start` as a whole.
