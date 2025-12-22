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

Current CLI commands:

- `stevedore doctor` — Health check
- `stevedore version` — Show version info
- `stevedore repo add <name> <url> --branch <branch>` — Add deployment with SSH key
- `stevedore repo key <name>` — Show public key for deployment
- `stevedore repo list` — List all deployments
- `stevedore param set/get/list` — Manage encrypted parameters
- `stevedore deploy sync <name>` — Git sync via worker container
- `stevedore deploy up <name>` — Deploy via docker compose
- `stevedore deploy down <name>` — Stop deployment
- `stevedore status [name]` — Show deployment/container status

Worker containers:

- Git operations run in isolated `alpine/git` containers for security.
- Worker containers are labeled with `com.stevedore.managed=true` and `com.stevedore.role=git-worker`.
- See `internal/stevedore/git_worker.go` for implementation.

Health monitoring:

- Container health status is checked via `docker inspect`.
- Supports Docker's built-in healthcheck states: healthy, unhealthy, starting, none.
- See `internal/stevedore/health.go` for implementation.

Integration tests (current state):

- Installer integration test is written in Go under `tests/integration/` (build tag: `integration`) and documented in `docs/INTEGRATION_TEST_PLAN.md`.
- Test strategy: start an Ubuntu donor container (`sleep infinity`), mount the checkout read-only, copy it into a work dir, run `./stevedore-install.sh`, then validate via minimal `docker exec` calls.
- All spawned processes must pipe and stream output line-by-line to the test output (no inherited stdio) to keep CI logs readable.
- Tests must best-effort cleanup stale containers (use a predictable prefix like `stevedore-it-`).
- Deployment workflow test exists (`tests/integration/deploy_test.go`) but is skipped in CI due to SSH complexity.
