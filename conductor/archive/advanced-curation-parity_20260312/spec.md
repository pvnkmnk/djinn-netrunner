# Track Specification: Advanced Curation Parity

**Track ID:** advanced-curation-parity_20260312
**Title:** Advanced Curation & Metadata Parity
**Type:** Feature/Parity
**Status:** [ ] Not Started

## Overview
NetRunner v2.1 added basic metadata extraction and hashing, but it still lacks the "intelligence" of the legacy Python pipeline. This track focuses on porting the search ranking algorithms, implementing library maintenance (pruning), and adding AcoustID fingerprinting to ensure NetRunner is functionally superior to the original implementation and competitors like SoulSync.

## Goals
1.  **Smart Result Scoring**: Port the Python Soulseek result ranking logic (bitrate, speed, file size, user reputation).
2.  **Library Integrity**: Implement a "Prune" feature to remove stale records from the database when files are deleted from disk.
3.  **AcoustID Verification**: Implement audio fingerprinting to verify download accuracy.

## User Stories
- As a user, I want the system to automatically pick the highest quality, most reliable Soulseek result.
- As a user, I want my database to be in sync with my actual files on disk.
- As a collector, I want to verify that my downloaded tracks are exactly what they claim to be using fingerprinting.

## Technical Invariants
- Search scoring must prioritize FLAC and high-bitrate MP3 based on quality profiles.
- Pruning must be non-destructive (only remove DB records, not files).
- AcoustID lookup must be rate-limited according to API policies.

## Acceptance Criteria
- [ ] `SlskdService` uses a scoring weight matrix for search results.
- [ ] `ScannerService` has a `Prune` method that cleans up the `tracks` table.
- [ ] `MetadataExtractor` supports AcoustID fingerprint generation and lookup.
