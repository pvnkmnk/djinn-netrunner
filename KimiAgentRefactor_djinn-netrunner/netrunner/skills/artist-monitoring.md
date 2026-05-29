***
skill: artist-monitoring
version: 1
repo: netrunner
language: go
tags: [domain, artist-tracking, musicbrainz, release-monitor]
***

# Skill: Artist Monitoring Workflow

## Purpose
Track artists, sync release metadata, and queue acquisition for new releases.

## Prerequisites
- MusicBrainz connectivity configured.
- Worker process running for periodic checks.

## Core Concepts
- `MonitoredArtist` stores tracking target and profile linkage.
- `TrackedRelease` stores release-group/release state.
- `ReleaseMonitorService` and artist sync routines generate jobs for new content.

## Step-by-Step Procedures
1. Add or verify monitored artist via API/UI.
2. Trigger/observe release monitor cycle from worker logs.
3. Confirm `tracked_releases` updates and queued acquisition jobs.
4. Review job outcomes via stats/log endpoints.

## Code Patterns
Artist sync service composition:
```go
mb := services.NewMusicBrainzService(cfg)
at := services.NewArtistTrackingService(db, mb)
rm := services.NewReleaseMonitorService(db, at)
```

## Validation
- Artist appears in monitored list.
- Discography sync updates release totals.
- New releases generate expected queued acquisition jobs.

## Edge Cases & Error Handling
- MusicBrainz rate limits can delay sync completion.
- Non-admin users must not access other users' monitored artists.
- Deduplicate releases by stable identifiers (release group IDs).

## References
- `backend/internal/services/artist_tracking_service.go`
- `backend/internal/services/release_monitor_service.go`
- `backend/internal/database/models.go`
- `backend/internal/api/artists.go`