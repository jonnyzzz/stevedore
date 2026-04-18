# Init Process Enforcement

Stevedore requires every Compose service it deploys to run with Docker's
init process (`init: true`). Deploys that don't are rejected with a clear
error before any container is started.

## Why

Without an init process, PID 1 inside the container is your application
itself. Applications typically don't reap orphaned child processes, so
orphans pile up as zombies. Once the container's cgroup `pids.max` is
reached, every `fork()` — including execing tools, subprocesses, and even
the container's own healthcheck — fails with `EAGAIN`. The service appears
up to the orchestrator but cannot do any work.

This has bitten production deployments of stevedore more than once; see
the `lg-tv-bot` incident where ~19,000 zombie `ping` processes exhausted a
container's PID slots.

Setting `init: true` tells Docker to use `tini` as PID 1. Tini reaps
orphans and forwards signals, so the problem simply does not occur.

## Required: set `init: true` on every service

```yaml
services:
  web:
    image: my/web:latest
    init: true      # required — stevedore refuses to deploy without this

  worker:
    image: my/worker:latest
    init: true
```

## Error when missing

If any service lacks `init: true`, `stevedore deploy up` fails:

```
ERROR: init: true is required on every service — missing in: bot, worker

Fix the compose file by adding `init: true` to each listed service, e.g.:

  services:
    bot:
      init: true
...
```

Nothing is started until the compose file is fixed.

## Opt-out: `stevedore.init.enforce=false`

Some images already ship their own init (e.g. `s6-overlay`, a custom
`tini` entrypoint, or a supervisor). In those cases the enforced
`init: true` is redundant — and sometimes conflicts with the in-image
init. Opt out per-service with the `stevedore.init.enforce` label:

```yaml
services:
  supervisord-image:
    image: my/s6-based-image:latest
    labels:
      stevedore.init.enforce: "false"   # skip stevedore's init check
    # init: true is NOT required here
```

The label value is case-insensitive. Any value other than `"false"`
(or the label being absent) leaves enforcement on.

Use this sparingly — services that opt out must reap their own orphans.

## Relationship to the PID-pressure watchdog

Stevedore also runs a watchdog that monitors each container's cgroup
`pids.current / pids.max` ratio and restarts a deployment if it climbs
past the configured threshold. The watchdog is a safety net for services
that still leak despite having init; it is not a substitute for running
init. Keep both.
