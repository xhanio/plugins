---
name: zen-knowledge-capture
description: Use when working in the zen repo and we generate knowledge worth preserving — both long-form notes and focused decisions / tradeoffs / gotchas / patterns become Zen Cards (optionally decomposed into children) so the user can browse and search them later from Zen's web UI. Also use when the user asks to "ingest" a document file into Zen. Captures domain knowledge, NOT how-to-work-with-this-user feedback (that goes in auto-memory).
---

# Zen Knowledge Capture

## Overview

Zen is the user's self-hosted knowledge tool — this very repo. The data model is **Cards** in a flat list of **Groups**, carrying **Tags**. A long-form note IS a card; a focused takeaway IS a card; a card can be decomposed into N child cards — and the parent **stays live as a container** whose body renders the ordered composition of its children (it is *not* deleted). "Document" is not a separate entity: it is the name for a card that has children — the multi-section unit of a group (`frontend/src/utils/documents.ts`). There are no `document.*` tools; a document is edited as the card it is.

This skill is the recipe for capturing our conversation into Zen as it happens: I propose what to save, the user approves, I call the zen-mcp tools.

## When to use

**Propose a Card when:**
- A spec / plan / design note is being authored or finalized (anything written in `docs/superpowers/specs/` or `docs/superpowers/plans/` is canonical — capture as a long-form card)
- The user pastes >~300 words of source text worth re-reading later
- A design decision is made + the reasoning behind it ("we picked X over Y because Z")
- A non-obvious gotcha is uncovered (the kind that would burn the next reader)
- A useful pattern, snippet, or workflow emerges
- After finishing a task there's a takeaway worth more than a commit message

## When NOT to use

- Information already in code, commit history, or design docs — `git log` / `grep` are authoritative
- Transient debugging steps ("I tried X then Y") — only the conclusion is card-worthy
- "How to work with the user" guidance → that goes in [auto-memory](../../../../memory/), not Zen

## Picking a format

Cards carry one of three content formats. Default to `markdown`. Reach for the others only when they buy you something specific.

- **markdown** (default) — prose, notes, decisions, design summaries. Markdown headers / lists / code fences render natively.
- **text** — logs, raw data dumps, code snippets where markdown's accidental formatting (asterisks, underscores) would corrupt the content. Verbatim `<pre>` rendering.
- **html** — rich layout markdown can't express: inline `<svg>` diagrams, complex tables, MathML, fine-grained typography. Pass `format: "html"` to `card.create` or `card.update`. Write static markup only: no `<script>`, no inline event handlers (`on*`), no `javascript:` URLs. The renderer silently strips disallowed content.

Decompose works for cards of any format. Each `cards[]` spec accepts an optional `format` to mix formats per child. When `format` is omitted on a spec, the child inherits the parent's format.

## Picking a level

Each card sits at an abstraction level within its Group. A group's `level_catalog` is an ordered list of entries, each `{id, weight, name}` — `weight` is a float (0 = most abstract; higher = more concrete), `name` is user-visible ("原则" / "模式" / "决策" / "细节" in a typical Design group). The catalog is per-Group; the same weight means different things in different groups. A card attaches to one entry by its **`level_entry_id`** (the entry's ULID) — not by a number or a name.

When creating or decomposing into a Group:

- **If an existing level fits → reuse it.** Look up the entry's `id` from `group.list` (each group carries its `level_catalog`) and pass `level_entry_id: "<that id>"`.
- **If no existing level fits → add one first.** Call `group.update` with a new `{weight, name}` appended to `level_catalog` (pick the weight: midpoint between neighbors, or extend past the edges); the response echoes the catalog with the server-assigned `id`. Then pass that `id` as `level_entry_id`. There is no way to mint a level through `card.create` itself.
- **To leave a card Unfiled**, omit `level_entry_id` (on create) or pass `clear_level_entry: true` (on update).
- **Don't rename or delete existing levels while capturing.** That's a human operation through `group.update`.

When deriving a card from a parent (`parent_card_id` set), pick the catalog entry whose `weight` sits in the right direction from the parent's:

- A **summarization** card moves toward abstraction — a *lower*-weight entry than the parent's.
- An **explanation / expansion / detail** card moves toward concreteness — a *higher*-weight entry.
- A **sibling** rephrasing or alternative angle reuses the parent's entry (same `level_entry_id`).

## Provenance conventions

Every card has a `genesis` field — a free-form human-readable note about where the card came from.

**Genesis MUST NOT contain raw card IDs, conversation IDs, or any ULID.** Show provenance with a title breadcrumb instead — titles are what the reader sees on the tile; IDs are unreadable noise. The backend enforces this convention in its own defaults, and so must any override you write.

- **Decomposed from a parent**: backend default is `"Decomposed from <ancestor title chain>"` — e.g. `"Decomposed from Zen roadmap - v0.12 planning - v0.12 spec"` (just the parent's title for a top-level parent). Override only when a more specific human note helps.
- **Composed from sources**: backend default is `"Composed from <source1 title>, <source2 title>, …"`.
- **Derived from a conversation**: pass `parent_card_id` + `source_conversation_id`, and describe the origin in words, e.g. `"Extracted from user chat about SectionGradePill"` — never the conversation's ULID.
- **AI-generated for a specific purpose**: e.g. `"AI-generated for v0.6 spec example"`.
- **Created by a human via the SPA**: backend default is `""` (empty).

## Decomposing a card

When asked to split a long Card into focused children:

- **Decompose is lossless splitting, not synthesis.** Each child's body is a **verbatim slice** of the parent's body **minus that section's own title heading** (see the title rule below); taken together the slices reproduce the parent's content modulo per-card wrapping, the shared style block, and the extracted section titles. If a "useful takeaway" isn't in the source text, it does not belong in a decomposed card — make a separate card via `card.create`.
- **The parent stays live as a container.** After decompose, its body is cleared and opening it renders the ordered composition of its children (each carries `parent_card_id` back to it). Optionally pass `container_content` to keep a verbatim leading metadata slice — the document's preamble, e.g. a date/status block — on the parent above the sections (valid only on a top-level card). A card that already has live children cannot be decomposed again.
- **Split on the source's own structural seams** — for HTML, the `<h2>` / `<section>` boundaries; for markdown, the `##` headers; for text, the blank-line paragraph groups. The number of children should fall out of the source's existing structure.
- **Card title = the section's heading text**, verbatim. Don't editorialize. **The title lives ONLY in the `title` field — never repeat it as a leading heading inside the child's content.** The SPA renders it as the section header; a body that starts with the title as `<h2>`/`##` has that heading stripped on decompose (the backend enforces it, matching on the title). Author each child's body starting *after* its title.
- **Omit `format`** per spec so each child inherits the parent's format. Only set `format` when the user explicitly wants a mix.
- **For HTML parents, copy the `<style>` block and wrapper class into every child's content** so the children inherit the parent's typography, color, and rhythm. Pattern: each child starts with `<article class="<doc-class>"><style>…</style>…</article>` reusing the parent's style block.

## Ingesting a document file

When the user says **"ingest `<path>` into zen"** (or "ingest this doc"), that names one
specific recipe: turn the file on disk into a card whose format and language follow the target
group's rule, then split that card into children.

Produce exactly these steps, in order:

1. **Read the file** from disk.
2. **`search(<title-ish>)`** to dedupe. On a hit, propose `card.update` instead and stop here.
3. **`group.list` / `tag.list`** — pick the group, **read its `rule`**, reuse existing tags and
   level. The rule governs the card's format and language (see step 4).
4. **Conform the body to the group's rule.** Format is whatever the rule requires — HTML (one
   shared `<style>` block + wrapper class, static markup only), or markdown when the rule is
   silent (markdown now renders with full styling, so this is the default, not a fallback). If
   the rule mandates a language, translate into it. Card title = the document's `#` H1,
   conformed the same way (translated if the rule requires it).
5. **Propose** title + group + tags + the child titles (and each child's level; the container itself is Unfiled — see below). Wait for the user's OK.
6. **`card.create(..., format: <the group's format>)`** → returns `{id}`.
7. **`decompose(parent_card_id=<id>, cards[])`** splitting the **card you just created** — not
   the original file — on its content seams (see below). Each child's body is a verbatim slice
   of the conformed card. Child title = each heading, verbatim from the conformed card. Omit
   per-child `format` so children inherit the parent's. For an HTML card, copy the shared
   `<style>` block and wrapper class into every child, per the decompose rules above.
8. **Report** the parent and child ids so the user can deep-link.

**Find the seam depth before you split.** Split at the heading level where the document's
*content units* actually live, which is not always `<h2>`. A spec whose `##` sections each hold
one idea splits at `<h2>`. An implementation plan that uses `##` for four framing sections and
`###` for ten tasks splits at the mixed depth — framing sections at `<h2>`, each task at `<h3>`
— because a pure `<h2>` split would collapse every task into one giant child. Scan the heading
outline first; ignore `#` inside fenced code blocks, which are comments, not headings. State the
chosen depth in the step-5 proposal.

**Content above the first heading becomes a lead child.** Preamble prose — a goal statement,
an intro blockquote, tech-stack bullets — belongs to no section, so a strict heading split would
drop it and break losslessness. Give it its own first child, titled with the document's H1
verbatim. It is the one child whose title isn't its own heading, because the source never gave
it one. Never fold it into the first section: that misfiles it.

**The container card itself is Unfiled — never pin it to a level.** A decomposed document is
multi-level: its sections sit at 原则/决策/模式/细节 (or the group's own catalog) as each one
warrants, and the container merely aggregates them. Giving the container a single level
misrepresents the whole document as one abstraction. Create the parent with NO level (omit
`level_entry_id`) so it lands Unfiled; its level legend then shows the spread of its
children's levels. Assign levels only to the section children — per `cards[]` spec, matched to
each section's abstraction — not one blanket level across all of them.

**Format follows the group rule, not a hardcoded default.** Use HTML when the rule requires it
(or the content genuinely needs rich layout markdown can't express); otherwise markdown, which
now renders with full styling — headings, lists, code, and tables all display correctly. Don't
force HTML on a ruleless group.

**Conforming to the group rule is a formatting change, not an editorial one.** Reformatting
(markdown → HTML) and translating (into the group's language) change the markup and the words,
but add no ideas: write no summary, no preface, no "key takeaways" section. A genuine takeaway
is a separate `card.create` afterwards. Then decompose the **conformed card losslessly** — each
child is a verbatim slice of the card you created, and the slices reproduce its body. Measure
losslessness against that card, not the original file: once the rule has translated or
reformatted the body, the card is the source of truth, and slicing the untranslated file would
reintroduce the wrong language or markup.

**`ingest` is the only trigger for this recipe.** `review` never triggers it. In Zen, *review*
means grading a card `LGTM` / `DIGESTED` / `GRILLED` (the v0.12 review system); in plain English
it means critiquing a document. So:

- "review `<file path>`" → write a critique. Don't touch Zen.
- "review `<card>`" → the user is talking about grades.
- "ingest `<file path>` into zen" → this recipe.

After finishing a critique you may *offer* to ingest the file, but never ingest on `review`
alone.

## Composing cards

Symmetric inverse of decompose: merge N existing Cards into 1 target Card by **lossless joining**. Call via `compose(source_card_ids[], target)` where `target` is a `CardSpec` (`{title, content, summary?, group_id?, level_entry_id?, tags?, format?, genesis?, position?}`).

- **Compose is lossless joining, not synthesis.** The AI's only creative act is **picking a good order**. `target.content` is the source bodies **concatenated verbatim** in that order, **each preceded by its source card's title as a heading** — titles live in the `title` field, not the body, so re-add them (e.g. `<h2>{title}</h2>` for HTML, `## {title}` for markdown) or the composed document loses its section structure. Do NOT otherwise rewrite for coherence, summarize, add framing prose, or drop sentences. If you find yourself paraphrasing or re-casting lists into prose, stop — that's a separate `card.create`, not compose.
- **Round-trip property:** `decompose → compose` reproduces the original parent's body modulo per-card wrapping — provided compose re-adds each section's title heading (decompose extracted it into the `title` field). If your compose can't survive that test, you've done synthesis instead of joining.
- **For HTML sources:** concatenate inner content while reusing one shared `<style>` block + wrapper class on the composed card — same pattern as decompose children. Strip the per-child `<style>` blocks from each source body before joining.
- **Sources must all live in the same group** and must all be live (not soft-deleted). Compose rejects cross-group sources and duplicates.
- **Compose soft-deletes the sources.** They move to Trash; recoverable via `card.restore` per source.
- **Default `target.genesis` is `"Composed from <source1 title>, <source2 title>, ..."`** — titles, never IDs — auto-filled by the backend when you leave `genesis` blank. This phrasing reflects "what was joined," not "what was rewritten."
- **Target inherits the sources' group** by default. Override `target.group_id` to place the composed card elsewhere.
- **Minimum 2 sources.**

## Anchoring derivations with references

When you create a card derived from a user's text selection in another card, **anchor the reference inline on `card.create`**:

```
card.create(
  title=..., content=..., group_id=...,
  parent_card_id=<source>, source_conversation_id=<conv>,
  reference={selection_text: "<verbatim text>"}
)
```

The selection text must be the **verbatim** excerpt the user highlighted — do not paraphrase, do not trim, do not normalize whitespace. The frontend matches strings character-by-character; any edit breaks the highlight. Backend creates the card and the reference atomically.

`source_conversation_id` is optional. If the derivation didn't go through a chat (SPA-driven derivations, future user-curated highlights), omit it; the resulting reference's `conversation_id` is null and clicking the highlight navigates to the derived card instead of opening the chat panel.

Use standalone `reference.create` only when:
- Adding a reference to a card you didn't just create (back-fill).
- Adding multiple selections to one derived card (call reference.create once per extra selection after card.create).

## Soft-delete + Trash

- `card.delete(id)` is a **soft** delete. The card disappears from default lists + search but stays in the DB with `deleted_at` set.
- `card.restore(id)` clears `deleted_at` and brings the card back. Errors if the card is live.
- `card.purge(id)` hard-deletes a soft-deleted card. Errors if the card is live (must soft-delete first).
- `trash.list({limit?})` returns soft-deleted cards across all groups, newest first.

## Workflow

1. **Detect a trigger** from above.
2. **Search first** — `search(title-ish)` or `card.list({group_id?})` to avoid duplicates. If found, propose updating instead.
3. **Propose, don't unilaterally write** — sketch title + group + tags + 1-line content summary. Wait for user OK.
4. **Pick group** — call `group.list` once per conversation; suggest if user didn't specify.
5. **Pick tags** — reuse existing (`tag.list`); only invent when nothing fits. Tags are lowercase, hyphen-separated.
6. **Call the tool** — `card.create(...)`. Report id back so the user can deep-link.

For "paste big text → split into cards":
1. `card.create(title, content, group_id)` → returns `{id}`
2. Propose 3–7 candidate child titles to the user.
3. On OK: one `decompose(parent_card_id=<that id>, cards[])` — transactional batch. The parent stays live as the container; children carry `parent_card_id` back to it.

## MCP tools quick reference

zen-mcp must be wired into Claude Code first. If the tool names below aren't in the tool list, tell the user and stop.

| Purpose | Tool | Notes |
|---|---|---|
| List groups | `group.list()` | Flat list; cache once per conversation |
| List tags w/ counts | `tag.list()` | Pick from these before inventing |
| Search before write | `search(query, scope?, limit?)` | Excludes soft-deleted cards |
| Get a card | `card.get(id)` | Works on soft-deleted cards too |
| List cards | `card.list({group_id?, include_trashed?})` | Default excludes soft-deleted |
| Create card | `card.create(title, content, group_id, {tags?, summary?, level_entry_id?, genesis?, parent_card_id?, source_conversation_id?, format?, reference?})` | Returns Card. Level via `level_entry_id` (a catalog-entry ULID), not a number |
| Update card | `card.update(id, {title?, content?, summary?, level_entry_id?, clear_level_entry?, genesis?, group_id?, position?, tags?, format?})` | `tags` (non-nil) REPLACES the whole set; `clear_level_entry: true` detaches the level |
| Soft-delete a card | `card.delete(id)` | Moves to Trash; recoverable |
| Restore a soft-deleted card | `card.restore(id)` | Errors if card is live |
| Hard-delete a soft-deleted card | `card.purge(id)` | Errors if card is live (must soft-delete first) |
| List soft-deleted cards | `trash.list({limit?})` | Ordered by deleted_at descending |
| Decompose a card | `decompose(parent_card_id, cards[], {container_content?})` | Parent stays live as the cleared container; children carry parent_card_id |
| Compose N cards into 1 | `compose(source_card_ids, target)` | Inverse of decompose; soft-deletes all sources; target.genesis defaults to "Composed from &lt;titles&gt;" |
| Anchor a derivation (back-fill) | `reference.create(source_card_id, derived_card_id, conversation_id, selection_text)` | Returns Reference. For most cases use the inline reference on card.create instead. |
| Get a reference | `reference.get(id)` | Returns Reference. |
| List references | `reference.list({source_card_id?, derived_card_id?, conversation_id?})` | At least one filter required. |
| Delete a reference | `reference.delete(id)` | Removes the highlight; does not touch the referenced cards/conversation. |

## Example

Conversation moment: user explains why they chose SQLite + FTS5 over Postgres.

I propose:
> Want me to capture this as a card under "Zen design" (group), tags `decision,backend,sqlite`?
>
> Title: **Why SQLite + FTS5 (not Postgres)**
> Content: three short paragraphs — single-binary deploy story, FTS5's `snippet()` HTML helper, single-file ops + backup, downsides accepted.

On user OK → `card.create(title, content, group_id, tags=["decision","backend","sqlite"])`.

## Common mistakes

| Mistake | Fix |
|---|---|
| Creating without asking | Always propose first — Zen is the user's knowledge base, not mine. |
| Skipping the search step → duplicates | Always `search` (or filtered `card.list`) before creating. |
| Putting "user prefers X" feedback into a card | That's auto-memory feedback, not domain knowledge. Use the memory dir. |
| Inventing new tags when existing ones fit | Pull `tag.list` and reuse. |
| Forgetting `tags` on `card.update` is a REPLACE | If the user said "add tag X", read existing tags first then PUT the union. |
| Synthesizing during `decompose` | Decompose is verbatim splitting only. Synthesis = a separate `card.create`. |
| Treating "review this file" as an ingest | `review` means critique (on a file) or grading (on a card). Only `ingest` triggers the ingest recipe. |
| Splitting an ingest strictly on `<h2>` | Find the depth where content units live — a plan's tasks may sit at `<h3>`. Preamble above the first heading becomes a lead child. |
| Forcing `format: "html"` on every ingest | Format follows the group rule; default to markdown (it renders fully styled now). Use HTML only when the rule requires it or the content needs rich layout. |
| Decomposing an ingest against the original file | The group rule may have translated or reformatted the card. Slice the conformed card you created, not the source file, or you reintroduce the wrong language/markup. |
| Acting when zen-mcp isn't wired | If the tool names aren't in your tool list, tell the user and stop. |
