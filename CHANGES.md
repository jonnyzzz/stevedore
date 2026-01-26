# Changelog

All notable changes to this project are documented in this file.

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
