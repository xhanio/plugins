---
name: zen-conversation-watcher
description: Use when handling <channel source="zen"> events arriving in this Claude Code session. Describes the event-response decision tree, the reply-after-mutation audit rule, and the anchor-bounded action scope. Active automatically when this plugin is installed and the session was started with --dangerously-load-development-channels plugin:zen@xhanio.
---

# Zen Conversation Watcher (channel mode)

## Overview

When Claude Code is started with `--dangerously-load-development-channels
plugin:zen@xhanio` (channels are still a research preview, so that flag is what
registers one — passed alone, never alongside `--channels`), the
`zen-channel` MCP server inside this plugin pushes every Zen SPA user
message into my context as a `<channel source="zen" …>` event. This skill is
the recipe for responding to those events well.

## When to use

- A `<channel source="zen" …>` block appears in my context. (The skill's
  description triggers it automatically; I don't need the user to say
  anything.)

## When NOT to use

- The user is talking to me in the terminal about something unrelated to
  Zen. Channel events still arrive in the background; they just queue and I
  handle them between turns.

## Concurrency (several watcher sessions are fine)

Events are addressed to one session, not broadcast. Each event carries a
target session id and a subscriber drops anything not addressed to it, so
running several channel-enabled sessions against one backend does
**not** produce duplicate replies — each session only sees its own turns. If
a second channel registers the *same* session id, the older one is displaced
and stops (`ErrDisplaced`), so a stale process can't answer either.

## The decision tree

```
<channel source="zen" …> event arrives
  │
  ├─ Do I have enough information to act / answer fully?
  │
  ├── NO → reply(conversation_id, "<a clarifying question>")
  │         (next event in this conversation is the user's answer)
  │
  └── YES → choose shape:
        │
        ├── Shape 1 — pure Q&A
        │   reply(conversation_id, "<written answer>")
        │
        ├── Shape 2 — edit the conversation's anchor in place
        │   (anchor_kind="card" only)
        │   card.update(anchor_id, {…})
        │   reply(conversation_id, "<short conclusion + markdown link>")
        │
        └── Shape 3 — extend the anchor with a derived card
              card.create(
                title, content, group_id=<the anchor's group>,
                parent_card_id=anchor_id  (when anchor_kind=="card"),
                source_conversation_id=conversation_id,
                …
              )
              reply(conversation_id, "<short conclusion + markdown link>")
```

## Non-negotiable rules

1. **Ask before acting when uncertain.** If the request is ambiguous, I
   reply with a question first and wait for the next event. I do not guess
   and mutate.
2. **Reply after every mutation.** Every `card.update` or `card.create`
   triggered by a conversation message MUST be followed by `reply` with a
   short summary and a markdown link to the changed card (`[…](/c/<id>)`).
   This is the audit trail in the conversation thread.
3. **Actions stay anchor-bounded.** Mutations target either the
   conversation's anchor entity or extend it with a derived child card. I
   don't edit unrelated entities from inside a conversation thread.
4. **Standalone conversations (no anchor)** default to Shape 1. Shape 2
   doesn't apply (nothing to edit in place). Shape 3 only when the user
   explicitly asks me to create something.
5. **Group-anchored conversations** (`anchor_kind="group"`) have no body to
   edit, so Shape 2 doesn't apply. Answer (Shape 1), or create a card in
   that group (Shape 3, `group_id=anchor_id`, no `parent_card_id` — a group
   is not a parent card). Link a group as `[…](/g/<id>)`.

## Documents are cards

Zen shows "documents" — the home dashboard counts them per group, and
`decompose` talks about a "top-level document". They are not a separate
entity and there are no `document.*` tools. A **document is a live top-level
card that has at least one live child** (`frontend/src/utils/documents.ts` is
the single source of truth); it spans levels, so it is the multi-section unit
of a group. `decompose` is what turns a card into one.

Everything follows from that: a document arrives as `anchor_kind="card"`, is
edited with `card.update`, and is linked as `[…](/c/<id>)` — the SPA's own
dashboard routes to `{ name: 'card' }` for them. There is no `/d/` route.

(A `documents` table did exist once; migration `010_v06_card_only` dropped it
in v0.6, and the word returned in v0.16 meaning the derived view above. If a
doc tells you to call `document.update`, it predates v0.6.)

## Reading the event tag

```
<channel source="zen"
  conversation_id="01CONV…"   ← required arg to reply()
  message_id="01MSG…"          ← the user's message id
  anchor_kind="card"           ← "card" | "group"; both anchor attrs absent if standalone
  anchor_id="01CARD…"          ← anchor entity id (when anchor_kind is set)
  has_selection="true"         ← whether the message body has a quoted selection
  ts="2026-06-27T19:30:00Z">
> selected: "FTS5's snippet() helper"

what does this mean?
</channel>
```

The body is the user message verbatim. If `has_selection="true"`, the body
starts with a `> selected: "…"` quoted block — that's the span the user
highlighted in their SPA card/document before clicking Ask.

## When I need more context than the tag carries

The event tag has identifiers but no anchor content or message history. If
I need them:

- **Anchor body** — call `card.get(anchor_id)` (zen-mcp tool). For a group
  anchor, `group.get(anchor_id)` gives the group's rule and level catalog;
  `card.list` gives its cards.
- **Conversation history beyond what I remember** — call
  `conversation.get(conversation_id, message_limit=N)` (zen-mcp tool;
  defaults to 100).
- **Triage what else is waiting** — call
  `conversation.list({pending: true})` (zen-mcp tool).

Default behavior: I fetch the anchor only when the question genuinely needs
its content. Pure greetings or short follow-ups don't.

## Termination

Channel mode is per-session. To stop watching, exit Claude Code; restart
without the channel flag (a bare `claude`, bypassing the installer's alias with
`\claude`) to use Zen tools without receiving events.

## Common mistakes

| Mistake | Fix |
|---|---|
| Mutating without follow-up `reply` | "Do thing then reply" pattern — never let a `card.update` ship alone. |
| Acting on an ambiguous request | Reply with a clarifying question first; the next event is the answer. |
| Editing entities unrelated to the conversation's anchor | Stay anchor-bounded. If the user wants to change something else, they'll start a separate conversation. |
| Reaching for `document.*` tools, or linking `/d/<id>` | See "Documents are cards" below — every document is a card and is addressed as one. |
| Treating a `group` anchor like a card | A group has no body: no Shape 2, and no `parent_card_id` on Shape 3. |
| Forgetting `parent_card_id` / `source_conversation_id` on Shape 3 | Derived cards must carry both so their provenance is queryable. |
| Confusing the channel's `reply` tool with the zen-mcp tools | Different servers: `mcp__plugin_zen_zen-channel__reply` talks back to the user; `mcp__zen__card_update` and friends edit entities. |
