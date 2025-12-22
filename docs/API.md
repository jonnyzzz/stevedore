# HTTP API Reference

Stevedore exposes an HTTP API on port `42107` for health checks and administrative operations.

## Authentication

All `/api/*` endpoints require authentication using a Bearer token.

```bash
curl -H "Authorization: Bearer $(cat /opt/stevedore/system/admin.key)" \
     http://localhost:42107/api/status
```

The admin key is generated during installation and stored at `/opt/stevedore/system/admin.key`.

## Endpoints

### Health Check

**GET /healthz**

Unauthenticated health probe for systemd or load balancer health checks.

**Response:**
```json
{
  "status": "ok",
  "version": "0.1.0 (fork@abc123def456 2025-01-15)"
}
```

**Status Codes:**
- `200 OK` - Service is healthy

---

### List Deployments

**GET /api/status**

Lists all deployments with their current status.

**Response:**
```json
{
  "deployments": [
    {
      "deployment": "my-app",
      "healthy": true,
      "message": "All 2 containers healthy",
      "containers": 2,
      "projectName": "stevedore-my-app",
      "lastCommit": "abc123def456",
      "lastSyncAt": "2025-01-15T10:30:00Z",
      "lastDeployAt": "2025-01-15T10:31:00Z"
    }
  ]
}
```

---

### Get Deployment Status

**GET /api/status/{name}**

Gets detailed status for a specific deployment.

**Response:**
```json
{
  "deployment": "my-app",
  "projectName": "stevedore-my-app",
  "healthy": true,
  "message": "All 2 containers healthy",
  "containers": [
    {
      "id": "abc123def456",
      "name": "stevedore-my-app-web-1",
      "service": "web",
      "image": "my-app:latest",
      "state": "running",
      "health": "healthy",
      "status": "Up 2h (healthy)"
    }
  ],
  "lastCommit": "abc123def456",
  "lastSyncAt": "2025-01-15T10:30:00Z",
  "lastDeployAt": "2025-01-15T10:31:00Z"
}
```

---

### Trigger Sync

**POST /api/sync/{name}**

Manually triggers a Git sync for a deployment.

**Response:**
```json
{
  "deployment": "my-app",
  "commit": "abc123def456789...",
  "branch": "main",
  "synced": true
}
```

**Status Codes:**
- `200 OK` - Sync completed successfully
- `500 Internal Server Error` - Sync failed

---

### Trigger Deploy

**POST /api/deploy/{name}**

Manually triggers a deployment.

**Response:**
```json
{
  "deployment": "my-app",
  "projectName": "stevedore-my-app",
  "composeFile": "docker-compose.yaml",
  "services": ["web", "worker"],
  "deployed": true
}
```

**Status Codes:**
- `200 OK` - Deploy completed successfully
- `500 Internal Server Error` - Deploy failed

---

## Error Responses

All errors return JSON with an `error` field:

```json
{
  "error": "deployment not found: my-app"
}
```

**Common Status Codes:**
- `400 Bad Request` - Invalid request (e.g., invalid deployment name)
- `401 Unauthorized` - Missing or invalid authentication
- `405 Method Not Allowed` - Wrong HTTP method
- `500 Internal Server Error` - Server error

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `STEVEDORE_LISTEN_ADDR` | HTTP server listen address | `:42107` |
| `STEVEDORE_ADMIN_KEY` | Admin key (overrides file) | - |
| `STEVEDORE_ADMIN_KEY_FILE` | Path to admin key file | `system/admin.key` |
