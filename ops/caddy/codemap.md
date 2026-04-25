# ops/caddy/

## Responsibility
Reverse proxy with TLS termination, routing, and WebSocket support for the NETRUNNER web application and Gonic music server.

## Design
| File | Role |
|------|------|
| `Caddyfile` | Caddy v2 configuration for routing, TLS, and proxying |

## Flow
1. Domain configuration with TLS (Let's Encrypt for production, internal CA for localhost)
2. Routes `/ws/*` to `ops-web:8000` with WebSocket headers
3. Routes `/static/*`, `/api/*`, `/` to web application
4. Routes `/music/*` and `/rest/*` to Gonic (Subsonic API)
5. Applies security headers (HSTS, X-Frame-Options, etc.)

## Integration
- **Consumed by**: Docker Compose (`caddy` service)
- **Backend**: Receives traffic from Caddy, serves Fiber app on port 8000
- **Gonic**: Subsonic API accessible via `/music/` and `/rest/`