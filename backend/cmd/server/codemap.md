# backend/cmd/server/

## Responsibility
HTTP server entry point. Initializes Fiber with Pongo2 templates, registers all API routes, WebSocket endpoints, and serves the HTMX-based web UI.

## Design
- Sequential initialization: config → database → migrations → seed profiles → services → Fiber app → routes → graceful shutdown
- Fiber app with `logger` and `recover` middleware
- Pongo2 (Jinja2-compatible) template engine via `internal/api/templates`
- Static file serving from `ops/web/static/`
- WebSocket endpoint at `/ws` for real-time job log streaming
- Route groups: `/auth`, `/api`, page routes (watchlists, libraries, profiles, schedules, artists, jobs)
- Graceful shutdown on SIGINT/SIGTERM with `app.Shutdown()`

## Flow
1. `config.Load()` → `database.Connect()` → `database.Migrate()` → seed default profile
2. Initialize services: MusicBrainz, ArtistTracking, Scanner, Watchlist, Slskd, Gonic, Spotify, etc.
3. Create Fiber app with Pongo2 engine, register template functions
4. Mount static files, auth routes, API routes, page routes, WebSocket
5. `app.Listen(":8080")` → blocks until signal → `app.Shutdown()`

## Integration
- **Consumes**: All `internal/*` packages, `ops/web/` templates and static assets
- **Consumed by**: Web browsers (HTMX UI), API clients, WebSocket consumers
- **External**: Proxies to slskd, Gonic, Spotify, MusicBrainz via services
