# backend/internal/config/

## Responsibility
Environment-based configuration loader. Reads from `.env` file and environment variables into a typed `Config` struct.

## Design
- Single `config.go` file with `Config` struct and `Load()` function
- Uses `github.com/joho/godotenv` for `.env` file loading
- Fields: Server (Port, Domain, Environment), Security (JWTSecret), Database (DatabaseURL), Spotify (ClientID, ClientSecret), MusicBrainz (UserAgent, APIKey), AcoustID (ApiKey), Slskd (URL, APIKey), Gonic (URL, User, Pass), Library (MusicLibraryPath), Templates (TemplatesPath), Proxy (SOCKS5/HTTP), DiskQuota
- `Load()` reads env vars with `os.Getenv()`, returns `*Config` or error

## Flow
1. `Load()` → `godotenv.Load()` (optional `.env`) → read each field from env → return `Config`

## Integration
- **Consumed by**: All `cmd/*` entry points, all `internal/services`
- **Depends on**: Environment variables, `.env` file
