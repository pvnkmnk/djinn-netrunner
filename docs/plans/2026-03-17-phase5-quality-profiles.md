# Phase 5 Design: Quality Profiles Enhancement

## Overview
Enhance the quality profile system to provide better management, validation, and operational capabilities for acquisition quality handling.

## Current State
- QualityProfile model exists with: Name, Description, PreferLossless, AllowedFormats, MinBitrate, PreferBitrate, PreferSceneReleases, PreferWebReleases, IsDefault
- Used by Watchlists and MonitoredArtists
- Listed in dashboard

## Goals
1. **Quality Profile CRUD API** - Full API for creating, reading, updating, deleting profiles
2. **Quality Profile CLI** - CLI commands to manage profiles
3. **Profile Validation** - Validate download candidates against profile rules
4. **Profile Presets** - Built-in default profiles

## Proposed Changes

### 1. QualityProfile CRUD API
- `GET /api/profiles` - List all profiles
- `GET /api/profiles/:id` - Get single profile
- `POST /api/profiles` - Create profile
- `PATCH /api/profiles/:id` - Update profile
- `DELETE /api/profiles/:id` - Delete profile

### 2. QualityProfile CLI
- `netrunner-cli profile list` - List profiles
- `netrunner-cli profile add [name]` - Add profile with defaults
- `netrunner-cli profile rm [id]` - Delete profile

### 3. Profile Validation Service
- Validate download candidates against profile rules
- Methods:
  - `ValidateFormat(profile, format) bool`
  - `ValidateBitrate(profile, bitrate) bool`
  - `CalculateScore(profile, result) float64`

### 4. Profile Presets
- Add default profiles on first run:
  - "High Quality" - FLAC, lossless preferred, 320kbps min
  - "Portable" - MP3 192kbps minimum
  - "Archival" - FLAC only, no compression

## Acceptance Criteria
1. Full CRUD API for quality profiles
2. CLI commands for profile management
3. Profile validation logic integrated into acquisition
4. Default profiles created on initialization
