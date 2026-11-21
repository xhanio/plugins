# Configuration: read in Init, reloaded whole

## A setting is constructed or reloadable, and its option.go says which

**Decision.** Every setting of a service is of one class or the other:

- **Construction:** fixed for the instance's life. An option sets it at
  `New`; one the options leave unset takes the built-in default, a constant
  on the package. `Init` never reads or writes it. The wiring may read the
  config to choose one — `db.type`, `db.source.*`, `db.migration.*` — and a
  change to such a key needs a new process.
- **Reloadable:** re-read at every `Init`. Its default is the built-in one as
  an option sets it, fixed at `New` and held on the instance (`defaults`).
  Each `Init` resolves the settings of the start (`settings`) from the
  defaults and the `confutil.Config` in its context, under the root the
  service's name gives (`nameutil.ConfigRoot(m.name)`): where the config sets
  the key it wins, where it does not the default stands, and a value that is
  not one — an interval of zero — falls back to the default. Each reloadable
  setting has one key; `confutil.Config`'s `Get…Or(default, key)` getters read
  it over its default. A setting that belongs to another goes with it: an
  exporter's config block is the config's alone where the config names the
  exporter, never the default exporter's block, and over the default one's
  where it does not — so a config can point the exporter the code chose at
  another collector.

A start is resolved from the defaults and its own config alone, never from
the last start's settings: a key dropped at a reload falls back to the
default. The settings resolved are published whole (below).

An `option.go` puts its options under two comments, `// Construction
options:` and `// Reloadable options:`, a reloadable option's doc naming its
key: "`WithInterval` sets the default of `metrics.interval`". `framingo vet`'s
`optionclass` holds the comments ([checks.md](checks.md)).

**Why.** The config `Init` reads is what a running process may change, not
the whole of what it was built with: a restart does not change the database
a service talks to. An option is an entry, not the semantics, and one
setting with two writers and no order let the last write win: `Init`
overwrote a reloadable setting's option at the first start, so
`tracing.WithSampleRatio` or `db.WithConnection` set in code never held,
and the example read each key twice to pass it to an option `Init` then
replaced. As the default, an option means the same at `New` and at a
reload; resolving from the defaults alone makes a start depend on its config,
never on the starts before it.

**Cost.** A reloadable setting is held twice, its default and its value. The
keys a service reads are not in its constructor's signature: its doc names
them (`db.connection.*` in `pkgs/db.md`). A construction key in a config
file looks as reloadable as any other there.

## A reload is seen whole

**Decision.** What one `Init` reads it publishes whole, so a reader sees the
old configuration or the new, never a mix: under the one lock its readers
take — the example service's greeting, the tracing and metrics managers'
settings — or inside the immutable value a restart swaps in, as
`db.Manager`'s exec timeout travels with its pool.

**Why.** A service restarted alone — by the monitor, or by
`RestartService` — runs its `Init` while its dependents go on serving, so
requests are in flight; SIGHUP's reload stops everything first, but a stop
that passes its deadline leaves handlers behind. Fields rewritten one by one
under a reader are a data race, and a reader caught between two of them sees
a configuration no file ever said.

## The file is re-read on SIGHUP alone

**Decision.** Nothing watches the config file. The example re-reads it only
on SIGHUP: it checks that the file reads — one that does not changes
nothing, and everything goes on running — then stops every service, re-reads
the file, and inits and starts them on it — the init in one pass, whatever
the boot's wait, so a file whose values fail starts what it can. A restart the monitor makes
reads the version last read.

**Why.** A change taking effect whenever the file is written is of little
use; one taking effect when the operator says so is. Stopped first, no
`Init` reads the config while it is rewritten — viper is not safe for that —
and every service starts on one version of the file.

**Cost.** Keys the wiring reads once at startup — `db.source.*`, the api
servers' listeners — still need a process restart; SIGHUP reaches only what
services read in `Init`.
