# Planner — Scheduled and Ad-Hoc Task Execution

`pkg/services/planner` runs plans — described tasks with optional schedules —
and reports their results through the message bus. `planner.Manager` =
`model.Planner` + `Daemon` + `Debuggable`.

## Interfaces

```go
// pkg/types/model
type Planner interface {
    common.Service
    Add(todo *entity.Plan) error
    Cancel(id string) error
    Delete(id string, force bool) error
    GetResult(id string) (any, error)
    Stats(opts entity.PlannerStatsOptions) ([]*entity.PlannerStats, error)
}

// pkg/types/entity
type Plan struct {
    ID          string
    Metadata    labels.Set   // k8s.io label set - selectable via Stats
    Description string
    Task        *task.Task   // pkg/utils/task - what actually runs
}

type PlannerStatsOptions struct {
    Selector string // label selector over Plan.Metadata
    All      bool
}
```

## Construction

```go
import "github.com/xhanio/framingo/pkg/services/planner"

// es: where task results/events are sent - typically the messagebus manager
// (any common.MessageSender).
pl := planner.New(messagebusMgr,
    planner.WithLogger(logger),
)
```

Register it with the supervisor like any service; it is a `Daemon`, so the
supervisor starts and stops it.

## Jobs

Plans wrap `pkg/utils/task` tasks. For shelling out, the package ships job
constructors:

```go
job := planner.NewBashJob(caller, labels.Set{"kind": "backup"}, "/usr/bin/pg_dump", []string{"mydb"})
// NewAsyncBashJob - same, detached from the caller's wait
```

`Stats` filters by label selector over each plan's `Metadata`, so operational
tooling can group and inspect running plans. Results flow to the
`MessageSender` handed to `New` — on the example's wiring, that is the
message bus, so any registered `RawMessageHandler` can consume them
([pubsub.md](pubsub.md)).
