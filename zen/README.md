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

The all-in-one image carries its own installer. Extract and run it — it pulls
the image, starts Zen at `http://localhost:38000`, and drops the matching
`zen-channel` binary on your `$PATH`:

```bash
docker run --rm --entrypoint cat \
  docker.io/xhanio/zen-allinone:latest /app/install.sh > zen-install.sh
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

With Zen running, add the marketplace and install the plugin:

```
/plugin marketplace add github.com/xhanio/plugins
/plugin install zen@xhanio
```

Then restart with the channel enabled:

```
claude --channels plugin:zen@xhanio
```

## Configure

The channel connects to `ws://localhost:38000/api/v1/conversations/_stream/ws`
by default. Zen is self-hosted on this machine, so the host is always
`localhost`; the only thing that varies is the port. If you published Zen on a
port other than 38000, point `ZEN_BACKEND_URL` at it:

```bash
export ZEN_BACKEND_URL=http://localhost:8000
claude --channels plugin:zen@xhanio
```

## What's inside

- **zen-channel** MCP server — a `zen-channel` subprocess on your machine.
  Subscribes to Zen's fan-out WS, pushes `<channel source="zen" …>` events,
  exposes `reply(conversation_id, content)`.
- **zen** MCP server — HTTP reference to the zen-mcp daemon running inside your
  Zen container. Needs no local binary.
- **zen-knowledge-capture** skill — captures specs / decisions as Zen cards.
- **zen-conversation-watcher** skill — describes the event-response loop.
