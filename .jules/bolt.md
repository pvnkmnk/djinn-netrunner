## 2025-05-15 - N+1 query pattern in Watchlist filtering
**Learning:** Found a classic N+1 query bottleneck in the `WatchlistService.FilterExistingTracks` function. The original implementation was performing two separate database `Count` queries for every track in the input list. For a 100-track watchlist, this resulted in 200 queries, causing significant latency during sync operations.
**Action:** Replaced the loop-based queries with bulk fetches (`LOWER(artist) IN ?`) and in-memory hash map lookups. This reduced the database roundtrips to 2, regardless of the number of tracks. Also optimized `GetNewTracks` to use a targeted query instead of loading the entire user's acquisition history.

## 2026-03-18 - N+1 query pattern in SyncDiscography
**Learning:** Found a classic N+1 query bottleneck in `ArtistTrackingService.SyncDiscography`. The original code queried and inserted/updated records inside a loop for every release in an artist's discography. For an artist with 50+ releases, this caused dozens of database roundtrips. When implementing `CreateInBatches` for related records (e.g., `TrackedRelease` and `JobItem`), manual UUID generation is required to maintain ID consistency between memory slices before the DB generates them.
**Action:** Replaced the loop-based queries with a bulk fetch of existing releases and used GORM's `CreateInBatches` for bulk inserts of releases and job items. Manually generated UUIDs to ensure job items correctly reference newly created releases.

## 2026-03-20 - Multi-layered optimizations in ScannerService
**Learning:** Found several performance bottlenecks in `ScannerService` that significantly impacted large libraries: 1) `filepath.Walk` performs redundant `Lstat` calls, 2) individual database updates for file hashes after track upserts, and 3) N+1 database deletions in the pruning logic.
**Action:** Replaced `filepath.Walk` with `filepath.WalkDir` for faster traversal, consolidated track and hash updates into a single `FirstOrCreate` call with `Assign`, and refactored pruning to use targeted field selection (`id`, `path`) and a single batch `DELETE`. These changes collectively reduce syscalls and database roundtrips by orders of magnitude for large music collections.

## 2026-03-22 - Batching and Consolidation in Job Handlers
**Learning:** Identified classic O(N) database operations in `SyncHandler` and `AcquisitionHandler`. Specifically, `JobItem` creation for large watchlists was performing individual inserts in a loop, and job progress polling was using three separate `Count` queries every 5 seconds.
**Action:** Implemented GORM's `CreateInBatches` for `JobItem` creation and consolidated progress counts into a single query using `COUNT(*) FILTER`. These optimizations significantly reduce database roundtrips during sync and monitoring, especially under load. Verified that `FILTER` clause is supported by the CGO-free SQLite driver.

## 2026-03-24 - Consolidated Activity Stats Queries
**Learning:** Identified a performance bottleneck in `StatsHandler` where `GetActivityStats` and `GetSummary` were performing 5-6 separate `Count` queries across different tables (monitored_artists, watchlists, quality_profiles, etc.) to build a single response. This resulted in unnecessary database roundtrips and increased latency for dashboard-related endpoints.
**Action:** Consolidated these independent count queries into a single SQL query using subqueries in the `SELECT` clause (e.g., `SELECT (SELECT COUNT(*) FROM table1) as count1, ...`). This reduces database roundtrips from O(N) to O(1) for activity metrics, providing a measurable boost to dashboard load times.
