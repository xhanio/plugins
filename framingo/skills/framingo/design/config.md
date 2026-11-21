# Configuration: read in Init, reloaded whole

## Config is read in Init, from the context

**Decision.** A service's constructor takes its dependencies and options;
the configuration it can reload it reads in `Init`, from the
`confutil.Config` the supervisor puts in `Init`'s context
(`confutil.FromContext(ctx)`), under the root its name gives
(`nameutil.ConfigRoot(m.name)`). What `Init` reads replaces what the options
set, whole: a key the config leaves unset falls back to the default, not to
the constructed value.

**Why.** `Init` runs at start and again on every restart, so one path serves
both: a restart is how a changed configuration takes effect, without
rebuilding the service. Options stay for what the wiring fixes and tests set.

**Cost.** The keys a service reads are not in its constructor's signature;
its doc names them (`db.connection.*` in `pkgs/db.md`).

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
