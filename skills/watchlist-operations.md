***
skill: watchlist-operations
version: 1
repo: netrunner
language: go
tags: [domain, watchlists, sync, ingestion]
***

# Skill: Watchlist Operations Workflow

## Purpose
Create, validate, and sync watchlists to feed acquisition jobs in NetRunner.

## Prerequisites
- Server and worker running.
- Authenticated user session (or CLI access to DB-backed environment).
- At least one quality profile exists.

## Core Concepts
- Watchlists are source descriptors (`source_type`, `source_uri`) bound to a quality profile.
- Sync flow: watchlist provider fetch -> delta detection -> job + jobitems creation.
- Providers are pluggable through `WatchlistProvider` interface.

## Step-by-Step Procedures
1. List existing watchlists.
```bash
cd backend
go run ./cmd/cli watchlist list
```
2. Add a watchlist via CLI.
```bash
go run ./cmd/cli watchlist add "My Playlist" "spotify_playlist" "spotify:playlist:<id>"
```
3. Trigger sync job.
```bash
go run ./cmd/cli watchlist sync <watchlist-uuid>
```
4. Inspect queued/running jobs.
```bash
go run ./cmd/cli stats jobs
```
5. Confirm resulting acquisitions/tracks in DB or API.

## Code Patterns
Provider registration:
```go
service.RegisterProvider("rss_feed", NewRSSProvider())
service.RegisterProvider("spotify_liked", NewSpotifyProvider(spotifyAuth))
```

## Validation
- Watchlist record exists and is enabled.
- Sync job is created with expected scope.
- New job items reflect discovered tracks and profile binding.

## Edge Cases & Error Handling
- Invalid `source_uri` should fail provider validation.
- Spotify sources require OAuth token for user.
- Duplicate sources are rejected by unique constraints.

## References
- `backend/internal/services/watchlist_service.go`
- `backend/internal/interfaces/watchlist.go`
- `backend/internal/api/watchlists.go`
- `backend/cmd/cli/main.go`