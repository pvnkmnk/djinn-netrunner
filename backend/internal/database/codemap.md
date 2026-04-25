# backend/internal/database/

## Responsibility
Provides the data persistence layer for NETRUNNER using GORM with CGO-free SQLite (modernc.org/sqlite) in WAL mode, supporting PostgreSQL as an alternative. Handles all database operations including models, migrations, connection management, and advisory locking for job scope protection.

## Design

### Models (models.go)
- **User/Session**: Authentication and session management
- **QualityProfile**: Download preferences (lossless/bitrate, formats, scene vs web releases)
- **MonitoredArtist**: Artist tracking with MusicBrainz integration and per-artist quality settings
- **TrackedRelease**: Release monitoring with acquisition status
- **Library/Track**: Local music library catalog with file metadata
- **Watchlist**: Automated source monitoring (Spotify playlists, liked songs)
- **SpotifyToken**: OAuth token storage
- **Schedule**: Cron-based sync schedules for watchlists
- **Job/JobItem**: Background job queue with retry logic
- **Acquisition**: Imported track history with MusicBrainz/AcoustID metadata
- **MetadataCache**: Cached API responses
- **Lock**: Distributed lock table (for SQLite advisory locks)
- **Setting**: Key-value configuration store

### Connection Strategy (connection.go)
- Dual-backend support: SQLite (default) or PostgreSQL
- SQLite: WAL mode, synchronous=NORMAL, foreign_keys=ON, busy_timeout=5000ms
- Connection pool: 10 idle, 100 max, 1hr lifetime

### Locking Model (locks.go)
- **LockManager interface**: Abstracts lock acquisition
- **PostgresLockManager**: Uses `pg_try_advisory_lock`/`pg_advisory_unlock` for session-level advisory locks
- **TableLockManager**: Uses `locks` table with expiry-based locking for SQLite (15-minute default TTL)
- Scope locks: `1001 * 1000000000 + hash(scopeType:scopeID)` - protects per-scope job execution

### Migration (migrate.go)
- PostgreSQL: Converts ENUM columns to text for GORM compatibility
- AutoMigrate: Handles schema evolution for all models

### LiteFS Support (litefs.go)
- **LiteFSGuard**: Detects primary node via `.primary` file in mount directory
- Enables single-primary database writes in distributed deployments

### Helpers (models_helper.go)
- **CalculateBackoff**: Exponential backoff (1m, 5m, 1h, 24h)
- **AppendJobLog**: Job progress logging
- **QualityProfile.IsMatch**: Format/bitrate matching logic
- **QualityProfile.GetSearchSuffix**: Search query suffix generation

## Flow
1. **Startup**: `Connect()` establishes DB connection, applies WAL/pragmas, tests ping
2. **Migration**: `Migrate()` runs schema migrations (ENUM conversion, AutoMigrate)
3. **Locking**: `NewLockManager()` selects Postgres or Table-based implementation
4. **Operations**: Services use GORM for CRUD; job workers acquire scope locks before execution
5. **LiteFS**: `IsPrimary()` checked before write operations in distributed setups

## Integration
- **Dependencies**: GORM, modernc.org/sqlite, github.com/google/uuid
- **Consumers**: All services in `internal/services/` for data access; `internal/agent/handlers.go` for CLI operations
- **Configuration**: Database URL passed via `config.Config.DatabaseURL`
