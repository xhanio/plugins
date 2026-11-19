# Plan manager — Scheduled and Ad-Hoc Task Execution

`pkg/services/plan` runs plans — described tasks with optional schedules —
and reports the runs of its predefined kinds through the message bus.
`plan.Manager` = `model.Plan` + `Daemon` + `Debuggable`.

## Interfaces

```go
// pkg/types/model
type Plan interface {
    common.Service
    // Keeps a copy of the plan; an ID already added is a Conflict.
    Add(plan *entity.Plan) error
    // Runs the plan now; returns the run's ID.
    // source goes on the run and on its retries and requeues; empty is an InvalidArgument.
    // A plan already queued, running or being requeued is a Conflict.
    // The run always ends with PlanRunFinished; see "Run events".
    Trigger(id string, source entity.PlanRunSource) (string, error)
    // Ends what the plan is doing, a run still queued included, and keeps the plan;
    // an idle plan is a Conflict.
    Cancel(id string) error
    // Ends what the plan is doing, forgets the plan and returns its latest run.
    Delete(ctx context.Context, id string) (*entity.PlanRun, error)
    // The latest run; nil if there is none yet.
    LastRun(id string) (*entity.PlanRun, error)
    // Opens a run's log by its PlanRun.LogPath; see "Run logs".
    OpenRunLog(path string, tailBytes int64) (io.ReadCloser, error)
    Stats(opts entity.PlanStatsOptions) ([]*entity.PlanStats, error)
}

// pkg/types/entity
type Plan struct {
    ID          string
    Metadata    labels.Set // k8s.io label set
    Description string
    Kind        string     // a predefined kind, from which the plan manager builds the Task's Job
    Params      any        // what the kind takes: a jobs.Command for a command plan
    Task        *Task      // entity.Task - what actually runs
}

type PlanStatsOptions struct {
    Selector string // label selector over the plans' Metadata; empty is every plan
    All      bool   // also the plans whose job succeeded
}
```

A plan needs an `ID`, and `Add` keeps a copy of it and its `Task`: the caller's stay as they
were. The `Task` decides when a plan runs: `Schedule` for cron, or `Manual: true` to run it
only when `Trigger` does. It takes one of the two, since a plan stays until `Delete`; a job
to run once goes on the task manager itself. `Timeout`, `RetryAttempts`, `RequeueAttempts`
and the rest apply as they do in the task manager, `pkg/services/task`. How a stopped plan
ends is its job's: a command plan's `Command.Grace`, see [Stopping a plan](#stopping-a-plan).

## Construction

```go
import (
    "github.com/xhanio/framingo/pkg/services/plan"
    "github.com/xhanio/framingo/pkg/services/task"
)

// tm runs the plans. It is yours: set the concurrency there (task.MaxConcurrency, 10 by
// default) and share it with tasks of your own if you like. The plan manager takes it as a
// model.Task. Cron schedules are read in time.Local unless task.WithLocation(loc) says
// otherwise - pass what osutil.Timezone() returns to follow the system's named setting.
tm := task.New(task.WithLogger(logger))

// the message sender: where run events are sent - typically the messagebus manager
// (any common.MessageSender), or nil. The plan manager depends on it and on tm.
pm := plan.New(messagebusMgr, tm,
    plan.WithLogger(logger),
    plan.WithLogDir("/var/log/myapp/plans"), // empty keeps no log files
    plan.WithOutputTailBytes(4096),          // the default; <= 0 keeps it
    // plan.WithName(name) as on every service
)
```

Register both with the supervisor like any service. It starts `tm` before the plan manager
and stops it after; `tm.Stop(ctx)` waits for every execution it started until `ctx` ends, then
sends what still runs `job.SignalKill` and returns. The plan manager neither starts nor stops `tm`: stopping the plan manager ends
and forgets its own plans, and whatever else runs on `tm` carries on. Plan IDs share a
namespace with the other tasks on `tm`, and a clash is the `Conflict` from `Add`.

## The task manager

`pkg/services/task` is what runs every plan, and runs jobs of your own as well. `task.Manager`
= `model.Task` + `Daemon`; its options are `WithLogger`, `WithLocation` and `MaxConcurrency`.

```go
// pkg/types/model
type Task interface {
    common.Service
    Add(tasks ...*entity.Task) error                  // all or none: no Job is InvalidArgument, a taken ID a Conflict
    Remove(ctx context.Context, tasks ...*entity.Task) error // by job, not by ID; waits until ctx ends, then SignalKill
    Trigger(id string, params any) error              // queue it now; params replace Task.Params for this firing, nil keeps them
    Cancel(id string) error                           // ends it queued, running, waiting to retry or requeueing; idle is a Conflict
    Stats(id string) *entity.TaskStats                // nil if not registered
}
```

A task's ID is its job's. `Schedule` (cron, [`cronutil`](utils.md) grammar) or `Manual: true`
keeps it registered; with neither it is one-shot, queued at once and gone after it runs —
both is an `InvalidArgument`. The queue runs lower `Priority` first, then first come first
served; `Exclusive` waits for every slot and runs alone. `Timeout` bounds each attempt, and
`RetryAttempts` / `RetryDelay` retry it in place, the delay doubling after each retry;
`RequeueAttempts` / `RequeueCooldown` put a failed run
back in the queue after a cooldown, freeing its slot meanwhile, so a task runs up to
`(RetryAttempts+1) * (RequeueAttempts+1)` times per firing. A firing that finds the task
already queued, running or cooling down is a `Conflict` from `Trigger`, and a cron tick that
does is skipped.

## Command plans

Every plan has a kind, and the plan manager builds its task's `Job` from it, under the plan's
ID: a `Task` with a `Job` of its own is an `InvalidArgument`. A job of your own goes on the
task manager itself, as in [Commands and job signals](#commands-and-job-signals).
`entity.PlanKindCommand` is the one predefined kind, defined in `pkg/utils/plans`:

```go
err := pm.Add(&entity.Plan{
    ID:     "backup",
    Kind:   entity.PlanKindCommand,
    Params: jobs.Command{Argv: []string{"/usr/bin/pg_dump", "mydb"}, Dir: "/tmp"},
    Task: &entity.Task{
        Schedule: "30 2 * * *",
        Timeout:  time.Hour,
    },
})
runID, err := pm.Trigger("backup", entity.PlanRunManual) // run it now as well
```

A plan of a kind takes what the kind needs in `Plan.Params` and leaves `Task.Params` empty:
the plan manager passes each firing's run ID and source there. A command plan's job is its
`jobs.Command` (see below) with the run's bookkeeping around it. Leave the command's `Out`
unset — a command plan with an `Out` is an `InvalidArgument`: the plan manager sets it for
each run. The command runs in its own process group, so stopping it reaches
its children, and one that ends on its own keeps any background children it left. Its
output goes to `<log dir>/<plan ID>/<YYYYMMDD-HHMMSS>-<first 8 of the run ID>.log`, under a
header naming the run, its source and the command line, and the tail of it into the run. A
run whose log file cannot be created still runs, without a `LogPath`.

## Commands and job signals

`jobs.Command` is a predefined job function in `pkg/utils/jobs`, and a command does not need
a plan manager to run on a task manager:

```go
backup := jobs.Command{
    Argv:  []string{"/usr/bin/pg_dump", "mydb"},
    Grace: 30 * time.Second, // how long it gets to stop before a SIGKILL; 10s by default
    Out:   logWriter,        // nil discards the output
}
err := tm.Add(&entity.Task{Job: job.New("backup", backup.Run), Schedule: "30 2 * * *"})
```

`Command` defines what runs and its `Run` is the job function; `job.New` takes a copy, so
the definition is fixed there. Run decides how the command stops: the job's context
ending, by a cancel or a timeout, sends the process group a SIGTERM, then a SIGKILL once
`Grace` is up; `job.SignalKill` sends the SIGKILL at once. Whatever a stopped command left
in its group goes with it.

Run returns nil when the command ended on its own with status 0, and otherwise a
`*jobs.CommandError` that says what actually happened: the exit code, whether it
was stopped and by what, whether it was killed. It unwraps to what stopped the command, so
`errors.Is(err, context.Canceled)` works, and `errors.As` finds it through wrapping. A job
function with bookkeeping of its own around the command calls `Run` on a copy with the
`Out` it wants for that execution:

```go
func(jc job.Context) error {
    c := cmd // a copy; cmd is the shared definition
    c.Out = openThisRunsLog()
    err := c.Run(jc)
    var failed *jobs.CommandError
    if errors.As(err, &failed) {
        // failed.ExitCode, failed.Stopped, failed.Killed ...
    }
    return err
}
```

A command plan takes its `Command` from `Plan.Params` when it is added, and its `Grace` says
when the command gets its SIGKILL. Neither the plan manager nor the task manager waits by it: how
long a stop waits is the caller's context.

`job.SignalKill` is one of a job's signals. `Job.Signal(sig)` sends a signal to the
execution going on now, and a job's function reads `jc.Signaled(sig)`, a channel closed
once the signal has arrived; what a signal means is up to the function. A job with its own
function that wants a forced stop to cut its cleanup short should watch for it.

## Stopping a plan

`Cancel` ends what a plan is doing, a run still queued included, and keeps the plan.
`Delete(ctx, id)` also forgets it, removing its cron entry first so no new run starts
meanwhile, waits for its run until `ctx` ends, and returns its latest run. `Stop(ctx)` does
that to every plan at once.

- Canceling a command SIGTERMs its process group, and the command SIGKILLs the group once
  its `Grace` is up, so a command plan ends within its grace period whatever `ctx` allows.
- Once `ctx` ends, what still runs is sent `SignalKill` and the error wraps `ctx.Err()`:
  the run returned may then still be `running`, and its end comes as `PlanRunFinished`. An
  ended `ctx` makes it a forced delete; to bound the wait, give `Delete` a `ctx` with a
  deadline.
- Go cannot kill a goroutine, so a job that ignores its context and signals goes on until
  it returns, whatever `Delete` did.
- A process that left the group, with `setsid` say, is out of reach. If it holds the
  command's output, the end is waited for no longer than the grace period.
- Stopping several plans, `Delete` them concurrently, so every cron entry goes at once and
  the wait lasts as long as the slowest rather than the sum. `Stop` does the same.
- To escalate while a `Delete` waits, cancel its `ctx`: that forces it.

Under systemd, set `KillMode=mixed` on the unit. With the default `control-group`,
systemd's SIGTERM reaches the commands as well as your process: `Setpgid` gives a command
a process group of its own, not a cgroup. A command that dies of that signal before your
service stops it ended on its own as far as the plan manager can tell, so its run is recorded
as `failed`.

## Run events

Every run of a command plan is reported through the `MessageSender`, with an
`entity.PlanRun` carrying its ID, plan ID, source, state, times, exit code, output tail,
log path and error. The source is `schedule` for a cron run and otherwise what `Trigger`
was given: `entity.PlanRunManual`, `entity.PlanRunSchedule`, or a value of your own.

| Event | `Kind()` | When |
|---|---|---|
| `entity.PlanRunStarted` | `plan.run.started` | before the command starts |
| `entity.PlanRunFinished` | `plan.run.finished` | after it ends: `succeeded`, `failed` or `canceled` |

A retry or a requeue is a new run with a new ID. The ID `Trigger` returns always ends with
a `PlanRunFinished`: a run that a `Cancel`, `Delete` or `Stop` drops while it is still queued
is reported by that event alone, `canceled`, with the time of the `Trigger` as `StartedAt`
and no log. Consume the events with a `MessageHandler` on the bus ([pubsub.md](pubsub.md)):

```go
func (m *manager) HandleMessage(ctx context.Context, msg common.Message) error {
    switch ev := msg.(type) {
    case entity.PlanRunStarted:
        // ev.PlanRun.ID, ev.PlanRun.Source ...
    case entity.PlanRunFinished:
        // ev.PlanRun.State, ev.PlanRun.ExitCode, ev.PlanRun.OutputTail ...
    }
    return nil
}
```

A bus can lose a message: one whose subscriber's queue is full, or one still queued
as the process stops. When every run must be accounted for, also compare your records
with `LastRun` from time to time, which holds the plan's latest `PlanRun` while the
process lives, and apply the run `Delete` returns as you shut down. Make applying a run
idempotent, since these paths all deliver the end of a run.

## Run logs

`OpenRunLog(path, tailBytes)` opens a run's log by its `PlanRun.LogPath`. It takes the
path rather than a run ID because the plan manager only remembers the latest run, while your
records go further back. With `tailBytes > 0` it reads from that many bytes before the
end. The file may still be being written, so polling it follows a live run.

A path not under the log dir is `Forbidden`; no log dir or no such file is `NotFound`.

## Stats

`Stats` lists the plans by their `ID`, filtered by a label selector over their `Metadata`, so
operational tooling can group and inspect them. Each carries its latest run as `LastRun`, nil
if it has not run; a plan whose job succeeded is left out unless `All` is set.
