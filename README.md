# stevedore

Tiny container management system, very simple, runs on small hardware

## Overview

Stevedore is a lightweight container management system built in Go, designed to run efficiently on small hardware with minimal resource usage.

## Requirements

- Go 1.24 or newer
- Docker (for containerized deployment)
- Docker Compose (optional, for orchestration)

## Building

### Local Build

```bash
go build -o stevedore .
./stevedore
```

### Docker Build

```bash
docker build -t stevedore:latest .
```

### Docker Compose

```bash
docker-compose up -d
```

## Docker Setup

The project includes:
- **Dockerfile**: Multi-stage build using Go 1.24 on Alpine Linux
- **docker-compose.yml**: Orchestration configuration
- **README.docker.md**: Detailed Docker documentation

The container includes Git, Docker CLI, and Docker Compose for full container management capabilities.

## License

See [LICENSE](LICENSE) file for details.

