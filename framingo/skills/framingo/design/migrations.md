# Migrations run once per release, from the app's own binary

**Decision.** The database service migrates in `Init` when `db.migration.dir`
is set; who sets it is the deployment's choice.

- **Local** (docker compose, one daemon): the daemon's config sets the dir
  and it migrates on start.
- **Prod** (Kubernetes): the daemon's config sets no `db.migration`; a Job
  runs `exampleapp migrate` — the release's own image, the migrations baked
  in — once per release, before the rollout. The Job's name carries the
  version, since a Job cannot change once created.

**Why a separate step in prod.** A failed migration marks the schema dirty;
migrating on start would crash-loop every replica, while a failed Job stops
the release with the old one still serving. Long migrations no longer hold
up readiness, and the app's account can do without DDL. Concurrency is not
the reason — golang-migrate takes a Postgres advisory lock. Old and new
versions overlap during a rollout wherever the migration runs, so
migrations stay backward compatible (expand, then contract).

**Why a subcommand, not the migrate CLI image.** One config, one secret,
one driver registry, one version pin: the official image would need the DSN
and the version restated in the Job. `exampleapp migrate` is a few dozen
lines over the database service's own `Init`.

**Every key is overridable from the environment.** Viper replaces dots with
underscores, so `db.source.host` is `FRAMINGO_EXAMPLE_DB_SOURCE_HOST` — how
the Job and the Deployment inject the database and the migration dir
without a second config file.
