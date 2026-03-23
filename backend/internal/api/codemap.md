# backend/internal/api/

## Responsibility
Fiber HTTP handler layer. Provides REST API endpoints, HTMX page renderers, partial renderers, WebSocket event streaming, and Spotify OAuth2 integration.

## Design
- Handler structs: `AuthHandler`, `WatchlistHandler`, `LibraryHandler`, `ProfileHandler`, `SchedulesHandler`, `ArtistsHandler`, `StatsHandler`, `DashboardHandler`, `SpotifyAuthHandler`, `WatchlistPreviewHandler`
- Each handler struct holds `*gorm.DB` + relevant service instances
- Auth: session-based via cookies, `AuthMiddleware` validates session → injects user into `c.Locals()`
- Pages: full-page renderers return HTML via Pongo2 templates
- Partials: HTMX partial renderers return HTML fragments for dynamic DOM swaps
- JSON API: RESTful CRUD endpoints returning `fiber.Map` JSON
- WebSocket: `/ws` endpoint with `Htmx-Request` header check, broadcasts job log events via PostgreSQL LISTEN/NOTIFY

## Flow
1. Request → Fiber router → middleware (auth, logger, recover) → handler method
2. Handler: extract user from `c.Locals("user")` → validate input → call service → render response
3. HTMX partials: check `Htmx-Request` header → return HTML fragment
4. WebSocket: upgrade connection → listen for NOTIFY events → push to client

## Integration
- **Consumed by**: `cmd/server` (route registration)
- **Consumes**: `internal/database`, `internal/services`, `internal/api/templates`
- **External**: Spotify OAuth2 flow
