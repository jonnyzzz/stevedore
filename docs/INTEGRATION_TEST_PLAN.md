# Integration Test Plan

This document describes the minimal integration-test coverage we want in CI, with a focus on the
host installer (`stevedore-install.sh`).

## Goals

- Keep tests runnable in GitHub Actions (Docker-based).
- Keep tests deterministic and isolated (unique container/image/state names).
- Exercise the real installer flow, not a mocked one.

## How We Test The Installer In CI

Integration tests are Go tests with an explicit build tag to keep `go test ./...` fast and Docker-free.

Implementation: `tests/integration/install_test.go` (build tag: `integration`).

### Donor Container Flow (current)

We test the installer by driving an Ubuntu “donor” container via `docker exec`:

1. Start `ubuntu:22.04` donor container with `sleep infinity`.
2. Mount:
   - host Docker socket (`/var/run/docker.sock`) so the installer can build/run containers
   - project checkout (read-only) into a temp path in the donor (e.g. `/tmp/stevedore-src`)
   - a per-test host state directory into the donor at the same absolute path (so Docker volume mounts resolve on the host)
3. Inside the donor:
   - install the Docker client via `apt` (we do not exercise the “install Docker on the host” branch yet)
   - copy the checkout into a real working directory (e.g. `/work/stevedore`) using system tools (`cp -a`)
   - run `./stevedore-install.sh` from that copied folder
4. Validate using minimal `docker exec` calls:
   - the Stevedore container is running and has the expected restart policy (no systemd path in this test)
   - the host wrapper `stevedore` is in `PATH` and delegates into the running container via `docker exec`
   - basic CLI roundtrips work (repo add/key/list, param set/get)
   - DB is encrypted at rest and a wrong DB key fails
   - no legacy plaintext parameter files are created
5. Cleanup:
   - remove donor + Stevedore containers, remove the test image, delete the test state directory
   - best-effort removal of stale `stevedore-it-*` containers (from interrupted runs)

### Output / Debuggability

All external commands are executed through a small Go runner that pipes stdout/stderr and prints line-by-line
with short prefixes (no inherited stdio), so CI logs remain readable while still being useful for debugging.

### CI Scenarios (must cover)

1. **Executable install path**: `./stevedore-install.sh`
2. **No-systemd host**: installer falls back to Docker restart policy (`--restart unless-stopped`)
3. **Wrapper usability**: `/usr/local/bin/stevedore` can run:
   - `doctor`, `version`
   - `repo add`, `repo list`
   - `param set`, `param get`
4. **Install-time configuration via env** (isolation + non-interactive CI):
   - `STEVEDORE_HOST_ROOT` (state root under `.tmp/…` in tests)
   - `STEVEDORE_CONTAINER_NAME`, `STEVEDORE_IMAGE` (avoid collisions)
   - `STEVEDORE_ALLOW_UPSTREAM_MAIN`, `STEVEDORE_ASSUME_YES` (avoid prompts in CI)
   - `STEVEDORE_BOOTSTRAP_SELF=0` (keep the test focused on installation mechanics)

### What We Assert (pass criteria)

- Installer exits successfully.
- The Stevedore container starts and is reachable via the installed wrapper.
- Container uses the requested image tag and `--restart unless-stopped` (no-systemd fallback).
- State root contains install artifacts (at minimum):
  - `system/db.key` (non-empty)
  - `system/container.env`
- CLI roundtrip works (repo registration + parameter set/get) and `stevedore version` has no `unknown`/URLs.
- DB is not plaintext SQLite (`stevedore.db` header is not `SQLite format 3`) and a wrong DB key fails.
- No legacy plaintext parameter files are created under `deployments/<deployment>/parameters/`.

## Scenarios We Intentionally Do Not Cover In CI (yet)

These are useful, but add complexity and/or require a real systemd host:

- **systemd path**: installation/enabling of `/etc/systemd/system/stevedore.service`
- **Non-root + sudo path**: cases where Docker is only accessible via `sudo`
- **Idempotency**: running the installer twice and verifying state + workloads are preserved

## Recommended Follow-ups (small incremental tests)

- **Idempotency test**: install → create a deployment + parameter → re-run install → parameter still readable.
- **Non-root install test**: create a non-root user in the Ubuntu container, install `sudo`, and verify the
  installer works when Docker access requires escalation.
- **systemd test**: run on a systemd-capable environment (VM job) and assert the unit is installed, enabled,
  and starts the container with the expected mounts/env-file.

## Deployment Workflow Test

Implementation: `tests/integration/deploy_test.go`

This test exercises the full deployment lifecycle using a real SSH Git server sidecar:

### Test Flow

1. **Install Stevedore** via `stevedore-install.sh` in a donor container
2. **Start Git server sidecar**: Ubuntu container with OpenSSH + Git (`tests/integration/git_server_test.go`)
3. **Initialize test repository** with sample app (Dockerfile + docker-compose.yaml with `build:` config)
4. **Register deployment** via `stevedore repo add` (generates SSH key)
5. **Add deploy key** to Git server's authorized_keys
6. **Sync repository** via `stevedore deploy sync` (clones via SSH)
7. **Deploy application** via `stevedore deploy up` (builds image, starts containers)
8. **Verify health** via `stevedore status` (checks container health)
9. **Stop deployment** via `stevedore deploy down`
10. **Cleanup** all containers and images

### Git Server Helper

The `GitServer` struct (`git_server_test.go`) provides a reusable SSH Git server:

- Runs Ubuntu 22.04 with OpenSSH server and Git
- Configures root login with public key authentication
- Seeds repositories using local file protocol (no SSH client needed for setup)
- Provides SSH URL for stevedore to clone: `root@<ip>:/git/<repo>.git`

### Test App

Located in `tests/integration/testdata/simple-app/`:

- `Dockerfile`: Python HTTP server with `/health`, `/version`, `/env` endpoints
- `docker-compose.yaml`: Uses `build:` to test image building, includes healthcheck
- `version.txt`: Version file to verify builds

### What We Assert

- Repository sync succeeds via SSH
- Git checkout exists at correct path
- `docker compose up --build` builds and starts containers
- Containers have correct compose project labels (`com.docker.compose.project=stevedore-<name>`)
- Health status reports correctly
- `deploy down` removes all containers

## Related Integration Coverage

The integration test package (`tests/integration/`) now contains:

1. **TestInstaller_UbuntuDonorContainer**: Installer, CLI, DB encryption
2. **TestDeploymentWorkflow**: Full deployment lifecycle with Git server
3. **TestGitServer_Basic**: Git server helper validation

## Running Locally

- `make test-integration` (requires Docker and outbound network access for `apt` and Docker build downloads)
- `go test -tags=integration ./tests/integration -v -count=1`
