# Phase 4 Design: Metadata Enrichment

## Overview
Enhance track metadata by extending the data model and adding enrichment from external sources (MusicBrainz, Discogs).

## Goals
1. **Extended Track Model** - Add genre, year, composer, genre fields
2. **Cover Art Enrichment** - Download and embed cover art from external sources
3. **Batch Re-enrichment** - API to re-scan existing tracks
4. **Discogs Integration** - Use Discogs API for release metadata
5. **Genre Support** - Store and retrieve genre information

## Current State
- Track model has: ID, LibraryID, Title, Artist, Album, Path, TrackNum, DiscNum, Format, FileSize, FileHash
- MetadataExtractor extracts basic info from files
- MusicBrainzService for artist lookups
- DiscogsProvider for watchlists
- Cover art embedding exists in MetadataExtractor

## Proposed Changes

### 1. Extended Track Model
Add fields to Track:
```go
type Track struct {
    // ... existing fields
    Year     *int    // Release year
    Genre    string  // Genre
    Composer string  // Composer
    CoverURL string   // URL to cover art
}
```

### 2. Cover Art Enrichment
- Add method to MusicBrainzService to get release cover art
- Add method to DiscogsProvider for cover art
- Add CLI/API to trigger cover art download for library

### 3. Batch Re-enrichment API
- `POST /api/libraries/:id/enrich` - Trigger metadata enrichment job
- Creates "enrich" job type in worker
- Updates existing tracks with fresh metadata

### 4. Discogs Integration for Tracks
- Add DiscogsService for track-level lookups
- Search by artist/title to get release info
- Extract: year, genre, track listing

### 5. CLI Commands
- `netrunner-cli library enrich [id]` - Trigger enrichment
- `netrunner-cli library coverart [id]` - Download cover art

## Implementation Tasks

### Task 1: Extend Track Model
- Add Year, Genre, Composer, CoverURL to Track model
- Add database migration

### Task 2: Add DiscogsService
- Create new service for Discogs API (separate from provider)
- Methods: SearchRelease, GetReleaseDetails

### Task 3: Enhance MusicBrainzService
- Add GetReleaseCoverArt method

### Task 4: Add Enrichment Job Type
- Add "enrich" job type to worker
- Logic to lookup metadata and update tracks

### Task 5: API Endpoints
- POST /api/libraries/:id/enrich

### Task 6: CLI Commands
- library enrich, library coverart

## Acceptance Criteria
1. Track model includes genre, year, composer fields
2. Can trigger metadata enrichment for library
3. Discogs integration for release lookups
4. Cover art can be downloaded and stored
5. CLI commands work
