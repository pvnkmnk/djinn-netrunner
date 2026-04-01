# backend/internal/services/

## Responsibility

The services package is the **core business logic layer** of NETRUNNER, implementing the music acquisition pipeline. It handles:
- **Artist tracking** and release monitoring via MusicBrainz
- **Watchlist management** across multiple music services (Spotify, Last.fm, ListenBrainz, Discogs, RSS, local files)
- **Job orchestration** for sync and acquisition workflows
- **P2P acquisition** via Soulseek (slskd)
- **Metadata enrichment** from multiple sources (MusicBrainz, Discogs, AcoustID)
- **Library management** (scanning, quotas, notifications)
- **Quality profile** enforcement for downloads

## Design

### Architecture Overview

The services are organized into distinct functional domains, each with a dedicated service file:

```
services/
├── artist_tracking_service.go    # Artist monitoring + MusicBrainz discography sync
├── watchlist_service.go          # Watchlist CRUD + provider orchestration
├── job_handlers.go               # SyncHandler + AcquisitionHandler (job execution)
├── slskd_service.go              # Soulseek P2P search & download
├── gonic_client.go               # Subsonic API client (library indexing)
├── musicbrainz_service.go        # MusicBrainz API (artist/recording lookups)
├── spotify_service.go            # Spotify client credentials auth
├── spotify_provider.go           # Spotify watchlist provider (liked songs, playlists)
├── discogs_service.go            # Discogs API (metadata enrichment)
├── discogs_provider.go           # Discogs wantlist provider
├── lastfm_provider.go            # Last.fm loved tracks / top tracks provider
├── listenbrainz_provider.go      # ListenBrainz recent listens provider
├── rss_provider.go               # RSS/Atom feed provider
├── file_watchlist_provider.go   # Local CSV/M3U/TXT file provider
├── acoustid_service.go           # AcoustID fingerprint lookup
├── scanner_service.go             # Library file scanning + indexing
├── cache_service.go               # Database-backed metadata cache
├── notification_service.go       # Webhook notifications (job completion, quota alerts)
├── disk_quota_service.go         # Disk usage calculation + quota enforcement
├── metadata_extractor.go         # Audio tag reading + cover art embedding
├── profile_service.go            # Quality profile management
└── release_monitor_service.go    # Background release checking
```

### Key Patterns

#### Provider Pattern (Watchlist)
The `WatchlistService` uses a pluggable provider architecture. Each provider implements `interfaces.WatchlistProvider`:
- `FetchTracks(ctx, watchlist)` - retrieves tracks from source
- `ValidateConfig(config)` - validates source configuration

Providers are registered at service initialization:
```go
s.RegisterProvider("spotify_liked", NewSpotifyProvider(spotifyAuth))
s.RegisterProvider("lastfm_loved", NewLastFMProvider(cfg.LastFMApiKey))
s.RegisterProvider("rss_feed", NewRSSProvider())
// ... etc
```

#### Job Handler Pattern
Job execution is abstracted via the `JobHandler` interface:
```go
type JobHandler interface {
    Execute(ctx context.Context, jobID uint64, data database.Job) error
}
```
Two primary handlers:
- **SyncHandler** - discovers new tracks from watchlists
- **AcquisitionHandler** - executes the full acquisition pipeline per item

#### Rate Limiting
External APIs use ticker-based rate limiters:
- MusicBrainz: 1 req/sec
- Discogs: 60 req/min (authenticated)

#### Caching
The `CacheService` provides database-backed caching with TTL support. Used by:
- MusicBrainz lookups
- Spotify playlist data
- AcoustID results
- Cover art

### Quality Profiles
`ProfileService` manages quality profiles that control acquisition behavior:
- Allowed formats (FLAC, ALAC, WAV, etc.)
- Minimum bitrate
- Lossless preference
- Cover art source priority

The `QualityProfile.IsMatch()` method scores search results in `SlskdService`.

## Flow

### Watchlist Sync Flow
```
1. SyncHandler.Execute()
   │
   ├─► WatchlistService.FetchWatchlistTracks()
   │   └─► Provider.FetchTracks()
   │       (Spotify/Last.fm/Discogs/RSS/File)
   │
   ├─► WatchlistService.GetNewTracks()
   │   └─► Compare against acquisition history
   │
   ├─► WatchlistService.FilterExistingTracks()
   │   └─► Check library + active job queue
   │
   └─► Create acquisition job + job items
```

### Acquisition Flow (per item)
```
1. AcquisitionHandler.ExecuteItem()
   │
   ├─► GonicClient.Search3()  [pre-flight: already indexed?]
   │
   ├─► SlskdService.Search()  [profile-aware scoring]
   │   └─► QualityProfile.CalculateScore()
   │
   ├─► SlskdService.EnqueueDownload()
   │
   ├─► SlskdService.WaitForDownload()
   │
   ├─► MetadataExtractor.Extract()  [read tags]
   │   ├─► HashFile()  [deduplication]
   │   └─► Fingerprint()  [AcoustID]
   │
   ├─► AcoustIDService.Lookup()  [fingerprint → MBID]
   │
   ├─► MusicBrainzService.SearchRecording()  [enrichment]
   │
   ├─► Move file to library path
   │
   ├─► getCoverArtWithFallback()
   │   ├─► Source URL
   │   ├─► MusicBrainzService.GetCoverArt()
   │   └─► DiscogsService.GetCoverArt()
   │
   └─► MetadataExtractor.EmbedCoverArt()
       └─► Create Acquisition record
```

### Artist Tracking Flow
```
1. ArtistTrackingService.AddMonitoredArtist()
   └─► Create MonitoredArtist record

2. ReleaseMonitorService.CheckAllArtists() (hourly)
   └─► ArtistTrackingService.SyncDiscography()
       │
       ├─► MusicBrainzService.GetArtistDiscography()
       │   └─► Parse release-groups
       │
       ├─► Upsert TrackedRelease records
       │
       └─► Create acquisition job for new releases
```

## Integration

### Consumers of Services

| Service | Consumers |
|---------|-----------|
| WatchlistService | API handlers, SyncHandler |
| JobHandlers | Worker goroutines (job dispatcher) |
| SlskdService | AcquisitionHandler |
| GonicClient | AcquisitionHandler (scan trigger) |
| MusicBrainzService | ArtistTrackingService, AcquisitionHandler |
| DiscogsService | AcquisitionHandler (cover art fallback) |
| ScannerService | API handlers (manual scan) |
| DiskQuotaService | Background workers, API handlers |
| NotificationService | JobHandlers, DiskQuotaService |

### External Dependencies

| Service | External API/Dependency |
|---------|-------------------------|
| SlskdService | slskd (Soulseek daemon) - REST API |
| GonicClient | Gonic music server - Subsonic REST API |
| MusicBrainzService | musicbrainz.org - REST API |
| DiscogsService | api.discogs.com - REST API |
| SpotifyProvider | Spotify Web API - OAuth |
| LastFMProvider | ws.audioscrobbler.com - Last.fm API |
| ListenBrainzProvider | api.listenbrainz.org - ListenBrainz API |
| RSSProvider | Any RSS/Atom feed |
| AcoustIDService | api.acoustid.org - AcoustID API |
| MetadataExtractor | fpcalc (Chromaprint CLI), tag libraries |

### Database Models Used
- `database.Job` / `database.JobItem` - job queue
- `database.Watchlist` - watchlist configuration
- `database.MonitoredArtist` / `database.TrackedRelease` - artist tracking
- `database.QualityProfile` - acquisition profiles
- `database.Acquisition` - acquisition history
- `database.Track` - library tracks
- `database.Library` - music libraries
- `database.MetadataCache` - cache storage

### Configuration
Services receive configuration via `config.Config` struct:
- API keys (Spotify, Discogs, Last.fm, AcoustID)
- Service URLs (slskd, Gonic)
- Proxy settings for P2P traffic
- Music library path
