# Docker Setup for Stevedore

This document describes the Docker setup for the Stevedore container management system.

## Docker Image

The Dockerfile uses a multi-stage build:

1. **Build Stage**: Uses `golang:1.24-alpine` (latest Go version on small Alpine Linux distro)
2. **Runtime Stage**: Uses `alpine:3.19` (small Linux distribution)

## Installed Tools

The container includes the following tools:

- **Git**: Version control system
- **Docker CLI**: Docker command-line interface
- **Docker Compose**: Tool for defining and running multi-container Docker applications
- **CA Certificates**: SSL/TLS certificates for secure connections

## Building the Image

```bash
docker build -t stevedore:latest .
```

## Running with Docker Compose

```bash
docker-compose up -d
```

## Running Directly

```bash
docker run --rm stevedore:latest
```

## Accessing Docker from Container

To allow the container to manage Docker containers on the host, mount the Docker socket:

```bash
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock stevedore:latest
```

This is already configured in `docker-compose.yml`.

## Notes

- The Alpine Linux base image is chosen for its small size (~5MB)
- The multi-stage build keeps the final image size minimal
- All tools are available via Alpine's package manager (apk)
