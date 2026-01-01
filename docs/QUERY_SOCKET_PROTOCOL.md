# Query Socket Protocol Specification

This document describes the protocol for the stevedore query socket API, which allows deployments to discover other services and deployments.

## Overview

The query socket provides a read-only HTTP API over a Unix domain socket. It is designed for use cases like:
- Ingress controllers discovering services to route traffic
- Monitoring systems discovering deployments
- Service mesh components discovering endpoints

## Socket Location

Default: `/var/run/stevedore/query.sock`

Configurable via `STEVEDORE_QUERY_SOCKET` environment variable or daemon config.

## Authentication

All endpoints (except `/healthz`) require authentication via Bearer token:

```
Authorization: Bearer <token>
```

Tokens are per-deployment and can be managed via CLI:
- `stevedore token get <deployment>` - Get/create token
- `stevedore token regenerate <deployment>` - Regenerate token
- `stevedore token list` - List deployments with tokens

## Endpoints

### GET /healthz

Health check endpoint. No authentication required.

**Response:**
```json
{
  "status": "ok"
}
```

### GET /services

List all services managed by stevedore.

**Query Parameters:**
- `ingress=true` - Filter to only services with ingress labels enabled

**Response:**
```json
[
  {
    "deployment": "homepage",
    "service": "web",
    "container_id": "abc123def456",
    "container_name": "stevedore-homepage-web-1",
    "running": true,
    "ingress": {
      "enabled": true,
      "subdomain": "www",
      "port": 8080,
      "websocket": false,
      "healthcheck": "/health"
    }
  }
]
```

### GET /deployments

List all deployments.

**Response:**
```json
[
  {"name": "homepage"},
  {"name": "api"},
  {"name": "dyndns"}
]
```

### GET /status/{name}

Get detailed status for a specific deployment.

**Response:**
```json
{
  "deployment": "homepage",
  "project_name": "stevedore-homepage",
  "healthy": true,
  "message": "All 2 containers healthy",
  "containers": [
    {
      "id": "abc123def456",
      "name": "stevedore-homepage-web-1",
      "service": "web",
      "image": "homepage:latest",
      "state": "running",
      "health": "healthy",
      "status": "Up 2h (healthy)",
      "exit_code": 0,
      "started_at": "2026-01-01T20:00:00Z"
    }
  ]
}
```

### GET /poll

Long-polling endpoint for deployment changes. Blocks until a change occurs or timeout (60s).

**Query Parameters:**
- `since={unix_timestamp}` - Only return if changes occurred after this time

**Response (change detected):**
```json
{
  "changed": true,
  "timestamp": 1735772400
}
```

**Response (timeout, no change):**
```json
{
  "changed": false
}
```

## Ingress Labels

Services can declare ingress routing via Docker labels in docker-compose.yaml:

```yaml
services:
  web:
    labels:
      - "stevedore.ingress.enabled=true"
      - "stevedore.ingress.subdomain=myapp"
      - "stevedore.ingress.port=8080"
      - "stevedore.ingress.websocket=true"
      - "stevedore.ingress.healthcheck=/health"
```

| Label | Type | Description |
|-------|------|-------------|
| `stevedore.ingress.enabled` | bool | Enable ingress discovery (true/1/yes) |
| `stevedore.ingress.subdomain` | string | Subdomain for routing |
| `stevedore.ingress.port` | int | Container port to route to |
| `stevedore.ingress.websocket` | bool | Enable WebSocket support |
| `stevedore.ingress.healthcheck` | string | Health check path |

## Example: Ingress Controller Integration

A deployment like `stevedore-dyndns` can use the query socket to discover services:

```yaml
# docker-compose.yaml for dyndns
services:
  dyndns:
    image: jonnyzzz/stevedore-dyndns
    volumes:
      - /var/run/stevedore/query.sock:/var/run/stevedore/query.sock:ro
    environment:
      - STEVEDORE_QUERY_TOKEN=${STEVEDORE_QUERY_TOKEN}
```

The dyndns service can then:
1. Query `GET /services?ingress=true` to get all services
2. Use `GET /poll` to wait for changes
3. Automatically update routing when deployments change

## Error Responses

| Status | Description |
|--------|-------------|
| 401 | Missing or invalid Authorization header |
| 404 | Resource not found |
| 405 | Method not allowed |
| 500 | Internal server error |

## Security Considerations

1. **Read-only access**: The query socket only provides read access; no write operations are supported.
2. **Token isolation**: Each deployment has its own token; compromising one doesn't affect others.
3. **Socket permissions**: Socket is created with mode 0666 for container access.
4. **No sensitive data**: Parameters and secrets are never exposed via this API.
