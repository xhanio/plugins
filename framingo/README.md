# framingo — Claude Code plugin

Packages the `framingo` skill so Claude Code knows how to write code against
the [Framingo](https://github.com/xhanio/framingo) Go framework: bootstrapping a
new backend, creating services and registering them with the supervisor, the
declarative HTTP API server, the database manager, pub/sub messaging, and the
package layout the framework expects.

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

One skill, `framingo`, with four reference documents it pulls in on demand, plus a
set of copy-ready templates:

- **SKILL.md** — the entry point: architecture, core concepts, the service
  lifecycle interfaces, database and pub/sub usage, how to create a new
  service, common mistakes, and the recipe for starting a new backend.
- **api-server.md** — the API server in full: registration flow, `fapi.Router`
  and middleware contracts, `router.yaml` format, handler keys, route mapping,
  WebSocket handlers, and middleware resolution.
- **package-layout.md** — the required `pkg/` structure, category rules, type
  separation, server component file layout, and import organization.
- **config-reference.md** — annotated config YAML template covering log, db,
  api, pprof, and custom service keys.
- **errors-reference.md** — `github.com/xhanio/errors`: creating, wrapping,
  combining, and checking errors, plus the category → HTTP status table.

Only SKILL.md is read up front; the references load when the task calls for
them.

`_templates/` holds the files a new project can't be written without — the
project-owned `api.Context` + `DiscoverHandlers`, the canonical
`router.go`/`handler.go`/`router.yaml` triple, and the two halves of a service
interface. They are copied into a new project rather than read: the skill's
handler pattern depends on `api.Context` existing, and hand-reconstructing it
from prose yields a subtly different type that every handler then inherits.
The directory is underscore-prefixed so the Go toolchain ignores it — the
files are real, compile-verified Go, not sketches.

## Versioning

The plugin has a version of its own, separate from the framework module's:
`plugin.json` and the skill's `metadata.version` carry it, bumped together
whenever the docs change — the plugin cache is keyed by it, so an unbumped
version never reaches installed copies. Which framework release the docs
describe is pinned separately, in the skill's `compatibility` line and
`metadata.framingo`; refresh the pin with every framingo release that touches
these docs. Where a repo's go.mod pins a different framingo version, the code
outranks this prose.

## Source

This plugin is developed in the framework repo at
[`plugins/framingo`](https://github.com/xhanio/framingo/tree/main/plugins/framingo)
and mirrored into the [xhanio/plugins](https://github.com/xhanio/plugins)
marketplace. File issues against the framingo repo.
