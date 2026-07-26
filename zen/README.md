# zen — Claude Code plugin

Self-hosted knowledge-tool integration: pushes Zen SPA chat messages into
your Claude Code session as channel events, exposes a `reply` tool to
respond, bundles the `zen-knowledge-capture` and `zen-conversation-watcher`
skills, and registers the zen HTTP MCP server.

## Requirements

- Claude Code v2.1.80 or later (channels research preview)
- Docker — Desktop on macOS/Windows, Engine on Linux

The installer below provides the two things the plugin needs: a running Zen
backend and the `zen-channel` binary on your `$PATH`.

## Install Zen

One installer does the whole job — it pulls the image, starts Zen at
`http://localhost:38000`, drops the matching `zen-channel` binary on your
`$PATH`, and installs this plugin into Claude Code:

```bash
curl -fsSL https://raw.githubusercontent.com/xhanio/zen/main/scripts/install.sh -o zen-install.sh
bash zen-install.sh          # installs to ~/zen; pass a directory to change it
```

If the target bin dir isn't on your `PATH`, the installer prints the line to
add. Manage it later with the same script:

```bash
bash zen-install.sh --update      # pull the newest image, keeping your data
bash zen-install.sh --uninstall   # stop Zen (the data folder is kept)
```

Your cards live in `~/zen/data` — a plain folder, not a Docker volume — so back
them up by copying it.

### Just the binary

If Zen already runs elsewhere and you only need the plugin binary, pull the one
for **your** machine's OS/CPU — not the container's. On a Mac the container is
Linux (Docker runs it in a VM) but Claude Code needs a darwin build:

```bash
docker run --rm --entrypoint cat docker.io/xhanio/zen-allinone:latest \
  /app/plugin/zen-channel_darwin_arm64 > ~/.local/bin/zen-channel   # Apple Silicon
chmod +x ~/.local/bin/zen-channel
```

Swap the filename for your host: `zen-channel_darwin_amd64` (Intel Mac),
`_linux_amd64`, or `_linux_arm64`.

## Add the plugin to Claude Code

The installer above already did this. To do it by hand — or on a machine where
Zen itself runs elsewhere — add the marketplace and install the plugin:

```
/plugin marketplace add https://github.com/xhanio/plugins
/plugin install zen@xhanio
```

Then restart with the channel enabled:

```
claude --dangerously-load-development-channels plugin:zen@xhanio
```

Channels are still a research preview, so this is the only flag that registers
one today. Pass it **alone** — do not also pass `--channels plugin:zen@xhanio`.
Claude Code resolves a server to its channel entry first-match-wins, and
`--channels` appends a non-dev entry ahead of the dev one, so the lookup returns
the non-dev entry and the plugin is rejected as "not on the approved channels
allowlist" — the very error the dev flag exists to bypass. (When channels
graduate from preview, plain `--channels` becomes the right flag.)

Accept the "Loading development channels" warning at startup: it blocks MCP
init, so nothing registers until you do. To confirm, launch with `--debug` and
look for `Channel notifications registered` rather than `… skipped`.

The installer adds a `claude` alias carrying this flag to your shell rc, so a
plain `claude` does the right thing. Without it — or without the flag — the
plugin still loads its skills and MCP tools, but Zen chat messages never reach
your session. Note the flag is ignored in non-interactive mode (`claude -p`),
so headless sessions cannot use the channel at all.

## Configure

The channel connects to `ws://localhost:38000/api/v1/conversations/_stream/ws`
by default. Zen is self-hosted on this machine, so the host is always
`localhost`; the only thing that varies is the port. If you published Zen on a
port other than 38000, point `ZEN_BACKEND_URL` at it:

```bash
export ZEN_BACKEND_URL=http://localhost:18000   # whatever port you published
claude --dangerously-load-development-channels plugin:zen@xhanio
```

That covers the channel push. The `zen` HTTP MCP server — the card, search and
group tools — is configured separately, because it is a different process
reached over HTTP rather than the backend's WS fan-out. It defaults to
`http://localhost:38000/api/v1/mcp` and moves with `ZEN_MCP_URL`:

```bash
export ZEN_BACKEND_URL=http://localhost:18000       # channel → backend
export ZEN_MCP_URL=http://localhost:18000/api/v1/mcp # tools → MCP endpoint
claude --dangerously-load-development-channels plugin:zen@xhanio
```

Set both, or the halves disagree: chat events arrive from one Zen while the
card tools edit another.

### Pointing the plugin at a dev checkout

Running Zen from source splits what the all-in-one image serves behind one
port: `zen-backend` on `:8080` and `zen-mcp` on `:8081`. `make dev` starts
both (plus Vite), and these two variables aim a session at them:

```bash
export ZEN_BACKEND_URL=http://127.0.0.1:8080
export ZEN_MCP_URL=http://127.0.0.1:8081/api/v1/mcp
claude --dangerously-load-development-channels plugin:zen@xhanio
```

Your installed Zen on 38000 keeps running untouched — this only changes where
one Claude session looks.

## What's inside

- **zen-channel** MCP server — a `zen-channel` subprocess on your machine.
  Subscribes to Zen's fan-out WS, pushes `<channel source="zen" …>` events,
  exposes `reply(conversation_id, content)`.
- **zen** MCP server — HTTP reference to the zen-mcp daemon running inside your
  Zen container. Needs no local binary.
- **zen-knowledge-capture** skill — captures specs / decisions as Zen cards.
- **zen-conversation-watcher** skill — describes the event-response loop.
