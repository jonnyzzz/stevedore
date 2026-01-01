Review all .md files under the project.

Use IntelliJ for everything where you can, it makes you 100x professional!

You must never ignore warnings or errors, instead, figure out
how to fix that the right way. 

We love tests, and tests must show the reality. A failing test is much must
better than a test that does not check the problems. Make sure you fix tests. 

Check you changes to see if there are no new warnings added to the code. 

Make sure you run compilate and tests when the work is done. 

We follow the industry best practices of Go programming
and DevOps industry. Our goal is to create the industry greatest
example of the tools. Review how people around Docker, RunC, Kubernetes, AWS, GCP
think before moving on with this project. 

We believe documentation is key. We maintain various .md files
to keep the project well documentated. It's part of our job
to update and clean up the .md files to replicate our state of
project understanding. 

We strongly believe in tests and integration tests. Project must
be tested, we should be sure tests are enough to handle. All bugs
are resolved in Test-first-approach. First test is added to reproduce,
next the fix is added to fix the test. Make sure you reproduce the
right problem and your fix fixes that. Tests are higher priority than the code!

Project specifics:

- Keep `README.md` and `docs/*.md` consistent with the actual implementation state.
- Community vs PRO is documentation-level for now (no feature gating in code yet).
- Host interactions should be via `stevedore-install.sh` and the installed `stevedore` wrapper (Docker-first). `stevedore.sh` remains supported.
- Runtime model (for now): mount the host Docker socket into the Stevedore container. DinD is postponed to later versions.
- Installer should prefer a systemd unit (`stevedore.service`) to keep the container running across reboots (fall back to a Docker restart policy if systemd is unavailable).
- Persist state in the SQLCipher-encrypted SQLite DB (`system/stevedore.db`); avoid plaintext secret files (installer generates `system/db.key`).
- Database schema is managed via versioned migrations in `internal/stevedore/db_migrations.go`.
- Repositories use Compose as the entrypoint (`docker-compose.yaml` preferred; fall back to other common names).
- Repository onboarding uses SSH deploy keys (read-only); later we can harden key handling.
- v4 (planned): store SSH private keys encrypted in SQLite and forward them via an SSH agent (no private key files on disk).
- The container service runs the daemon via `stevedore -d`.
- Prefer POSIX `sh` for host scripts; target Ubuntu and Raspberry Pi OS.
- Add integration tests that run Stevedore in Docker; keep them runnable in GitHub Actions.

Database migrations:

- All schema changes are defined as versioned migrations in `internal/stevedore/db_migrations.go`.
- Never modify existing migrations - always add new ones with incrementing version numbers.
- The `schema_migrations` table tracks which migrations have been applied.
- Migrations are applied automatically when `OpenDB()` is called.
- Tests in `internal/stevedore/db_test.go` verify migration correctness and schema integrity.
- `TestMigrations_VersionsAreSequential` ensures migrations are properly numbered.
- `TestMigrations_Idempotent` ensures migrations can run multiple times safely.
- Current migrations: v1 (base schema), v2 (sync_status table), v3 (poll_interval, enabled flag).

Sync status tracking:

- Daemon tracks sync/deploy status in `sync_status` table.
- Fields: last_commit, last_sync_at, last_deploy_at, last_error, last_error_at.
- Per-deployment poll intervals via `repositories.poll_interval_seconds` (default: 300s).
- Deployments can be disabled via `repositories.enabled` flag.
- See `internal/stevedore/sync_status.go` for implementation.

Current CLI commands:

- `stevedore -d` — Run daemon (polling loop + HTTP API)
- `stevedore doctor` — Health check
- `stevedore version` — Show version info
- `stevedore repo add <name> <url> --branch <branch>` — Add deployment with SSH key
- `stevedore repo key <name>` — Show public key for deployment
- `stevedore repo list` — List all deployments
- `stevedore param set/get/list` — Manage encrypted parameters
- `stevedore deploy sync <name>` — Git sync (local git inside container)
- `stevedore deploy up <name>` — Deploy via docker compose (includes parameters as env vars)
- `stevedore deploy down <name>` — Stop deployment
- `stevedore status [name]` — Show deployment/container status
- `stevedore check <name>` — Check for git updates (fetch only)
- `stevedore self-update` — Update stevedore itself
- `stevedore shared list` — List shared config namespaces
- `stevedore shared read <namespace> [key]` — Read shared config (entire namespace or specific key)
- `stevedore shared write <namespace> <key> <value>` — Write to shared config
- `stevedore services list [--ingress] [--json]` — List services (optionally filter by ingress labels)
- `stevedore token get <deployment>` — Get/create query token for deployment
- `stevedore token regenerate <deployment>` — Regenerate query token
- `stevedore token list` — List deployments with query tokens

HTTP API (port 42107):

- `GET /healthz` — Unauthenticated health probe
- `GET /api/status` — List deployments (admin auth)
- `GET /api/status/{name}` — Deployment details (admin auth)
- `POST /api/sync/{name}` — Trigger sync (admin auth)
- `POST /api/deploy/{name}` — Trigger deploy (admin auth)
- `POST /api/check/{name}` — Check for updates (admin auth)
- `POST /api/exec` — Execute CLI command in daemon (admin auth)
- Authentication: `Authorization: Bearer <admin.key>`
- Version headers required: `X-Stevedore-Version`, `X-Stevedore-Build`
- See `docs/API.md` for full reference

Worker containers:

- Worker container implementation exists in `internal/stevedore/git_worker.go` (uses `alpine/git`).
- Current default: Git sync/check runs locally inside the Stevedore container (`GitSyncClean`, `GitCheckRemote`).
- Worker containers are labeled with `com.stevedore.managed=true` and `com.stevedore.role=git-worker`.
- Update worker uses `docker:cli` for self-update operations.

Self-update:

- Stevedore can update itself when the `stevedore` deployment detects new commits.
- Self-update spawns an update worker container to stop/start the control-plane.
- Workload containers are NOT stopped during self-update.
- See `internal/stevedore/self_update.go` for implementation.

Admin key:

- Admin key is generated during installation and stored at `system/admin.key`.
- Used for HTTP API authentication (`Authorization: Bearer <key>`).
- Can be overridden via `STEVEDORE_ADMIN_KEY` env var.
- See `internal/stevedore/admin_key.go` for implementation.

Health monitoring:

- Container health status is checked via `docker inspect`.
- Supports Docker's built-in healthcheck states: healthy, unhealthy, starting, none.
- See `internal/stevedore/health.go` for implementation.

Integration tests (current state):

- Integration tests are written in Go under `tests/integration/` and documented in `docs/INTEGRATION_TEST_PLAN.md`.
- **Do NOT use `//go:build integration` tags** — all tests compile together.
- Test strategy: start an Ubuntu donor container (`sleep infinity`), mount the checkout read-only, copy it into a work dir, run `./stevedore-install.sh`, then validate via minimal `docker exec` calls.
- All spawned processes must pipe and stream output line-by-line to the test output (no inherited stdio) to keep CI logs readable.
- Tests must best-effort cleanup stale containers (use a predictable prefix like `stevedore-it-`).
- GitServer helper (`tests/integration/git_server_test.go`) provides SSH Git server sidecar using Dockerfile.gitserver.
- Deployment workflow test (`tests/integration/deploy_test.go`) exercises full lifecycle with GitServer.

Shared configuration:

- Cross-deployment configuration sharing via `/opt/stevedore/shared/`.
- YAML files organized by namespace: `{namespace}.yaml`.
- File locking via flock() for concurrent access safety.
- Environment variable `STEVEDORE_SHARED` points to this directory in all deployments.
- See `internal/stevedore/shared.go` for implementation.
- Tests in `internal/stevedore/shared_test.go` verify read/write/list operations.

Service discovery:

- Cross-deployment service discovery via Docker container labels.
- Services declare ingress routing via `stevedore.ingress.*` labels.
- Label schema:
  - `stevedore.ingress.enabled=true` — Enable ingress for this service
  - `stevedore.ingress.subdomain=myapp` — Subdomain for routing
  - `stevedore.ingress.port=8080` — Port to route to
  - `stevedore.ingress.websocket=true` — Enable WebSocket support
  - `stevedore.ingress.healthcheck=/health` — Health check path
- CLI: `stevedore services list [--ingress] [--json]`
- See `internal/stevedore/services.go` for implementation.
- Tests in `internal/stevedore/services_test.go` verify label parsing.

Query socket API:

- Unix domain socket at `/var/run/stevedore/query.sock` for read-only service queries.
- Designed for services like stevedore-dyndns to discover other deployments.
- Authentication via `Authorization: Bearer <token>` header (token from `stevedore token get`).
- Endpoints:
  - `GET /healthz` — Health check (no auth required)
  - `GET /services` — List all services
  - `GET /services?ingress=true` — List services with ingress enabled
  - `GET /deployments` — List all deployments
  - `GET /status/{name}` — Get deployment status
  - `GET /poll?since={timestamp}` — Long-poll for deployment changes
- Long-polling: clients can poll `/poll` which blocks until deployment changes occur.
- Database migration v4 adds `query_tokens` table.
- See `internal/stevedore/query_socket.go` and `query_token.go` for implementation.
- Tests in `internal/stevedore/query_socket_test.go` and `query_token_test.go`.

Bug fix requirements:

- All bugs must be fixed using test-first approach.
- First add a failing test that reproduces the bug.
- Then implement the fix to make the test pass.
- Tests must verify the fix works and prevent regression.
- Integration tests are preferred for end-to-end bug verification.

GitHub issue workflow:

- Review issues with `gh issue list` and `gh issue view <number>`.
- For feature requests, ask clarifying questions before implementation:
  - Post questions as issue comments via `gh issue comment`
  - Ask about implementation approach preferences
  - Clarify scope and priorities
  - Wait for responses before proceeding
- When implementing a fix:
  - Create tests first (test-first approach)
  - Implement the fix
  - Update documentation (CLAUDE.md, docs/*.md)
  - Run `go test ./...` to verify all tests pass
- When closing issues, add detailed implementation comments:
  - List all files created/modified
  - Describe code changes with file:line references
  - Explain how the fix works
  - Reference test files that verify the fix
  - Include CLI usage examples if applicable
  - Link to relevant documentation
- Example close comment format:
  ```
  ## Implementation Details

  ### Files Created/Modified:
  - `internal/stevedore/foo.go` - Core implementation
  - `internal/stevedore/foo_test.go` - Unit tests
  - `main.go` - CLI command

  ### How It Works:
  [Explanation of the implementation]

  ### Tests Added:
  [List of test functions and what they verify]

  ### Documentation:
  [Links to updated docs]
  ```
- Use `gh issue close <number>` after implementation is complete and tests pass.
