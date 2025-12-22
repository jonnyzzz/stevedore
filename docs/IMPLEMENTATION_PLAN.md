# Implementation Plan (Community + PRO roadmap)

This project is intentionally Docker-first: the host installs as little as possible, then Stevedore
runs in a container and keeps all state under a single mounted directory (`/opt/stevedore` by default).

See `docs/ARCHITECTURE.md` for the longer-form design notes and open questions.

## Guiding Principles

- Minimal host dependencies (close to "Docker-only" on Ubuntu / Raspberry Pi OS).
- State is a directory tree on disk (easy backups, easy audits, easy recovery).
- Clear console logs and deterministic behavior.
- Prefer simple, well-tested flows over complex rollback logic at this stage.
- Community and PRO are *documentation-level* concepts for now (no code-level gating yet).

## Definitions

- **Stevedore instance**: one Stevedore installation on a host.
- **Deployment** (deployment unit): one Git repository managed by Stevedore.

## Version Milestones

The implementation follows a versioned milestone approach:

| Version | Name | Status | Description |
|---------|------|--------|-------------|
| v0-1 | Installation | ✅ Done | Raspberry Pi installation via git clone + install script |
| v0-2 | Deployments | ✅ Done | Full deployment lifecycle with Git checkout and Compose |
| v0-3 | Daemon Loop | ✅ Done | Automated polling, HTTP API, self-update |
| v2-0 | Web Monitoring | ⏸️ Postponed | HTTP dashboard with React UI |

---

## v0-1 — Installation (Raspberry Pi)

**Status: ✅ Complete**

The installation workflow for Raspberry Pi and similar hosts:

1. **Clone the Git repository** (required — installer asserts this)
   ```bash
   git clone <your-fork-url> stevedore
   cd stevedore
   ```
2. **Run the install script**
   ```bash
   ./stevedore-install.sh
   ```

The installer performs:
- Asserts running from a Git checkout (Dockerfile and `.git` must exist)
- Installs Docker if missing (Ubuntu/Raspberry Pi OS)
- Creates state directories (`/opt/stevedore/system`, `/opt/stevedore/deployments`)
- Generates database encryption key (`system/db.key`)
- Builds the Stevedore Docker image locally
- Registers as a systemd service (preferred) or uses Docker restart policy
- Installs host wrappers (`stevedore`, `stevedore.sh`) to PATH
- Bootstraps self-deployment for future self-updates

---

## v0-2 — Deployments

**Status: ✅ Complete**

Full deployment lifecycle support:

1. **Add a Git repository** (`stevedore repo add`)
   ```bash
   stevedore repo add my-app git@github.com:user/my-app.git --branch main
   ```
   - Generates an Ed25519 SSH deploy key for the repository
   - Stores the key in the deployment layout (`deployments/<name>/repo/ssh/`)
   - Displays the public key and GitHub deploy key URL (for GitHub repos)
   - User registers the key, then continues

2. **Git sync** (`stevedore deploy sync`) — worker container
   - Spawns a dedicated Docker container (`alpine/git`) for Git operations (isolation)
   - Clones/fetches the repository using the generated SSH key
   - Stores checkout under `deployments/<name>/repo/git/`
   - Returns commit hash and branch info
   - Implementation: `internal/stevedore/git_worker.go`

3. **Compose deployment** (`stevedore deploy up/down`)
   - Searches for entrypoint files (in order):
     - `docker-compose.yaml`, `docker-compose.yml`
     - `compose.yaml`, `compose.yml`
     - `stevedore.yaml` (legacy)
   - Runs `docker compose up -d` with deployment-specific project name (`stevedore-<deployment>`)
   - Labels containers with `com.stevedore.managed=true` and `com.stevedore.deployment=<name>`
   - Implementation: `internal/stevedore/compose.go`

4. **Health monitoring** (`stevedore status`)
   - Lists deployed containers for a deployment
   - Shows container health status (healthy/unhealthy/starting/none)
   - Reports overall deployment health
   - Implementation: `internal/stevedore/health.go`

### CLI Commands Implemented

| Command | Description |
|---------|-------------|
| `stevedore deploy sync <name>` | Sync Git repository to local checkout |
| `stevedore deploy up <name>` | Deploy using docker compose up |
| `stevedore deploy down <name>` | Stop deployment using docker compose down |
| `stevedore status [name]` | Show deployment status and container health |

### v0-2 Integration Tests

- Full deployment workflow test created (`tests/integration/deploy_test.go`)
- Currently skipped in CI due to SSH git server complexity
- TODO: Simplify test to work reliably in GitHub Actions

---

## v0-3 — Daemon Loop

**Status: ✅ Complete**

Automated polling and HTTP API:

1. **Daemon polling loop** (`internal/stevedore/daemon.go`)
   - `stevedore -d` runs a continuous loop with HTTP server
   - Polls registered deployments at configurable intervals (default: 5 minutes)
   - On change detected: sync → deploy automatically
   - Persists last-seen revision and sync status in DB (`sync_status` table)
   - Parallel sync support with per-deployment tracking

2. **HTTP API** (port `42107`) (`internal/stevedore/server.go`)
   - `GET /healthz` — unauthenticated health probe for systemd
   - `GET /api/status` — list all deployments (admin-authenticated)
   - `GET /api/status/{name}` — deployment details (admin-authenticated)
   - `POST /api/sync/{name}` — trigger manual sync (admin-authenticated)
   - `POST /api/deploy/{name}` — trigger manual deploy (admin-authenticated)
   - Admin key generated at install time, stored in `system/admin.key`
   - Bearer token authentication: `Authorization: Bearer <admin-key>`

3. **Self-update workflow** (`internal/stevedore/self_update.go`)
   - Detect changes in the `stevedore` deployment
   - Build new Stevedore image from checkout
   - Spawn update worker container to replace running Stevedore
   - Workload containers are NOT stopped during update

4. **Database migrations** (v2, v3)
   - `sync_status` table: tracks last_commit, last_sync_at, last_deploy_at, last_error
   - `repositories.poll_interval_seconds`: per-deployment poll interval
   - `repositories.enabled`: enable/disable polling

### Implementation Files

| File | Description |
|------|-------------|
| `internal/stevedore/daemon.go` | Daemon loop and polling logic |
| `internal/stevedore/server.go` | HTTP API server |
| `internal/stevedore/admin_key.go` | Admin key management |
| `internal/stevedore/sync_status.go` | Sync status DB operations |
| `internal/stevedore/self_update.go` | Self-update workflow |
| `internal/stevedore/db_migrations.go` | Migrations 2 & 3 |

---

## v2-0 — Web Monitoring (Postponed)

**Status: ⏸️ Postponed for later**

React-based web dashboard:

- HTTP server listening on `0.0.0.0:39851` (or integrated with v0-3 HTTP API)
- Access token authentication (stored in cookies after first entry)
- Dashboard showing:
  - Current deployments and their status
  - Container health information
  - Deployment logs
- Styling similar to devrig.dev

---

## Milestone 0 — Bootstrap (Completed)

1. **Installer**
   - `stevedore-install.sh` supports Ubuntu and Raspberry Pi OS.
   - Installs missing prerequisites (Docker) and asks for `sudo` when required.
   - Builds the Stevedore image locally (`docker build`).
   - Prefers `stevedore.service` (systemd) to keep Stevedore running across reboots (fallback to Docker restart policy when systemd is unavailable).
   - The container runs the daemon via `stevedore -d`.
   - Writes a container env file under `system/container.env`.
   - Installs host wrappers:
     - `stevedore` (primary UX, installed into `PATH`)
     - `stevedore.sh` (compatibility name)
   - Bootstraps a `stevedore` deployment (self-management) when installed from a Git checkout.
   - Planned: “curl | sh” install path for public forks (private forks require manual auth setup).
2. **Fork warning**
   - The running container warns if installed from upstream `github.com/jonnyzzz/stevedore` `main`.
   - README explicitly requires installing from a fork.
3. **State layout**
   - A single state root directory (default `/opt/stevedore`), mounted into the container.
   - Per-deployment folders created on registration.
4. **Repository onboarding**
   - `stevedore repo add …` registers a repo and generates an SSH deploy key (`stevedore.sh` also works).
   - The tool prints the public key and instructions to add it as a read-only Deploy Key.
5. **Parameters store (secrets)**
   - Parameters stored in a local SQLite database under `system/stevedore.db`.
   - Database is encrypted at rest using SQLCipher.
   - Database password is generated by the installer and stored at `system/db.key` (and must be backed up).
6. **CI**
   - Integration tests run in Docker (driven by Go integration tests: `go test -tags=integration`).
   - GitHub Actions runs unit + integration tests automatically.
7. **Build metadata**
   - Docker build embeds: `VERSION`, git remote (sanitized), git commit, UTC build date.
   - Exposed via `stevedore version`.

## Milestone 1 — Status + HTTP API (Community)

- Add `stevedore status` to list deployments and their last known state.
- Add an HTTP server to the daemon (planned port `42107`) with:
  - unauthenticated `/healthz` (for service monitoring)
  - admin-authenticated status + manual triggers
  - admin-authenticated repository registration
- Installer generates and persists an admin key under `system/` and injects it into the container.
- Ensure there is always a `stevedore` deployment (self-management baseline).

## Milestone 2 — Git sync loop (Community)

- Poll interval + scheduler.
- Run Git operations in a dedicated worker container (isolation).
- Use the generated deploy key for SSH Git access (read-only).
- v4 hardening: store private keys encrypted in the DB and forward via SSH agent (no key files on disk).
- Persist last-seen revision and sync status in the state directory.
- Manual sync trigger via CLI + HTTP API.

## Milestone 3 — Deploy engine (Community)

- Repository contract:
  - `docker-compose.yaml` at repo root (recommended); also support `docker-compose.yml`, `compose.yaml`, `compose.yml`.
  - `stevedore.yaml` remains supported as a legacy name.
- Standard mounts / env:
  - Provide `${STEVEDORE_DATA}`, `${STEVEDORE_LOGS}`, `${STEVEDORE_SHARED}` to Compose and back them by predictable host paths.
- Build:
  - Build images with deterministic tags derived from commit SHA.
- Deploy:
  - Apply compose in a predictable way (project naming per deployment).
  - **Rollback logic stays simple**: prefer “don’t take down the current version unless the new one is healthy”.
- Run `docker compose` applies in a dedicated worker container (isolation).
- Stream workload container logs into `/opt/stevedore/deployments/<deployment>/logs/…` with best-effort secret redaction.

## Milestone 4 — Self-update + resilience (Community)

- Self-update workflow:
  - detect Stevedore repo changes
  - build a new Stevedore image
  - use an update worker container to stop/replace the running Stevedore control-plane container
  - do not stop workload containers during Stevedore restarts
- Health monitoring:
  - container exposes `/healthz` on `:42107`
  - systemd restarts Stevedore if it becomes unhealthy (Docker does not restart unhealthy containers by itself)

## Milestone 5 — Observability + operations (Community)

- Structured logs (console first, then UI).
- State introspection commands (status, last errors, last deploy revision).
- Backup / restore guidance (`tar` the state directory).
- Container labels (v3): label all Stevedore-managed containers to make `docker ps`/`docker inspect` readable.

## PRO Roadmap (documentation-level)

- Web UI (React) (logs viewer + manual triggers).
- Advanced rollbacks (multiple versions, retention).
- Notifications (Slack/Webhooks).
- AuthN/AuthZ for UI and API.
- External secrets backends (SOPS/Vault/Cloud secret managers).
- Multi-arch release pipelines (arm64/armv7) and Raspberry Pi validation.
