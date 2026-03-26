# ops/caddy/

## Responsibility
Caddy reverse proxy configuration for production deployments.

## Design
- `Caddyfile` — declarative reverse proxy config
- Routes traffic to the Fiber server (`:8080`) and WebSocket endpoints
- Automatic HTTPS via Caddy's ACME integration
- Static file serving fallback

## Integration
- **Consumed by**: Docker Compose (Caddy service)
- **Proxies to**: `cmd/server` (Fiber on :8080)
