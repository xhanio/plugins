# framingo — Claude Code plugin

Packages the `framingo` skill so Claude Code knows how to write code against
the [Framingo](https://github.com/xhanio/framingo) Go framework: bootstrapping a
new backend, creating services and registering them with the supervisor, the
declarative HTTP API server, the database manager, pub/sub messaging,
tracing and metrics, and the package layout the framework expects.

Documentation only — no MCP server, no binary, nothing to run.

## Install

```
/plugin marketplace add https://github.com/xhanio/plugins
/plugin install framingo@xhanio
```

The skill activates on its own whenever a session touches framingo — a
`github.com/xhanio/framingo` import, a mention of the supervisor, service
lifecycle, handler groups, or a request for a new Go backend. You can also
invoke it by name with `/framingo`.

## What's inside

One skill, `framingo`, whose reference files split into two halves plus the
design decisions behind them, and a set of copy-ready templates:

- **SKILL.md** — the entry point: the reference map, quick reference,
  architecture, common mistakes, and the recipe for starting a new backend.
- **pkgs/** — how to *use framingo's packages*: supervisor & service
  lifecycle, the API server and HTTP client, db, pubsub & messagebus,
  plan and task managers, tracing, metrics, framework types, config,
  errors, and utilities.
- **app/** — how to *write an application* shaped like `example/`: package
  layout, authoring services, routers, and middlewares, the project
  `types/` categories, the components family (server daemon, cobra cmd,
  client SDK), and testing.
- **design/** — *why* things are shaped as they are: the decisions the
  project met, by area (who decides access, narrow contracts, data access,
  service wiring, WebSockets, migrations), each with its reason and cost.

Only SKILL.md is read up front; the references load when the task calls for
them.

`_templates/` holds the files a new project can't be written without — the
project-owned `api.Context` + `DiscoverHandlers`, the canonical
`router.go`/`handler.go`/`router.yaml` triple, and the two halves of a service
interface (the project's `types/model` contract and the service's own
`Manager`). They are copied into a new project rather than read: the skill's
handler pattern depends on `api.Context` existing, and hand-reconstructing it
from prose yields a subtly different type that every handler then inherits.
The directory is underscore-prefixed so the Go toolchain ignores it — the Go
files are real and compile-verified, not sketches.

## Versioning

The plugin has a version of its own, separate from the framework module's:
`plugin.json` and the skill's `metadata.version` carry it, and the plugin
cache is keyed by it, so an unbumped version never reaches installed copies.
Which framework release the docs describe is pinned separately, in the
skill's `compatibility` line and `metadata.framingo`. Where a repo's go.mod
pins a different framingo version, the code outranks this prose.

Every framingo shipment ends with a single Release commit that bumps every
version surface at once — the plugin version and skill mirror (when docs
changed since the last release), both framingo pins here, and the example's
`project.yaml` version and `go.mod` requirement — and the tag lands on that
commit, so a tag always contains metadata naming itself. The marketplace
mirror syncs after.

## Source

This plugin is developed in the framework repo at
[`plugins/framingo`](https://github.com/xhanio/framingo/tree/main/plugins/framingo)
and mirrored into the [xhanio/plugins](https://github.com/xhanio/plugins)
marketplace. File issues against the framingo repo.
