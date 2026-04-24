# Changelog

All notable changes to this project are documented in this file.

## [0.10.1] - 2026-04-24

### Changed

- Updated the Go toolchain to 1.26 for the module and build container. The
  builder image now pins `golang:1.26.2-alpine`, and the README toolchain
  badge now reflects Go 1.26+.

## [0.10.0] - 2026-04-18

### Added

- **PID-pressure watchdog** - Daemon monitors each managed container's cgroup `pids.current / pids.max` ratio and restarts the deployment before fork() exhaustion. Thresholds configurable via `STEVEDORE_WATCHDOG_WARN_PCT` (default 0.5), `STEVEDORE_WATCHDOG_RESTART_PCT` (default 0.8), `STEVEDORE_WATCHDOG_INTERVAL` (default 30s), and `STEVEDORE_WATCHDOG_MIN_RESTART_GAP` (default 10m). Requires `/sys/fs/cgroup` read-only mount (added to `docker-compose.yml`). Skips the `stevedore` deployment itself.
- **Required `init: true` per service** - Stevedore now refuses to deploy a compose file whose services don't set `init: true`. Error names the offending services and shows how to fix them. Opt out per-service with label `stevedore.init.enforce: "false"` for images that ship their own init. See `docs/INIT.md`.
- **Systemd-aware self-update** - When the installer places a sentinel at `/opt/stevedore/system/managed_by.systemd`, self-update skips the worker+docker-run path and issues `docker kill <containerName>` against the mounted socket. Systemd's `Restart=always` then relaunches the container with the freshly-built `stevedore:latest`. This works whether self-update is called from the daemon (PID 1) or from a `docker exec`-invoked CLI subprocess — the earlier `os.Exit` path only killed the subprocess in the CLI case, leaving the daemon running and the container unrestarted.
- **Periodic watchdog PID-usage summary** - Every Nth sweep (default 10 ≈ 5 min at the default 30 s interval) the watchdog emits a single-line `pid usage` summary per deployment, sorted worst-first. Configurable via `STEVEDORE_WATCHDOG_SUMMARIZE_EVERY`. Gives operators trend visibility without noisy per-sweep logging.

### Changed

- `docker-compose.yml` now bind-mounts `/sys/fs/cgroup:/sys/fs/cgroup:ro` so the watchdog can read per-container PID counters.

### Breaking

- Compose files without `init: true` on every service are rejected. Set `init: true` on each service (recommended) or add the `stevedore.init.enforce: "false"` label to opt out.

## [0.9.1] - 2026-01-26

### Fixed

- **Auto-reconcile for stopped deployments** - Daemon now restarts stopped services that were previously deployed (enabled repos). Manual `deploy down` disables auto-reconcile until `deploy up`.

### Documentation

- Documented the reconcile loop and added integration coverage for auto-restart behavior.

## [0.9.0] - 2026-01-02

### Added

- **Parameter-based ingress configuration** - Configure ingress for services without modifying docker-compose.yaml
  - Use `STEVEDORE_INGRESS_<SERVICE>_*` parameters (e.g., `STEVEDORE_INGRESS_WEB_ENABLED`)
  - Service names normalized: uppercase, dashes → underscores
  - Container labels take precedence over parameters
  - Resolves #9

- **Event notification system** - Real-time change notifications for dependent services
  - Event types: `deployment.created`, `deployment.updated`, `deployment.removed`, `deployment.status_changed`, `params.changed`
  - Enhanced `/poll` endpoint returns event details with `events` array
  - Event bus with configurable history for `EventsSince()` queries
  - Resolves #10

### Changed

- `ListServices()` now checks deployment parameters for ingress config when container labels are absent
- `/poll` endpoint response includes `events` array when changes are detected

### Documentation

- Updated issue #9 with implementation clarifications
- Created issue #10 for change notification system

## [0.8.1] - 2026-01-02

### Fixed

- **Query socket not exposed to host** - Installer now mounts `/var/run/stevedore` directory to the host, allowing client containers to access the query socket
  - Resolves #6

### Documentation

- Added **Client Container Access** section to `docs/QUERY_SOCKET_PROTOCOL.md`
  - Docker Compose configuration example
  - Step-by-step access pattern explanation
  - Token retrieval instructions
  - Resolves #7

## [0.8.0] - 2026-01-02

### Added

- **Query Socket API** - Unix domain socket at `/var/run/stevedore/query.sock` for read-only service queries
  - Endpoints: `/healthz`, `/services`, `/deployments`, `/status/{name}`, `/poll`
  - Per-deployment Bearer token authentication
  - Long-polling support for deployment change notifications
  - CLI commands: `stevedore token get|regenerate|list`
  - See `docs/QUERY_SOCKET_PROTOCOL.md` for full specification
  - Resolves #5

- **Service Discovery** - Label-based service discovery via Docker container labels
  - Services declare ingress routing via `stevedore.ingress.*` labels
  - CLI command: `stevedore services list [--ingress] [--json]`
  - Label schema: `stevedore.ingress.enabled`, `stevedore.ingress.subdomain`, `stevedore.ingress.port`, `stevedore.ingress.websocket`, `stevedore.ingress.healthcheck`
  - Resolves #2

- **Shared Configuration** - Cross-deployment configuration sharing via YAML files
  - Files stored at `/opt/stevedore/shared/{namespace}.yaml`
  - File locking via flock() for concurrent access safety
  - CLI commands: `stevedore shared list|read|write`
  - Resolves #3

- **Integration Tests** - Comprehensive integration tests for query socket API
  - `TestQuerySocketWorkflow`: Full endpoint testing
  - `TestQuerySocketLongPolling`: Long-poll behavior
  - `TestQuerySocketTokenManagement`: Token lifecycle

### Fixed

- **Parameters not passed to docker-compose** - Parameters from `stevedore param set` are now correctly passed as environment variables when running `docker compose up`
  - Resolves #4

- **JSON field naming** - Added proper JSON tags to `DeploymentStatus` and `ContainerStatus` structs for consistent lowercase API responses

### Documentation

- Added `docs/QUERY_SOCKET_PROTOCOL.md` - Complete protocol specification for query socket API
- Updated `docs/INTEGRATION_TEST_PLAN.md` with query socket test coverage
- Added GitHub issue workflow documentation to `CLAUDE.md`

## [0.7.44] - 2026-01-01

- Production deployment on Raspberry Pi (`rp16g`)
- Self-update improvements and documentation
- GitHub deploy key CLI guidance

---

For older releases, see the git history.
