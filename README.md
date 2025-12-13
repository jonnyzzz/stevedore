# Stevedore

**A lightweight, self-managing container orchestration system for Git-driven deployments.**

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Docker](https://img.shields.io/badge/Docker-24+-2496ED?logo=docker&logoColor=white)](https://www.docker.com)

---

A deployment unit is a repository in GitHub. Stevedore watches for changes in the repository
and redeploys repositories once a change is detected. It uses Docker/Docker Compose for deployments. 

Stevedore designed to run on resource-constrained environments like Raspberry Pi while providing the 
GitOps workflow you'd expect from larger orchestration platforms.

Stevedore uses Stevedore to deploy itself. We recommend forking the project to avoid sudden redeploys.

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

## Why Stevedore?

You have a Raspberry Pi (or a small VPS) running a handful of services.
You want deployments to happen automatically when you push to GitHub.
You don't want to run Kubernetes. You don't need Terraform. 
You just want your containers to update when your code does.

Stevedore does exactly that — nothing more, nothing less. Simplicity is key.

## Features

- **Git-driven deployments** — Push to GitHub, containers rebuild and restart automatically
- **Self-managing** — Stevedore updates itself using the same mechanism it uses for your apps
- **Automatic rollback** — Failed deployments roll back to the previous working version
- **Docker and Docker Compose native** — Uses standard Docker stack under the hood

PRO features (donate!):
- **Web UI** — Simple dashboard to monitor container states and trigger manual actions
- **Shared volumes** — Each container receives mounted volumes from the host by default
- **Lightweight** — Runs comfortably on a Raspberry Pi
- **Zero dependencies** — Single Go binary + Docker, that's it

## Quick Start

### Prerequisites

- Docker 24+ with Compose plugin
- Git
- A host running Linux (tested on Ubuntu 22.04+, Raspberry Pi OS)

### Installation

```bash
# Clone Stevedore
git clone git@github.com:jonnyzzz/stevedore.git
cd stevedore

## Build and install stevedore containers to the host OS
./stevedore-install.sh
```

## How It Works

### The Deployment Cycle

```
1. Stevedore polls your Git repositories at configured intervals
2. When changes are detected, a Builder container is spawned
3. Builder clones the repo and runs `docker compose build`
4. On successful build, Stevedore deploys the new containers
5. Health checks run to verify the deployment
6. If healthy → old containers are removed
   If unhealthy → automatic rollback to previous version
```

### Repository Contract

Each managed repository must contain a `stevedore.yaml` file at its root.
This file follows Docker Compose syntax with optional Stevedore-specific extensions:

```yaml
# stevedore.yaml

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

| Variable              | Path on Host                | Description                        |
|-----------------------|-----------------------------|------------------------------------|
| `${STEVEDORE_DATA}`   | `/opt/stevedore/data/{app}` | Per-application persistent storage |
| `${STEVEDORE_SHARED}` | `/opt/stevedore/shared`     | Shared across all applications     |
| `${STEVEDORE_LOGS}`   | `/opt/stevedore/logs/{app}` | Application log directory          |

Use these in your `stevedore.yaml`:

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


## Architecture

```
┌────────────────────────────────────────────────────────────────────┐
│ Host System                                                        │
│                                                                    │
│  systemd: stevedore.service (ensures Stevedore always runs)        │
│                                                                    │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │ Docker                                                       │  │
│  │                                                              │  │
│  │  ┌────────────────────────────────────┐                      │  │
│  │  │ Stevedore Container (~15MB)        │                      │  │
│  │  │ • Git polling                      │                      │  │
│  │  │ • Container management             │                      │  │
│  │  │ • Web UI                           │                      │  │
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
│  /opt/stevedore/         (data, shared, logs, state)               │
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