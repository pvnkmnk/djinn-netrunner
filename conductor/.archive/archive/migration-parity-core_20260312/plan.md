# Implementation Plan: Migration Parity Core

**Track ID:** migration-parity-core_20260312
**Spec:** [spec.md](./spec.md)
**Created:** 2026-03-12
**Status:** [x] Completed

## Overview
Unify core models, implement job lifecycle, and ensure system stability parity to allow full removal of legacy Python code.

## Phase 1: Database Unification
Migrate legacy paradigms to the new Go-native structure.

### Tasks
- [x] Task 1.1: **Migration Script**: Create a SQL/Go utility to migrate all `Source` records to `Watchlist`. (25d579d)
- [x] Task 1.2: **SyncHandler Refactor**: Remove legacy `Source` handling from `SyncHandler` and the database package. (25d579d)
- [x] Task 1.3: **UI Updates**: Update dashboard templates to use `Watchlist` exclusively. (25d579d)

### Verification
- [x] Verify all previous "Sources" appear as "Watchlists" in the UI. (25d579d)
- [x] Syncing a migrated watchlist works correctly. (25d579d)

## Phase 2: Job Lifecycle & Stability
Implement robust tracking and recovery.

### Tasks
- [x] Task 2.1: **Job Progress**: Implement logic in `AcquisitionHandler.Execute` to calculate and update job-level progress. (25d579d)
- [x] Task 2.2: **Zombie Cleanup**: Create a background task in `WorkerOrchestrator` that resets "In Progress" jobs with expired heartbeats. (25d579d)
- [x] Task 2.3: **Gonic Sync Hook**: Ensure `GonicClient` triggers a library scan after successful imports. (25d579d)

### Verification
- [x] Simulate a worker crash and verify the job is reset to `queued`. (25d579d)
- [x] UI shows percentage progress for active acquisition jobs. (25d579d)

## Phase 3: CLI & Final Cleanup
Remove the "Python dependency" from the environment.

### Tasks
- [x] Task 3.1: **CLI Commands**: Implement `netrunner watchlist add` and `netrunner sync` in `backend/cmd/cli`. (25d579d)
- [x] Task 3.2: **Legacy Deletion**: Delete `ops/worker`, `ops/web`, and `scripts/add_source.py`. (25d579d)
- [x] Task 3.3: **README Update**: Remove Python setup instructions and update CLI usage. (25d579d)

### Verification
- [x] CLI commands work as expected for manual management. (25d579d)
- [x] `docker-compose up` works perfectly without Python images. (25d579d)

## Final Verification
- [x] Full migration parity achieved. (25d579d)
- [x] All Go tests passing. (25d579d)
- [x] No Python dependencies remaining. (25d579d)
