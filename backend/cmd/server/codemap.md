# backend/cmd/server/

## Responsibility
Provides the HTTP API and web UI server for NetRunner. Serves HTMX-rendered HTML pages, REST API endpoints, and WebSocket connections for real-time job log streaming. This is the primary interface for human operators accessing the system through a browser.

## Design
- **Entry Point**: `main.go` - Fiber app setup with middleware, service initialization, route definitions
- **Framework**: Fiber v2 with pongo2 template engine (Jinja2-compatible)
- **Middleware Stack**: recover (panic recovery) → logger → static files
- **Handler Pattern**: Individual handler structs per resource domain
- **Service Initialization**: MusicBrainzService, ArtistTrackingService, ScannerService, WatchlistService, ProfileService
- **WebSocket Manager**: Handles real-time event streaming via `/ws/events` and job console via `/ws/jobs/:job_id`
- **Auth Pattern**: Session-based auth with `AuthMiddleware` protecting most routes

## Flow
1. Load config via `config.Load()`
2. Connect to database via `database.Connect(cfg)`
3. Run migrations via `database.Migrate(db)`
4. Seed default quality profile via `profileService.EnsureDefaultProfile()`
5. Initialize services (MusicBrainz, ArtistTracking, Scanner, Watchlist)
6. Create pongo2 template engine and pre-load templates
7. Initialize Fiber app with template engine
8. Add middleware: recover, logger, static files
9. Initialize handlers (Auth, Dashboard, Stats, Library, Profile, Watchlist, Artists, Schedules)
10. Start WebSocket log listener goroutine
11. Call `setupRoutes()` to register all API and page routes
12. Start HTTP server on port 8080 in goroutine
13. Block on signal handler (SIGINT/SIGTERM) for graceful shutdown

## Integration
- **Depends On**: `internal/config`, `internal/database`, `internal/services`, `internal/api`, `internal/api/templates`
- **External**: Gonic server (music library), Spotify API, MusicBrainz API
- **What Uses It**: Browser-based UI accessed at http://localhost:8080, REST API consumers, WebSocket clients
