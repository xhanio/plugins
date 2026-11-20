# Lifecycle: what a stop may leave behind

## Stop alone ends what a start runs

**Decision.** `Start`'s context bounds the start and `Stop`'s the stop, and
neither outlives its call. What a start runs keeps `Start`'s context's
values but never its cancel or deadline —
`context.WithCancel(context.WithoutCancel(ctx))`, which `syncutil.Loop` does
for its loop — so it lives until `Stop` ends it.

**Why.** Two ways to end the same work were one too many: some daemons ended
when `Start`'s context did and others ignored it, and nothing said which a
caller could count on. With the cancel cut, the two contexts mean the same
thing, a bound on one call, and a start deadline the supervisor might give
some day could not end a running service.

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
