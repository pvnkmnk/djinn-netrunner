# backend/internal/interfaces/

## Responsibility
Defines abstraction boundaries for external music services. Allows pluggable implementations for different music sources and authentication methods.

## Design

### SpotifyClientProvider (spotify.go)
- **Interface**: `GetClient(ctx context.Context, userID uint64) (*spotify.Client, error)`
- **Purpose**: Abstracts Spotify OAuth client retrieval for user-specific API access
- **Implementers**: Concrete implementation in services package provides authenticated client

### WatchlistProvider (watchlist.go)
- **Interface**: 
  - `FetchTracks(ctx context.Context, watchlist *database.Watchlist) ([]map[string]string, string, error)`
  - `ValidateConfig(config string) error`
- **Purpose**: Abstracts music source (Spotify playlist, liked songs, etc.)
- **Implementers**: Spotify provider fetches tracks; validates source URIs

## Flow
1. **Services** depend on interfaces, not concrete implementations
2. **WatchlistService** uses `WatchlistProvider` to fetch current track lists
3. **Spotify auth** uses `SpotifyClientProvider` to get user-specific clients
4. Providers are injected at runtime, enabling testing with mocks

## Integration
- **Dependencies**: github.com/zmb3/spotify/v2, internal/database
- **Consumers**: internal/services/watchlist_service.go, internal/services/spotify_service.go
