# backend/internal/interfaces/

## Responsibility
Interface definitions establishing abstraction boundaries between packages. Defines contracts for external service providers.

## Design
- **spotify.go**: `SpotifyClientProvider` interface — methods for OAuth2 token management, playlist fetching, user profile
- **watchlist.go**: `WatchlistProvider` interface — methods for pluggable music source providers (fetch tracks, validate config)
- Interfaces enable dependency inversion: services depend on interfaces, not concrete implementations
- Concrete implementations in `internal/services/` (e.g., `SpotifyService` implements `SpotifyClientProvider`)

## Flow
1. Consumer (e.g., `WatchlistService`) depends on `WatchlistProvider` interface
2. Concrete provider (e.g., `SpotifyProvider`) injected at initialization
3. Consumer calls interface methods without knowing implementation details

## Integration
- **Consumed by**: `internal/services` (WatchlistService, SpotifyService)
- **Implemented by**: `internal/services` (SpotifyProvider, LastFMProvider, etc.)
- **Pattern**: Dependency Inversion / Strategy pattern
