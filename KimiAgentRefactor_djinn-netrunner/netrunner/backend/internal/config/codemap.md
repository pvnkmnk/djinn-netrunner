# backend/internal/config/

## Responsibility
Manages application configuration through environment variables with sensible defaults. Provides a centralized `Config` struct that all components use to access settings.

## Design

### Config Struct (config.go)
- **Server**: Environment, Port, Domain
- **Security**: JWT secret
- **Database**: DatabaseURL (required)
- **Spotify**: Client ID/Secret for OAuth
- **MusicBrainz**: User agent, API key
- **AcoustID**: API key for fingerprint lookup
- **Slskd**: URL and API key for Soulseek client
- **Gonic**: URL and credentials for music server
- **Library**: Music library path
- **Templates**: Paths for HTMX templates and static assets
- **Integrations**: Last.fm, ListenBrainz, Discogs tokens
- **Proxy**: SOCKS5/HTTP proxy URL for all external traffic
- **Notifications**: Webhook URL and enabled flag

### Loading Strategy
- Loads `.env` files via `joho/godotenv` (default: `../../.env`)
- Uses `getEnv()` for string values with defaults
- Uses `getEnvBool()` for boolean flags
- Validates required fields (DatabaseURL)

## Flow
1. **Load()** is called at startup with optional `.env` filenames
2. Returns `*Config` with all fields populated from environment or defaults
3. Validates required fields; returns error if DATABASE_URL missing
4. Config is passed to all components needing settings

## Integration
- **Dependencies**: github.com/joho/godotenv
- **Consumers**: All packages needing configuration (database, services, handlers)
