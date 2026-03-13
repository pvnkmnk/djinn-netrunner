# Track Specification: Migration Parity Core

**Track ID:** migration-parity-core_20260312
**Title:** Migration Parity Core & System Stability
**Type:** Refactor/Stability
**Status:** [ ] Not Started

## Overview
NetRunner has a functioning Go backend, but it currently maintains a "dual-personality" where legacy Python models (Source) and new Go models (Watchlist) coexist. This track will unify the data models, implement job lifecycle management, and ensure the system is stable enough to delete the legacy Python codebase.

## Goals
1.  **Unified Data Model**: Eliminate the `Source` model in favor of the `Watchlist` model.
2.  **Job Lifecycle Integrity**: Implement full job tracking and progress reporting.
3.  **System Resilience**: Add heartbeat-based "zombie job" recovery.
4.  **CLI Parity**: Replace `add_source.py` with native Go CLI commands.
5.  **Legacy Disposal**: Safely remove `ops/worker` and `ops/web` (Python).

## User Stories
- As a user, I want to manage all my music sources through a single "Watchlist" interface.
- As a user, I want to see the real-time progress of my acquisition jobs.
- As an administrator, I want the system to automatically recover from worker crashes without leaving jobs in "In Progress" states.
- As a developer, I want a clean Go codebase without vestigial Python directories.

## Technical Invariants
- No data loss during `Source` -> `Watchlist` migration.
- SQLite WAL mode must be respected for concurrent writes.
- UI (HTMX) must reflect the unified model correctly.

## Acceptance Criteria
- [ ] `SyncHandler` no longer contains logic for the `Source` model.
- [ ] `AcquisitionHandler.Execute` correctly tracks job progress.
- [ ] Zombie jobs are automatically reset to `queued` after a timeout.
- [ ] All Python files in `ops/` and `scripts/` are deleted.
- [ ] Go CLI can add and sync watchlists without Python scripts.
