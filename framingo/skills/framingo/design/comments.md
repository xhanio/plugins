# Comments: what the code cannot say

## A comment says what the code cannot

**Decision.** A comment is written where the code leaves a reader asking, and
says that and no more:

- **An exported name's doc** says what a caller can count on — what it does,
  its default, what it returns when it fails, what may run beside it — never
  how it does it. One sentence, where one will do.
- **A comment in a body** says why: a constraint, the trap the next line
  avoids ("canceling a read's context tears the connection down with no close
  frame"), an order that matters. Never what the next line does.
- **A field's comment** says what it holds or guards, and when it changes:
  "mu guards settings", "fixed at New".
- **An option's doc** says what it sets. A reloadable one names its key and
  its built-in default: "`WithInterval` sets the default of
  `metrics.interval` (built-in: 60s; zero or negative is unset)". Each
  option stands under its class's comment ([config.md](config.md)).
- **An exception** carries its reason on its line: `//nolint:errcheck //
  reason`, `//framingo:ignore <rule> <reason>` ([checks.md](checks.md)).
- **A test's name** is the sentence it holds —
  `TestATracerKeptAcrossARestartFollowsIt` — its comment the case the
  name cannot carry.

A comment uses the code's own names, and a term a design note defines as the
note defines it: construction, reloadable, defaults, settings, a session.

**Why.** A comment the code already says is read twice and kept in step with
it by hand, and the one that is wrong is the one nobody reads. What the code
cannot say — why, what a caller may rely on, what must not change — is what
a reviewer and the next change need.

## A comment points at a decision, never holds one

**Decision.** Reasoning that spans more than the line, or that a reviewer
would argue, is a design note's. The comment names the note —
`(design/config.md)` — and says what its own line needs of it.

**Why.** An argument kept in two places drifts, and only the note is
reviewed as a decision.

## A comment says how it is, never how it was

**Decision.** No history in a comment: no "before", "used to", "now", no
ticket, no name. The commit says what changed and why; a design note's
**Why** says what a decision replaced.

**Why.** A comment is read against the code as it stands, and history only
ages it.

**Cost.** Why a line is not written the obvious way — once the obvious way
was tried — is said as the constraint that rules it out, not as the story
of trying it.
