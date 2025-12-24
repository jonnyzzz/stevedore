# Repository Monitoring Service (Status)

This document tracks the monitoring flow (check-only + sync-clean + CLI/API).

## Current Implementation (v0-3)

- Git check-only operation (`GitCheckRemote`) uses `git fetch` and compares `HEAD` vs `FETCH_HEAD`.
- Clean sync (`GitSyncClean`) removes stale files by default; `--no-clean` disables cleanup.
- CLI command `stevedore check <deployment>` is implemented.
- Daemon poll loop uses check-only first, then sync + deploy when changes are detected.
- HTTP API includes `POST /api/check/{name}` and requires version headers.
- Version mismatches return `409 Conflict` (strict matching).
- Integration coverage: `tests/integration/monitoring_test.go`.

## Not Implemented / Future Ideas

- `stevedore check --all` and `stevedore check --sync`.
- Git client extraction (`internal/stevedore/gitclient`) to isolate git operations for unit tests.
- Worker-container isolation for all git operations (current default uses local git inside the Stevedore container).

## References

- `internal/stevedore/git_worker.go`
- `internal/stevedore/daemon.go`
- `internal/stevedore/server.go`
- `main.go`
- `tests/integration/monitoring_test.go`
