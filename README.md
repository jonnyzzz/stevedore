# Stevedore

**A lightweight, self-managing container orchestration system for Git-driven deployments.**

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Docker](https://img.shields.io/badge/Docker-24+-2496ED?logo=docker&logoColor=white)](https://www.docker.com)

---

Stevedore runs as a single container on a small host (Ubuntu / Raspberry Pi OS).
A **deployment** (deployment unit) is a Git repository. Stevedore watches for changes and redeploys
that repository using Docker / Docker Compose.

Stevedore is designed for resource‑constrained environments while keeping the operational model
simple and inspectable (state on disk, clear logs).

Stevedore can deploy itself, but you should fork first.

```
┌─────────────────────────────────────────────────────────────┐
│                                                             │
│   GitHub          Stevedore            Your Services        │
│                                                             │
│   ┌─────┐         ┌─────────┐          ┌─────┐  ┌─────┐     │
│   │repo1│ ──────▶ │         │ ───────▶ │app1 │  │app2 │     │
│   └─────┘   poll  │  ○ ○ ○  │  deploy  └─────┘  └─────┘     │
│   ┌─────┐         │         │          ┌─────┐  ┌─────┐     │
│   │repo2│ ──────▶ │         │ ───────▶ │app3 │  │app4 │     │
│   └─────┘         └─────────┘          └─────┘  └─────┘     │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

## Fork First (Required)

If you deploy Stevedore from `github.com/jonnyzzz/stevedore` `main`, a `git pull` or force‑push in
the upstream repo can unexpectedly redeploy your host.

- Fork the repository and install from your fork.
- Stevedore warns at startup if it detects it was installed from the upstream `main`.

## Project Status

Stevedore is early-stage. The current focus is:

- A minimal host installation (`./stevedore-install.sh`) for Ubuntu and Raspberry Pi OS
- A systemd service (`stevedore.service`) to keep Stevedore running across reboots (fallback to `--restart unless-stopped` when systemd is unavailable)
- A host wrapper (`stevedore`) that configures the running container via `docker exec` (`stevedore.sh` also works)
- A file-backed state layout under a single mounted volume (`/opt/stevedore` by default)
- Repository onboarding via generated SSH deploy keys
- A parameters store in a SQLCipher-encrypted SQLite database (`/opt/stevedore/system/stevedore.db`)
- Build metadata embedded at build time (`VERSION` + git info, see `stevedore version`)

See `docs/IMPLEMENTATION_PLAN.md` and `docs/ARCHITECTURE.md` for concrete milestones and scope.

## Why Stevedore?

You have a Raspberry Pi (or a small VPS) running a handful of services.
You want deployments to happen automatically when you push to GitHub.
You don't want to run Kubernetes. You don't need Terraform. 
You just want your containers to update when your code does.

Stevedore does exactly that — nothing more, nothing less. Simplicity is key.

## Features

Community (implemented):

- **Installer + wrapper** — `./stevedore-install.sh` + `stevedore` (Docker-first)
- **State on disk** — Everything under a single host directory (`/opt/stevedore` by default)
- **Repo onboarding** — Generates an SSH deploy key per repository with GitHub URL suggestion
- **Parameters store** — SQLCipher-encrypted SQLite database + install-generated key
- **Git sync** — Git operations run in isolated `alpine/git` worker containers with optional cleanup (`stevedore deploy sync`)
- **Compose deployment** — Deploy via Docker Compose with health monitoring (`stevedore deploy up/down`)
- **Status + health** — Container health monitoring (`stevedore status`)
- **Upstream main warning** — Warns if installed from `jonnyzzz/stevedore` `main`
- **Build metadata** — `VERSION` + git info embedded into the binary/image

Community (implemented — v0-3):

- **Daemon polling loop** — Automated Git sync and deployment on changes
- **HTTP API** — Health endpoint (`/healthz`) and admin API on port `42107`
- **Self-update** — Stevedore updates itself via worker container

PRO (planned, documentation only for now):

- **Web UI** (PRO) — Dashboard and logs viewer
- **Advanced rollback** (PRO) — Multiple versions, retention policies
- **Notifications** (PRO) — Slack/Webhooks
- **Multi-user** (PRO) — AuthN/AuthZ for UI and API

## Quick Start

### Prerequisites

- Docker 24+
- Docker Buildx (optional; installer falls back to legacy builder when unavailable)
- Git
- A host running Linux (tested on Ubuntu 22.04+, Raspberry Pi OS)

### Installation

```bash
# Clone your fork
git clone https://github.com/<you>/stevedore.git
cd stevedore

# Build and install Stevedore to the host OS
./stevedore-install.sh

# Configure / operate Stevedore (installed by the script)
stevedore doctor
```

Planned (public forks): a one-line installer (`curl | sh`). Target UX:

```bash
curl -fsSL https://raw.githubusercontent.com/<you>/stevedore/<ref>/stevedore-install.sh | sh
```

If the repository is not accessible without authorization, the installer should fail fast and ask
you to fix access first.

`stevedore-install.sh` installs the host wrapper `stevedore` (default: `/usr/local/bin/stevedore`).
All configuration and operations are done by running `stevedore …`, which executes the `stevedore`
binary inside the running container via `docker exec`.

For compatibility, `stevedore-install.sh` also installs `stevedore.sh`.

The installer prefers systemd and installs/enables `stevedore.service` when available, which runs a
single container named `stevedore` across reboots. On hosts without systemd, the installer starts
the container with a Docker restart policy (`--restart unless-stopped`).

If the installer detects it is running from a Git checkout, it also bootstraps a `stevedore`
deployment and prints an SSH Deploy Key for your fork (read-only).

### Prepare a Repository (Service)

Before you register a deployment, make sure the repository is ready to run under Docker Compose:

- Add a Compose file at repo root (`docker-compose.yaml` preferred).
- Define a `services` entry with `build:` or `image:` (or both).
- Add a healthcheck if you want `stevedore status` to report healthy/unhealthy.
- Use the Stevedore-provided volume env vars (`STEVEDORE_DATA`, `STEVEDORE_LOGS`, `STEVEDORE_SHARED`).
- Keep secrets out of git; use `stevedore param set` or environment variables.

### Add Your First Repository

```bash
# Register the repository (generates SSH deploy key)
stevedore repo add homepage git@github.com:<you>/homepage.git --branch main

# Show the public key (add to GitHub as Deploy Key)
stevedore repo key homepage
```

Add the deploy key as read-only (GitHub CLI):

```bash
gh api -X POST repos/<you>/homepage/keys \
  -f title="stevedore-homepage" \
  -f key="$(stevedore repo key homepage)" \
  -F read_only=true
```

Use `-F read_only=true` so the API treats the value as a boolean.

```bash
# Sync the repository (clones via worker container)
stevedore deploy sync homepage

# Deploy the application
stevedore deploy up homepage

# Check deployment status
stevedore status homepage

# Check for updates (git fetch only, safe while running)
stevedore check homepage

# Stop the deployment
stevedore deploy down homepage
```

### Sync Options

```bash
# Sync with stale file cleanup (default)
stevedore deploy sync homepage

# Sync without removing stale files
stevedore deploy sync homepage --no-clean
```

### Self-Update (Bootstrap Mode)

When Stevedore is installed with self-bootstrap mode (managing its own repository), it can update itself:

```bash
# Check for updates to Stevedore itself
stevedore check stevedore

# Trigger self-update (syncs, builds new image, replaces container)
stevedore self-update
```

The self-update workflow:
1. Syncs the `stevedore` deployment to get latest changes
2. Builds a new Docker image from the updated code
3. Spawns an update worker container
4. Worker stops the running Stevedore, starts a new one with the new image
5. Workload containers are **not affected** during the update

Add the printed public key to your repo as a **read-only Deploy Key**.
See `docs/REPOSITORIES.md`.

### Secrets / Parameters

Stevedore keeps configuration parameters (including secrets) in a local SQLCipher-encrypted SQLite database:

`/opt/stevedore/system/stevedore.db`

The database key is generated during installation and stored at:

`/opt/stevedore/system/db.key`

Use `stevedore param set/get/list` to manage them. See `docs/SECRETS.md`.

## How It Will Work (Target)

Stevedore is early-stage; this section describes the intended runtime model.

- The Stevedore container runs the daemon as `stevedore -d`.

### The Deployment Cycle

```
1. Stevedore polls your Git repositories at configured intervals
2. When changes are detected, a Builder container is spawned
3. Builder clones the repo and runs `docker compose build`
4. On successful build, Stevedore deploys the new containers
5. Health checks run to verify the deployment
6. If healthy → old containers are replaced
   If unhealthy → deployment is aborted (rollback is intentionally simple for now)
```

### Repository Contract

Each managed repository must contain a Docker Compose file at its root.

Stevedore looks for these files (in order):

1. `docker-compose.yaml` (recommended)
2. `docker-compose.yml`
3. `compose.yaml`
4. `compose.yml`
5. `stevedore.yaml` (legacy / backwards compatible)

The file follows Docker Compose syntax with optional Stevedore-specific extensions:

```yaml
# docker-compose.yaml

services:      ### usual docker compose below, it builds the application container!
  web:
    build: .
    ports:
      - "3000:3000"
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:3000/health"]
      interval: 10s
      timeout: 5s
      retries: 3
    volumes:
      - ${STEVEDORE_DATA}:/data    # Automatically provided

  worker:
    build: ./worker
    depends_on:
      - web

# Optional: Stevedore-specific configuration
x-stevedore:
  health_check_timeout: 120
  build_timeout: 600
```

### Shared Volumes

Stevedore provides a standard volume mount to every container.
This allows your services to persist data and share files 
without manual volume configuration:

| Variable              | Path on Host                                   | Description                       |
|-----------------------|------------------------------------------------|-----------------------------------|
| `${STEVEDORE_DATA}`   | `/opt/stevedore/deployments/{deployment}/data` | Per-deployment persistent storage |
| `${STEVEDORE_SHARED}` | `/opt/stevedore/shared`                        | Shared across all applications    |
| `${STEVEDORE_LOGS}`   | `/opt/stevedore/deployments/{deployment}/logs` | Per-deployment log directory      |

Use these in your `docker-compose.yaml`:

```yaml
services:
  myapp:
    volumes:
      - ${STEVEDORE_DATA}:/app/data
      - ${STEVEDORE_SHARED}/certs:/certs:ro
      - ${STEVEDORE_LOGS}:/var/log/myapp
```

### Application Logs

The stdout and stderr of all containers is automatically tracked and
persisted under the container logs of the service. 

In the PRO version we allow checking the logs via Web UI together with
logs retention. 

## Documentation

- `docs/IMPLEMENTATION_PLAN.md`
- `docs/STATE_LAYOUT.md`
- `docs/REPOSITORIES.md`
- `docs/SECRETS.md`
- `docs/INTEGRATION_TEST_PLAN.md`

## Architecture

```
┌────────────────────────────────────────────────────────────────────┐
│ Host System                                                        │
│                                                                    │
│  systemd: stevedore.service (preferred; otherwise Docker restart)  │
│                                                                    │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │ Docker                                                       │  │
│  │                                                              │  │
│  │  ┌────────────────────────────────────┐                      │  │
│  │  │ Stevedore Container (~15MB)        │                      │  │
│  │  │ • Git polling                      │                      │  │
│  │  │ • Container management             │                      │  │
│  │  │ • Web UI (PRO, planned)            │                      │  │
│  │  │ • Health monitoring                │                      │  │
│  │  └─────────────┬──────────────────────┘                      │  │
│  │                │                                             │  │
│  │                ▼ spawns                                      │  │
│  │  ┌────────────────────────────────────┐                      │  │
│  │  │ Builder Container (ephemeral)      │                      │  │
│  │  │ • git clone/pull                   │                      │  │
│  │  │ • docker compose build             │                      │  │
│  │  │ • Image tagging                    │                      │  │
│  │  └────────────────────────────────────┘                      │  │
│  │                                                              │  │
│  │  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐             │  │
│  │  │ App 1   │ │ App 2   │ │ App 3   │ │ App N   │             │  │
│  │  │ v2 ✓    │ │ v5 ✓    │ │ v1 ✓    │ │ vM ✓    │             │  │
│  │  └─────────┘ └─────────┘ └─────────┘ └─────────┘             │  │
│  │                                                              │  │
│  └──────────────────────────────────────────────────────────────┘  │
│                                                                    │
│  Volumes:                                                          │
│  /var/run/docker.sock    (Docker API)                              │
│  /opt/stevedore/         (system, deployments, shared, logs)       │
└────────────────────────────────────────────────────────────────────┘
```

## Contributing

Contributions are welcome! Please read our [Contributing Guide](CONTRIBUTING.md) before submitting a PR.

## License

Stevedore is released under the [Apache License 2.0](LICENSE).

```
Copyright 2025 Eugene Petrenko

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
```

## Acknowledgments

- The name "Stevedore" refers to dock workers who load and unload cargo — fitting for a tool that moves containers.
- Built with [Go](https://go.dev), [Docker](https://www.docker.com), and stubbornness.

---

<p align="center">
  <i>Stevedore: Because your Raspberry Pi deserves GitOps too.</i>
</p>
