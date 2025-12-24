# HTTP API Reference

Stevedore exposes an HTTP API on port `42107` for health checks and administrative operations.

## Authentication

All `/api/*` endpoints require authentication using a Bearer token and version headers.

```bash
curl -H "Authorization: Bearer $(cat /opt/stevedore/system/admin.key)" \
     -H "X-Stevedore-Version: 0.7.44" \
     -H "X-Stevedore-Build: abc123def456..." \
     http://localhost:42107/api/status
```

The admin key is generated during installation and stored at `/opt/stevedore/system/admin.key`.

### Version Verification

All authenticated API endpoints require version headers to ensure CLI and daemon binaries match exactly:

| Header | Description |
|--------|-------------|
| `X-Stevedore-Version` | Stevedore version (e.g., `0.7.44`) |
| `X-Stevedore-Build` | Git commit hash of the build |

If versions don't match, the API returns `409 Conflict` with a clear error message. Use `stevedore doctor` to diagnose version mismatches.

## Endpoints

### Health Check

**GET /healthz**

Unauthenticated health probe for systemd or load balancer health checks.

**Response:**
```json
{
  "status": "ok",
  "version": "0.7.44",
  "build": "abc123def456789..."
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

### Check for Updates

**POST /api/check/{name}**

Checks for updates without modifying files. Uses `git fetch` only to compare local and remote commits. Safe to call while deployment is running.

**Response:**
```json
{
  "deployment": "my-app",
  "currentCommit": "abc123def456789...",
  "remoteCommit": "def456789abc123...",
  "hasChanges": true,
  "branch": "main"
}
```

**Status Codes:**
- `200 OK` - Check completed successfully
- `500 Internal Server Error` - Check failed

---

### Execute CLI Command

**POST /api/exec**

Executes a CLI command inside the daemon process. This allows the CLI to delegate commands to the daemon for consistency.

**Request:**
```json
{
  "args": ["status", "my-app"]
}
```

**Response:**
```json
{
  "output": "Deployment: my-app\nProject: stevedore-my-app\n...",
  "exitCode": 0
}
```

If the command fails:
```json
{
  "output": "ERROR: deployment not found\n",
  "exitCode": 1,
  "error": "command failed with exit code 1"
}
```

**Status Codes:**
- `200 OK` - Command executed (check exitCode for result)
- `400 Bad Request` - Invalid request
- `503 Service Unavailable` - Command executor not configured

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
- `409 Conflict` - Version mismatch between CLI and daemon
- `500 Internal Server Error` - Server error

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `STEVEDORE_LISTEN_ADDR` | HTTP server listen address | `:42107` |
| `STEVEDORE_ADMIN_KEY` | Admin key (overrides file) | - |
| `STEVEDORE_ADMIN_KEY_FILE` | Path to admin key file | `system/admin.key` |
