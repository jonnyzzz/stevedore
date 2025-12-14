# Architecture Notes (Design + Roadmap)

This document captures the current architectural direction and open questions. It is intentionally
forward-looking; not everything here is implemented yet.

## Goals

- **Single host container**: one long-running Stevedore container managed by systemd.
- **Minimal host dependencies**: “Docker-first”, tested on Ubuntu and Raspberry Pi OS.
- **State on disk**: everything persisted under one host directory (`/opt/stevedore` by default).
- **Resilience**: survives reboots, restarts cleanly, does not take down workloads when Stevedore restarts.
- **Isolation**: potentially run risky operations (git, compose) in short-lived worker containers.

## Components

### Host OS

- `stevedore.service` (systemd): keeps Stevedore running across reboots.
- Host state directory: `/opt/stevedore` (override via `STEVEDORE_HOST_ROOT` at install time).
- Host CLI:
  - Current: `stevedore.sh` (wrapper that runs `docker exec`).
  - Planned: `stevedore` (no `.sh`) installed into `PATH` as the primary UX.

### Stevedore control-plane container

- Runs `stevedore -d` as PID 1.
- Mounts:
  - `/opt/stevedore` (host state) → `/opt/stevedore` (container)
  - Docker socket → `/var/run/docker.sock`
- Planned: HTTP server on `0.0.0.0:42107`
  - `/healthz` (unauthenticated): used by systemd health monitoring.
  - `/api/*` (admin-authenticated): status, manual triggers, onboarding.
  - Admin key generated at install time and stored under `system/` (see `docs/STATE_LAYOUT.md`).
- Planned: simple web UI (React) served by the same daemon for status + admin operations.

### Worker containers (planned)

Stevedore should avoid running “mutable” or potentially risky operations in the long-running daemon
container when feasible.

- **Git worker**: clone/poll repositories inside a short-lived container.
- **Deploy worker**: run `docker compose` apply steps inside a short-lived container.
- **Update worker**: update Stevedore itself by stopping/replacing the running Stevedore container
  (self-update without the control-plane container killing itself).

All workers operate via the host Docker socket and the mounted state directory.

## Deployment Model (planned)

Each deployment is a Git repository with a Compose file at its root (`docker-compose.yaml` preferred).

High-level cycle:

1. Poll remote repository for changes (git worker).
2. Prepare a new revision (clone/checkout; build images) without stopping the currently running services.
3. Apply Compose changes (deploy worker).
4. Validate basic health checks.
5. Persist status + last seen revision under `/opt/stevedore`.

## Self-Update (planned)

Self-update is special because Stevedore cannot reliably replace itself from inside the container
that is being replaced.

Proposed flow:

1. Detect Stevedore repo update.
2. Build the new Stevedore image (worker).
3. Start an **update worker** container that:
   - Stops the current `stevedore` container
   - Starts a new `stevedore` container from the new image
4. Ensure workloads (deployment containers) are not stopped; only the Stevedore control-plane is replaced.

## Health + Restart Semantics (planned)

Requirements:

- systemd must restart Stevedore on crashes.
- systemd should also restart Stevedore if the container becomes **unhealthy**.

Proposed approach:

- Add an HTTP health endpoint (`:42107/healthz`) in the daemon.
- Add container-level health checks (Docker `HEALTHCHECK`) that call that endpoint (via `curl`).
- Ensure `curl` is available in the container image to support health probes.
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

- Community (v1): **public HTTPS** repositories only (no credentials).
- PRO (planned): private repositories via SSH Deploy Keys / tokens.

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

## Container Labels (planned, v3)

Add predictable labels to all created/managed containers, e.g.:

- `com.stevedore.managed=true`
- `com.stevedore.deployment=<name>`
- `com.stevedore.role=control-plane|deploy-worker|git-worker|workload`

This improves observability (`docker ps` / `docker inspect`) and enables safe cleanup.

## Docker Socket vs DinD (research)

Current approach: mount the host Docker socket into the Stevedore container.

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
