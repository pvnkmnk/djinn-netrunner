# Implementation Plan: Advanced Curation Parity

**Track ID:** advanced-curation-parity_20260312
**Spec:** [spec.md](./spec.md)
**Created:** 2026-03-12
**Status:** [x] Completed

## Overview
Port advanced curation intelligence and library maintenance features from the legacy Python codebase to Go.

## Phase 1: Intelligent Search Scoring
Bring back the "Soul" of the search engine.

### Tasks
- [x] Task 1.1: **Scoring Logic**: Port the Python result ranking algorithm to `backend/internal/services/slskd_service.go`. (25d579d)
- [x] Task 1.2: **Profile Weighting**: Update `SlskdService.Search` to apply quality profile weights to the scores. (25d579d)

### Verification
- [x] Verify that a search for a popular track picks a high-bitrate, high-speed result over a low-quality one. (25d579d)

## Phase 2: Library Maintenance & Pruning
Ensure the database stays in sync with the filesystem.

### Tasks
- [x] Task 2.1: **Prune Logic**: Implement a `PruneTracks` method in `ScannerService` that checks for file existence. (25d579d)
- [x] Task 2.2: **Sync Cleanup**: Integrate the prune task into the daily `ReleaseMonitorService` or a dedicated system job. (25d579d)

### Verification
- [x] Manually delete a file and verify its DB record is removed after a prune scan. (25d579d)

## Phase 3: Fingerprinting (AcoustID)
High-fidelity verification.

### Tasks
- [x] Task 3.1: **Chromaprint Wrapper**: Integrate a Go wrapper for `fpcalc` or a native implementation. (25d579d)
- [x] Task 3.2: **AcoustID Service**: Implement `AcoustIDService` to fetch metadata based on fingerprints. (25d579d)
- [x] Task 3.3: **Verification Step**: Add a fingerprint check to the `AcquisitionHandler` import flow. (25d579d)

### Verification
- [x] Successfully fetch MusicBrainz IDs for a track using its audio fingerprint. (25d579d)

## Final Verification
- [x] Functional parity with Python `MetadataEnricher` achieved. (25d579d)
- [x] Search ranking verified with real-world queries. (25d579d)
- [x] Library pruning confirmed operational. (25d579d)
