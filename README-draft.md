
## Configuration

### Main Configuration

```yaml
# config.yaml

stevedore:
  poll_interval: 300          # Check repos every 5 minutes
  max_concurrent_builds: 1    # Limit parallel builds (useful for Pi)
  health_check_timeout: 120   # Seconds to wait for healthy status
  keep_versions: 2            # Versions to retain for rollback
  
  web_ui:
    enabled: true
    port: 8080
    # auth:                   # Optional basic auth
    #   username: admin
    #   password_hash: $2a$...

volumes:
  data_root: /opt/stevedore/data
  shared_root: /opt/stevedore/shared
  logs_root: /opt/stevedore/logs

github:
  default_token: ${GITHUB_TOKEN}    # From environment variable

repositories:
  - name: homepage
    url: git@github.com:user/homepage.git
    branch: main

  - name: api-service
    url: git@github.com:user/api.git
    branch: production
    poll_interval: 60               # Override: check every minute

  - name: stevedore
    url: git@github.com:jonnyzzz/stevedore.git
    branch: main
    self: true                      # Marks this as Stevedore itself

notifications:
  webhook: https://hooks.slack.com/services/xxx  # Optional
```

### Environment Variables

| Variable              | Description                                    | Default                      |
|-----------------------|------------------------------------------------|------------------------------|
| `GITHUB_TOKEN`        | GitHub personal access token for private repos | —                            |
| `STEVEDORE_CONFIG`    | Path to config file                            | `/etc/stevedore/config.yaml` |
| `STEVEDORE_LOG_LEVEL` | Logging verbosity (debug, info, warn, error)   | `info`                       |

## Web UI

The management interface provides:

- **Dashboard** — Overview of all managed applications and their status
- **Application details** — Current version, deploy history, logs
- **Manual actions** — Trigger rebuild, rollback, restart
- **Live logs** — Stream build and runtime logs
- **Health status** — Real-time container health monitoring

![Stevedore Web UI](docs/images/web-ui-preview.png)



### Self-Update Process

Stevedore manages its own updates using a carefully orchestrated sequence:

1. Detects changes in its own repository
2. Spawns Builder to create new Stevedore image
3. Starts new Stevedore container with updated image
4. Waits for new container to pass health checks
5. Old container gracefully shuts down
6. If new container fails, it's removed and old one continues

This ensures zero-downtime updates with automatic rollback on failure.

## Deployment Strategies

### Rollback Behavior

Stevedore keeps the previous version of each application ready for instant rollback:

```
/opt/stevedore/state/apps/homepage/
├── current -> v3/        # Active deployment
├── previous -> v2/       # Ready for rollback
├── v3/
│   ├── commit: abc123
│   ├── compose.yaml
│   └── deployed_at: 2024-01-15T10:30:00Z
└── v2/
    └── ...
```

Rollback triggers automatically when:
- Container fails to start
- Health check doesn't pass within timeout
- Application crashes within 60 seconds of deployment

Manual rollback via Web UI or API:
```bash
curl -X POST http://localhost:8080/api/apps/homepage/rollback
```

## API Reference

Stevedore exposes a REST API for automation and integration:

| Endpoint                    | Method | Description                     |
|-----------------------------|--------|---------------------------------|
| `/api/health`               | GET    | Stevedore health status         |
| `/api/apps`                 | GET    | List all managed applications   |
| `/api/apps/{name}`          | GET    | Application details and history |
| `/api/apps/{name}/rebuild`  | POST   | Trigger manual rebuild          |
| `/api/apps/{name}/rollback` | POST   | Rollback to previous version    |
| `/api/apps/{name}/restart`  | POST   | Restart without rebuild         |
| `/api/apps/{name}/logs`     | GET    | Fetch recent logs               |

## Best Practices

### Writing Good Health Checks

Always include health checks in your `stevedore.yaml`. Stevedore relies on them for deployment verification:

```yaml
healthcheck:
  test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
  interval: 10s
  timeout: 5s
  retries: 3
  start_period: 30s    # Grace period for slow-starting apps
```

### Handling Secrets

Never commit secrets to your repository. Instead:

1. Use environment variables in `stevedore.yaml`:
   ```yaml
   environment:
     - DATABASE_URL=${DATABASE_URL}
   ```

2. Configure secrets in Stevedore's config:
   ```yaml
   repositories:
     - name: myapp
       env:
         DATABASE_URL: postgres://user:pass@db:5432/myapp
   ```

3. Or use Docker secrets with the shared volume:
   ```yaml
   volumes:
     - ${STEVEDORE_SHARED}/secrets/myapp:/run/secrets:ro
   ```

### Resource Constraints for Raspberry Pi

Limit concurrent builds and set appropriate timeouts:

```yaml
stevedore:
  max_concurrent_builds: 1
  build_timeout: 900      # 15 minutes for slow ARM builds
  poll_interval: 600      # Less frequent polling saves resources
```

## Troubleshooting

### Container won't start

Check the build and deploy logs:
```bash
docker logs stevedore
# Or via Web UI: Applications → [app] → Logs
```

### Stuck in "Building" state

Builder container may have hung. Check running containers:
```bash
docker ps -a | grep stevedore-builder
docker logs stevedore-builder-{app}
```

Force cleanup:
```bash
curl -X POST http://localhost:8080/api/apps/{name}/cancel-build
```

### Rollback not working

Ensure `keep_versions` is at least 2 and previous deployment succeeded:
```bash
ls -la /opt/stevedore/state/apps/{name}/
```
