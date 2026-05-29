# backend/internal/api/

## Responsibility
HTTP API layer for NETRUNNER music acquisition pipeline. Provides both server-rendered HTML pages for initial loads and JSON APIs for programmatic access. Uses HTMX for partial page updates and WebSockets for real-time job log streaming. All endpoints use session-based authentication with cookie management.

## Design

### Handler Organization
Each domain feature has its own handler file with dedicated structs:
- **AuthHandler** - Session-based auth with bcrypt, cookie management
- **DashboardHandler** - Optional-auth index page with aggregate stats
- **WatchlistHandler** - Watchlist CRUD via WatchlistService
- **LibraryHandler** - Library management with job triggering (scan/enrich)
- **ProfileHandler** - Quality profile CRUD with default handling
- **SchedulesHandler** - Cron-based schedule management
- **ArtistsHandler** - Monitored artists via ArtistTrackingService + MusicBrainz
- **StatsHandler** - Aggregate statistics endpoints
- **WebSocketManager** - Real-time log streaming via PostgreSQL LISTEN/NOTIFY
- **SpotifyAuthHandler** - OAuth2 Spotify integration

### Key Patterns
- **Session validation**: Every handler performs auth check via cookie lookup against sessions table with expiration validation
- **HTMX detection**: Check `Htmx-Request` header to return HTML partials vs JSON vs redirect
- **GORM preloading**: Use `.Preload()` for related entities (Watchlist.QualityProfile, Schedule.Watchlist)
- **Ownership validation**: Non-admin users can only access their own resources; admin sees all
- **Service integration**: WatchlistHandler/ArtistsHandler delegate to domain services

### Structs
```go
type AuthHandler struct { db *gorm.DB }
type WatchlistHandler struct { db *gorm.DB; service *services.WatchlistService }
type WebSocketManager struct { clients map[string]map[*websocket.Conn]struct{}; mu sync.RWMutex }
```

## Flow

### Full Page Request Flow
1. Request arrives at Fiber router
2. Handler validates session cookie against sessions table
3. Query domain data (Jobs, Watchlists, Libraries, etc.)
4. Call `c.Render(templateName, fiber.Map{...})` - uses Pongo2 engine
5. Server returns complete HTML page

### HTMX Partial Request Flow
1. Request includes `Htmx-Request: true` header
2. Handler detects HTMX and returns `c.Render("partials/...")`
3. Pongo2 renders only the fragment (no layout)
4. HTMX swaps content into page

### JSON API Request Flow
1. Request to `/api/*` endpoints (no HTMX)
2. Handler validates auth, returns `c.JSON(data)`
3. Client receives JSON for programmatic access

### WebSocket Flow
1. Client connects to `/ws/console/:job_id` or `/ws/events`
2. WebSocketManager subscribes client to job-specific channel
3. PostgreSQL NOTIFY triggers on job_log inserts
4. Listener receives notification, broadcasts to WebSocket clients
5. Clients receive HTML-formatted log lines for direct insertion

## Integration

### Dependencies
- **gofiber/fiber/v2** - HTTP framework, routing, context
- **gorm.io/gorm** - Database access via injected *gorm.DB
- **services.WatchlistService** - Watchlist domain logic
- **services.ArtistTrackingService** - Artist monitoring logic
- **services.MusicBrainzService** - Artist lookup
- **github.com/gofiber/contrib/websocket** - WebSocket upgrade
- **github.com/lib/pq** - PostgreSQL LISTEN/NOTIFY for real-time

### Consumers
- **Browser** - HTMX client making requests to all endpoints
- **MCP Server** (backend/cmd/agent/) - JSON API calls for agentic access
- **WebSocket connections** - Live job log viewers
- **External scripts** - JSON API for CI/CD pipelines

### Database Models Accessed
- User, Session, Job, JobLog, Watchlist, Library, Track
- QualityProfile, Schedule, MonitoredArtist, SpotifyToken