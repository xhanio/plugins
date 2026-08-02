# Components — cmd, server, client

`pkg/components/` is the application-wiring category: the code that assembles
services into a runnable program. Three kinds, each in its own subtree, each
with its own reference:

| Subtree | Role | Reference |
|---|---|---|
| `components/cmd/` | Cobra CLI wiring — one package per binary persona; thin `main`s under `build/binary/` | [components-cmd.md](components-cmd.md) |
| `components/server/<app>/` | The application daemon: config, service graph, API registration, signals | [components-server.md](components-server.md) |
| `components/client/<app>/` | The app-facing SDK over the HTTP client | [components-client.md](components-client.md) |

The flow between them: a binary's `main` executes `cmd/`'s root command; the
`daemon` subcommand constructs and runs the server component; the CLI
subcommands construct the client component and call the running server
through it.
