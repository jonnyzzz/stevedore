# Ingress Configuration

Stevedore provides a flexible ingress system that allows services to declare how they should be exposed to the outside world. This document describes how to configure ingress for your deployments.

## Overview

The ingress system works with companion services like [stevedore-dyndns](https://github.com/jonnyzzz/stevedore-dyndns) to provide:

- **Service Discovery**: Automatic detection of services that need external routing
- **Dynamic DNS**: Updates DNS records when your IP changes
- **HTTPS Termination**: Automatic TLS certificates via Let's Encrypt
- **Reverse Proxy**: Routes traffic to the correct container based on subdomain

## Configuration Methods

### Method 1: Docker Labels (Recommended)

Add labels directly to your `docker-compose.yaml`:

```yaml
services:
  web:
    image: myapp:latest
    ports:
      - "8080:8080"
    labels:
      - "stevedore.ingress.enabled=true"
      - "stevedore.ingress.subdomain=myapp"
      - "stevedore.ingress.port=8080"
      - "stevedore.ingress.websocket=false"
      - "stevedore.ingress.healthcheck=/health"
```

### Method 2: Stevedore Parameters

For services where you cannot modify the docker-compose file (public images, upstream repos), use stevedore parameters:

```bash
# Configure ingress for service "web" in deployment "nginx"
stevedore param set nginx STEVEDORE_INGRESS_WEB_ENABLED true
stevedore param set nginx STEVEDORE_INGRESS_WEB_SUBDOMAIN mysite
stevedore param set nginx STEVEDORE_INGRESS_WEB_PORT 80
stevedore param set nginx STEVEDORE_INGRESS_WEB_HEALTHCHECK /
```

**Service name convention:** Uppercase, dashes converted to underscores.

Example for service `my-api-server`:
```bash
stevedore param set app STEVEDORE_INGRESS_MY_API_SERVER_ENABLED true
stevedore param set app STEVEDORE_INGRESS_MY_API_SERVER_SUBDOMAIN api
stevedore param set app STEVEDORE_INGRESS_MY_API_SERVER_PORT 3000
```

## Configuration Options

| Option | Label | Parameter | Required | Description |
|--------|-------|-----------|----------|-------------|
| Enabled | `stevedore.ingress.enabled` | `STEVEDORE_INGRESS_<SERVICE>_ENABLED` | Yes | Enable ingress (`true`, `1`, `yes`) |
| Subdomain | `stevedore.ingress.subdomain` | `STEVEDORE_INGRESS_<SERVICE>_SUBDOMAIN` | Yes | Subdomain for routing |
| Port | `stevedore.ingress.port` | `STEVEDORE_INGRESS_<SERVICE>_PORT` | Yes | Container port |
| WebSocket | `stevedore.ingress.websocket` | `STEVEDORE_INGRESS_<SERVICE>_WEBSOCKET` | No | WebSocket support |
| Health Check | `stevedore.ingress.healthcheck` | `STEVEDORE_INGRESS_<SERVICE>_HEALTHCHECK` | No | Health check path |

## Priority Rules

When both Docker labels and parameters exist:
1. **Container labels take precedence** - explicit labels override parameters
2. **Parameters as fallback** - applied when container has no ingress labels
3. **Service must be explicit** - no deployment-wide defaults

## Query API

The ingress configuration is exposed via the Query Socket API:

```bash
# List all services with ingress enabled
curl --unix-socket /var/run/stevedore/query.sock \
  -H "Authorization: Bearer $TOKEN" \
  "http://localhost/services?ingress=true"
```

Response:
```json
[
  {
    "deployment": "myapp",
    "service": "web",
    "container_id": "abc123",
    "running": true,
    "ingress": {
      "enabled": true,
      "subdomain": "myapp",
      "port": 8080,
      "websocket": false,
      "healthcheck": "/health"
    }
  }
]
```

## Companion Project: stevedore-dyndns

[stevedore-dyndns](https://github.com/jonnyzzz/stevedore-dyndns) is the recommended ingress controller for Stevedore. It provides:

- **Dynamic DNS**: Updates Cloudflare DNS records with your public IP (IPv4/IPv6)
- **HTTPS Termination**: Wildcard Let's Encrypt certificates via DNS-01 challenge
- **Reverse Proxy**: Caddy-based routing with HTTP/2, HTTP/3, WebSocket support
- **IP Detection**: Automatic detection via Fritzbox TR-064/UPnP or manual override
- **Cloudflare Integration**: Optional proxy mode with mTLS origin protection

### Quick Setup

```bash
# Add stevedore-dyndns as a deployment
stevedore repo add dyndns git@github.com:jonnyzzz/stevedore-dyndns.git

# Configure Cloudflare credentials
stevedore param set dyndns CLOUDFLARE_API_TOKEN "your-token"
stevedore param set dyndns CLOUDFLARE_ZONE_ID "your-zone-id"
stevedore param set dyndns DOMAIN "example.com"
stevedore param set dyndns ACME_EMAIL "[email protected]"

# Deploy
stevedore deploy sync dyndns
stevedore deploy up dyndns
```

### Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        Internet                                  │
│                            │                                     │
│                    ┌───────▼───────┐                            │
│                    │   Cloudflare  │                            │
│                    │  DNS + Proxy  │                            │
│                    └───────┬───────┘                            │
│                            │                                     │
│              *.example.com → Public IP                          │
│                            │                                     │
├────────────────────────────┼────────────────────────────────────┤
│  Host (Raspberry Pi)       │                                     │
│                            │                                     │
│  ┌─────────────────────────▼─────────────────────────────────┐  │
│  │  stevedore-dyndns                                         │  │
│  │  ┌─────────────────┐  ┌────────────────────────────────┐ │  │
│  │  │  IP Detector    │  │  Caddy Reverse Proxy           │ │  │
│  │  │  + DNS Update   │  │  - HTTPS on :443               │ │  │
│  │  └─────────────────┘  │  - Routes by subdomain         │ │  │
│  │                       └────────────────────────────────┘ │  │
│  └───────────────────────────────────────────────────────────┘  │
│                                                                  │
│  ┌───────────────────┐  ┌───────────────────┐                   │
│  │ stevedore-app1    │  │ stevedore-app2    │                   │
│  │ (port 8080)       │  │ (port 3000)       │                   │
│  └───────────────────┘  └───────────────────┘                   │
└──────────────────────────────────────────────────────────────────┘
```

### How dyndns Discovers Services

stevedore-dyndns uses the Query Socket API to discover services:

1. Queries `/services?ingress=true` to get all ingress-enabled services
2. Uses `/poll` for real-time change notifications
3. Generates Caddy configuration based on discovered services
4. Updates DNS records and routes traffic accordingly

### Operational Modes

**Direct Mode** (default):
- DNS points directly to your server
- Let's Encrypt certificates via DNS-01 challenge
- Your IP is visible in DNS

**Cloudflare Proxy Mode** (`CLOUDFLARE_PROXY=true`):
- Traffic routed through Cloudflare edge
- DDoS protection and CDN benefits
- Origin IP hidden, mTLS protection

See the [stevedore-dyndns documentation](https://github.com/jonnyzzz/stevedore-dyndns) for full configuration options.

## Example: Complete Setup

### 1. Deploy stevedore-dyndns

```bash
stevedore repo add dyndns git@github.com:jonnyzzz/stevedore-dyndns.git
stevedore param set dyndns CLOUDFLARE_API_TOKEN "..."
stevedore param set dyndns CLOUDFLARE_ZONE_ID "..."
stevedore param set dyndns DOMAIN "home.example.com"
stevedore param set dyndns ACME_EMAIL "[email protected]"
stevedore deploy sync dyndns
stevedore deploy up dyndns
```

### 2. Deploy a service with labels

```yaml
# my-app/docker-compose.yaml
services:
  web:
    image: nginx:alpine
    labels:
      - "stevedore.ingress.enabled=true"
      - "stevedore.ingress.subdomain=myapp"
      - "stevedore.ingress.port=80"
```

```bash
stevedore repo add myapp git@github.com:you/my-app.git
stevedore deploy sync myapp
stevedore deploy up myapp
```

### 3. Deploy a service with parameters

```bash
# Deploy nginx without modifying docker-compose
stevedore repo add nginx git@github.com:you/nginx-config.git
stevedore deploy sync nginx
stevedore deploy up nginx

# Configure ingress via parameters
stevedore param set nginx STEVEDORE_INGRESS_NGINX_ENABLED true
stevedore param set nginx STEVEDORE_INGRESS_NGINX_SUBDOMAIN www
stevedore param set nginx STEVEDORE_INGRESS_NGINX_PORT 80
```

### 4. Verify

```bash
# Check services are discovered
stevedore services list --ingress

# Access via HTTPS
curl https://myapp.home.example.com
curl https://www.home.example.com
```

## Troubleshooting

### Service not discovered

1. Check container is running: `stevedore status <deployment>`
2. Verify labels: `docker inspect <container> | grep stevedore.ingress`
3. Check parameters: `stevedore param list <deployment>`
4. Query the API: `stevedore services list --ingress --json`

### DNS not updating

1. Check dyndns logs: `docker logs stevedore-dyndns-dyndns-1`
2. Verify Cloudflare credentials: Check API token permissions
3. Check IP detection: Look for IP detection logs

### Certificate issues

1. Check Caddy logs for ACME errors
2. Verify DNS-01 challenge can complete
3. Ensure `ACME_EMAIL` is set correctly

## Related Documentation

- [Query Socket Protocol](QUERY_SOCKET_PROTOCOL.md) - API for service discovery
- [stevedore-dyndns](https://github.com/jonnyzzz/stevedore-dyndns) - Ingress controller implementation
