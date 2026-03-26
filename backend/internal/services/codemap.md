# backend/internal/services/

## Responsibility
Core business logic layer. Implements the music acquisition pipeline: watchlist management, job orchestration, P2P acquisition, metadata enrichment, and artist tracking.

## Design
- **20+ service files** organized by functional domain
- **Provider pattern** for watchlist sources: `SpotifyProvider`, `LastFMProvider`, `ListenBrainzProvider`, `DiscogsProvider`, `RSSProvider`, `FileWatchlistProvider` — all implement `interfaces.WatchlistProvider`
- **Job handler pattern**: `SyncHandler` (watchlist sync → diff → create job items), `AcquisitionHandler` (search → download → fingerprint → enrich → move)
- **Rate limiting**: MusicBrainz 1 req/sec, Discogs 60 req/min
- **Caching**: `CacheService` — database-backed with TTL
- **Key services**: `WatchlistService`, `ArtistTrackingService`, `ScannerService`, `SlskdService`, `GonicClient`, `MusicBrainzService`, `DiscogsService`, `SpotifyService`, `MetadataExtractor`, `AcoustIDService`, `NotificationService`, `DiskQuotaService`, `ProfileService`, `ReleaseMonitorService`

## Flow
- **Watchlist sync**: WatchlistService.Sync() → provider.FetchTracks() → diff with existing → create JobItems → SyncHandler executes
- **Acquisition**: AcquisitionHandler → SlskdService.Search() → download → MetadataExtractor → AcoustIDService.Fingerprint() → MusicBrainzService.Enrich() → embed cover art → ScannerService.MoveToLibrary()
- **Artist tracking**: ArtistTrackingService → monitor artists → periodic discography sync → create jobs for new releases

## Integration
- **Consumed by**: `cmd/worker` (job handlers), `cmd/server` (API handlers), `cmd/agent`/`cmd/cli` (agent functions)
- **External**: slskd (P2P), Gonic (Subsonic API), MusicBrainz, Discogs, Spotify, Last.fm, ListenBrainz, AcoustID, RSS feeds
- **Database**: All GORM models from `internal/database`
