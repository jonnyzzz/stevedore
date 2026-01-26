# Architecture Notes (Design + Roadmap)

This document captures the current architectural direction and open questions. It is intentionally
forward-looking; not everything here is implemented yet.

## Goals

- **Single host container**: one long-running Stevedore container (systemd preferred; fallback to Docker restart policy when systemd is unavailable).
- **Minimal host dependencies**: “Docker-first”, tested on Ubuntu and Raspberry Pi OS.
- **State on disk**: everything persisted under one host directory (`/opt/stevedore` by default).
- **Resilience**: survives reboots, restarts cleanly, does not take down workloads when Stevedore restarts.
- **Isolation**: run risky operations (git) in short-lived worker containers; compose isolation planned.

## Components

### Host OS

- `stevedore.service` (systemd, preferred): keeps Stevedore running across reboots (otherwise `--restart unless-stopped`).
- Host state directory: `/opt/stevedore` (override via `STEVEDORE_HOST_ROOT` at install time).
- Host CLI:
  - Primary: `stevedore` (wrapper that runs `docker exec`, installed into `PATH` by `stevedore-install.sh`).
  - Compatibility: `stevedore.sh` (same wrapper, legacy name).

### Stevedore control-plane container

- Runs `stevedore -d` as PID 1.
- Mounts:
  - `/opt/stevedore` (host state) → `/opt/stevedore` (container)
  - Docker socket → `/var/run/docker.sock`
- HTTP server on `0.0.0.0:42107` (implemented in v0-3):
  - `/healthz` (unauthenticated): used by systemd health monitoring.
  - `/api/*` (admin-authenticated): status, manual triggers.
  - Admin key generated at install time and stored under `system/admin.key` (see `docs/STATE_LAYOUT.md`).
- Planned: simple web UI (React) served by the same daemon for status + admin operations (v2-0).

### Worker containers

Stevedore avoids running "mutable" or potentially risky operations in the long-running daemon
container when feasible.

- **Git worker** (implemented, default): sync and check operations run in an isolated `alpine/git` container (`internal/stevedore/git_worker.go`).
  - Uses deployment SSH key for authentication
  - Mounts state directory for checkout storage
  - Labels: `com.stevedore.managed=true`, `com.stevedore.role=git-worker`

- **Deploy worker** (planned): run `docker compose` apply steps inside a short-lived container.
  - Currently runs compose from within Stevedore container
  - Future: isolate compose operations in dedicated worker

- **Update worker** (implemented in v0-3): update Stevedore itself by stopping/replacing the running Stevedore container.
  - See `internal/stevedore/self_update.go`
  - Spawns a container to stop old and start new Stevedore
  - Workloads are not stopped during the update

All workers operate via the host Docker socket and the mounted state directory.

## Deployment Model

Each deployment is a Git repository with a Compose file at its root (`docker-compose.yaml` preferred).

### Implemented (v0-2)

Manual deployment cycle via CLI:

1. `stevedore deploy sync <name>` — Clone/fetch repository using git worker container
2. `stevedore deploy up <name>` — Run `docker compose up -d` with project name `stevedore-<name>`
3. `stevedore status <name>` — Check container health status
4. `stevedore deploy down <name>` — Stop deployment

### Implemented (v0-3)

Automated polling cycle:

1. Poll remote repository for changes at configured intervals (git worker).
2. Detect changes by comparing HEAD with last-seen revision.
3. On change: sync → deploy automatically.
4. Validate basic health checks.
5. Persist status + last seen revision in SQLite DB (`sync_status` table).
6. HTTP API for manual triggers and status queries (port 42107).

## Self-Update (implemented in v0-3)

Self-update is special because Stevedore cannot reliably replace itself from inside the container
that is being replaced.

### CLI

```bash
stevedore check stevedore    # Check if updates are available
stevedore self-update        # Trigger self-update
```

### Implementation (`internal/stevedore/self_update.go`)

1. **Sync** the stevedore deployment to get latest changes from Git.
2. **Build** new image with the same tag as current container (e.g., `stevedore:latest`).
3. **Backup** the current image with a timestamped tag for rollback (e.g., `stevedore:backup-1703456789`).
4. **Spawn update worker** (`docker:cli` container) that:
   - Stops the current `stevedore` container
   - Removes the old container
   - Starts a new `stevedore` container from the new image
5. Workloads (deployment containers) are NOT stopped during the update.

### Update Worker Details

The update worker is a short-lived `docker:cli` container that:
- Mounts the Docker socket for docker commands
- Mounts the system directory for reading the update script and env file
- Logs to `/opt/stevedore/system/update.log` for debugging
- Uses `--rm` to auto-remove after completion
- Labeled with `com.stevedore.role=update-worker`

### Why Not Just Exit and Restart?

Docker's restart policy (e.g., `--restart unless-stopped`) uses the same image ID that the
container was originally created with. It does NOT re-pull the tag. So even if we build a new
image with the same tag and exit, Docker restarts with the OLD image.

With systemd (`stevedore.service`), the service runs `docker run` on restart, which DOES look up
the tag and use the latest image. However, we use the update worker approach for consistency
across both systemd and non-systemd environments.

### Rollback

If the new image has problems, manual rollback is possible:
```bash
docker stop stevedore
docker rm stevedore
docker run -d --name stevedore ... stevedore:backup-<timestamp>
```

## Health + Restart Semantics

Current state (v0-3):

- HTTP health endpoint implemented: `GET :42107/healthz` returns `{"status":"ok","version":"..."}`
- systemd restarts Stevedore on crashes.
- Container-level health checks (Docker `HEALTHCHECK`) can call the endpoint.
- Daemon reconcile loop restarts stopped deployments that were previously deployed and still enabled.
  - Interval: `STEVEDORE_RECONCILE_INTERVAL` (default: 30s).
  - `stevedore deploy down` disables auto-reconcile until `stevedore deploy up`.

Remaining work:

- Add container-level `HEALTHCHECK` in Dockerfile that calls `/healthz` (via `curl` or `wget`).
- Add a systemd monitor (or companion mechanism) that restarts the container when health turns
  `unhealthy` (Docker does not restart unhealthy containers by itself).

## Logging (planned)

- Stream logs for Stevedore-managed containers into files under:
  - `/opt/stevedore/deployments/<deployment>/logs/…`
- Censor secrets:
  - never print secret values directly
  - apply a best-effort redaction filter based on known parameter values stored in the encrypted DB
    (with clear limits documented).

## Repository Access Policy (planned)

- Recommended: SSH deploy keys (read-only) per deployment.
- Alternative: HTTPS with tokens (broader scope than deploy keys).
- Future hardening (v4): store SSH keys encrypted in the DB and forward via SSH agent.

## SSH Key Handling (planned, v4)

When supporting private repositories via SSH deploy keys, Stevedore should avoid writing any private
key material to disk (even under `/opt/stevedore`).

Proposed approach:

- Store SSH private keys **encrypted at rest** in the SQLCipher SQLite DB (`system/stevedore.db`).
- Run an SSH agent inside the Stevedore daemon (or implement the SSH agent protocol in-process).
- When starting a git worker container, mount the agent socket and set `SSH_AUTH_SOCK` so Git/SSH can
  authenticate via the agent.
- Load keys into the agent only when required (on-demand) and optionally evict after use.

This keeps keys out of plaintext files and limits key exposure to in-memory usage.

## Container Labels (partial, v3)

Add predictable labels to all created/managed containers. Currently implemented for git workers; other container types are planned.

Examples:

- `com.stevedore.managed=true`
- `com.stevedore.deployment=<name>`
- `com.stevedore.role=control-plane|deploy-worker|git-worker|workload`

This improves observability (`docker ps` / `docker inspect`) and enables safe cleanup.

## Docker Socket vs DinD (research)

Current approach: mount the host Docker socket into the Stevedore container.

DinD is explicitly postponed for now; we should keep the runtime model as simple as possible until
we have a clear need and Raspberry Pi validation.

To research:

- Docker-in-Docker (DinD) for tighter isolation of build/run operations.
- Performance and kernel feature support on Raspberry Pi OS (Pi 5).
- Operational pitfalls (nested daemons, storage drivers, networking).

This is a design decision and should be validated before committing to it.

## CI + Multi-Arch (research)

Questions to validate:

- Can GitHub Actions build and run meaningful tests for `linux/arm64` (and possibly `arm/v7`) via QEMU?
- Do we need a self-hosted runner on a real Raspberry Pi for confidence?
- What is the release artifact strategy (multi-arch images vs “build on host”)?
