# Phase 4 Design: Library Scanner

## Overview
Add full library scanning capability to NetRunner - scanning local directories for audio files, extracting metadata, and indexing them in the database.

## Goals
- Scan local music library directories
- Extract metadata from audio files (title, artist, album, format)
- Compute file hashes for deduplication
- Support prune mode to remove DB entries for deleted files
- Full integration: job type + CLI + API

## Current State
- `ScannerService` exists with `ScanLibrary()` and `PruneTracks()` methods
- FileWatchlistProvider exists for parsing playlist files
- Job types: `sync`, `acquisition`, `artist_scan`, `release_monitor`, `index_refresh`

## Architecture

### Job Type: `scan`
```
Job {
  Type: "scan"
  ScopeType: "library"
  ScopeID: <library-uuid>
}
```

### Components to Add
1. **Scan Job Handler** - Wire ScannerService into worker
2. **CLI Command** - `netrunner-cli scan <path> [--prune]`
3. **API Endpoint** - `POST /api/scan` 

## Data Flow
1. User triggers scan via CLI or API
2. Job created with type="scan", scope_type="library"
3. Worker claims job, acquires lock
4. ScannerService.ScanLibrary() processes files
5. Logs streamed to console via job_logs
6. Job marked complete

## CLI Commands
```bash
# Scan a library
netrunner-cli scan /music/library

# Scan and prune missing files
netrunner-cli scan /music/library --prune

# List libraries
netrunner-cli library list
```

## API Endpoints
- `POST /api/scan` - Trigger library scan
- `GET /api/libraries` - List configured libraries
- `POST /api/libraries` - Add library config

## Database Tables
- `libraries` - Library configurations (path, name, enabled)
- `tracks` - Already exists, stores scanned tracks

## Acceptance Criteria
1. Can scan a directory via CLI and see progress in console
2. Can scan via API and track job status
3. Metadata extracted and stored in DB
4. Prune mode removes stale entries
5. Deduplication works (via file_hash)
