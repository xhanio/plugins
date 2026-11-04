# Planner — Scheduled and Ad-Hoc Task Execution

`pkg/services/planner` runs plans — described tasks with optional schedules —
and reports the runs of its predefined kinds through the message bus.
`planner.Manager` = `model.Planner` + `Daemon` + `Debuggable`.

## Interfaces

```go
// pkg/types/model
type Planner interface {
    common.Service
    Add(todo *entity.Plan) error
    // Runs the plan now; returns the run's ID, empty for a plan without a kind.
    // A plan already queued, running or being requeued is a Conflict.
    Trigger(id string) (string, error)
    // Ends what the plan is doing and keeps the plan; an idle plan is a Conflict.
    Cancel(id string) error
    Delete(id string, force bool) error
    // The Job's result; for a plan of a predefined kind, its latest entity.PlanRun.
    GetResult(id string) (any, error)
    Stats(opts entity.PlannerStatsOptions) ([]*entity.PlannerStats, error)
}

// pkg/types/entity
type Plan struct {
    ID          string
    Metadata    labels.Set // k8s.io label set
    Description string
    Kind        string     // a predefined kind, or empty for a Task with its own Job
    Task        *task.Task // pkg/utils/task - what actually runs
}

type PlannerStatsOptions struct {
    Selector string // label selector over the Job's labels
    All      bool
}
```

The `Task` decides when a plan runs: `Schedule` for cron, `Manual: true` to keep it
registered until `Trigger` runs it (the two exclude each other), neither to run it
once on `Add`. `Timeout`, `RetryAttempts`, `RequeueAttempts` and the rest apply as
they do in `pkg/utils/task`.

## Construction

```go
import "github.com/xhanio/framingo/pkg/services/planner"

// es: where run events are sent - typically the messagebus manager
// (any common.MessageSender), or nil. The planner depends on it.
pl := planner.New(messagebusMgr,
    planner.WithLogger(logger),
    planner.WithLogDir("/var/log/myapp/plans"), // empty keeps no log files
    planner.WithOutputTailBytes(4096),          // the default
)
```

Register it with the supervisor like any service; it is a `Daemon`, so the
supervisor starts and stops it.

## Command plans

`entity.PlanKindCommand` is the one predefined kind. The planner builds the `Job`,
so the caller writes none:

```go
err := pl.Add(&entity.Plan{
    ID:   "backup",
    Kind: entity.PlanKindCommand,
    Task: &task.Task{
        Params:   entity.CommandParams{Command: []string{"/usr/bin/pg_dump", "mydb"}, Dir: "/tmp"},
        Schedule: "30 2 * * *",
        Timeout:  time.Hour,
    },
})
runID, err := pl.Trigger("backup") // run it now as well
```

A command runs in its own process group, so canceling or timing it out reaches its
children. Its output goes to `<log dir>/<plan ID>/<time>-<run>.log` and the tail of
it into the run.

## Run events

Every run of a command plan is reported through the `MessageSender`, with an
`entity.PlanRun` carrying its ID, plan ID, source (`schedule` or `manual`), state,
times, exit code, output tail, log path and error:

| Event | `Kind()` | When |
|---|---|---|
| `entity.PlanRunStarted` | `planner.run.started` | before the command starts |
| `entity.PlanRunFinished` | `planner.run.finished` | after it ends: `succeeded`, `failed` or `canceled` |

A retry or a requeue is a new run with a new ID. Consume the events with a
`MessageHandler` on the bus ([pubsub.md](pubsub.md)):

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
with `GetResult` from time to time, which holds the plan's latest `PlanRun` while the
process lives, and make applying a run idempotent, since both paths deliver the end
of a run.

## Plans with their own job

Without a `Kind`, the `Task` carries its own `Job`, and the planner sends no events for
it. For shelling out from one, the package ships job constructors:

```go
job := planner.NewBashJob(caller, labels.Set{"kind": "backup"}, "/usr/bin/pg_dump", []string{"mydb"})
// NewAsyncBashJob - same, detached from the caller's wait
```

`Stats` filters by label selector over each job's labels, so operational tooling can
group and inspect running plans; a command plan's job takes its labels from the plan's
`Metadata`.
