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

## 2026-03-24 - Consolidated Dashboard Stats Queries
**Learning:** Dashboard endpoints `GetActivityStats` and `GetSummary` were performing multiple sequential `Count` queries (up to 6) to gather various metric totals. This created unnecessary database roundtrips and increased latency for the dashboard UI.
**Action:** Consolidated multiple `Count` operations into a single SQL statement using subqueries. This reduces the database roundtrips to 1 per request, significantly improving response times for metric-heavy endpoints. Verified correctness with new integration tests using a seeded in-memory SQLite database.

## 2026-03-24 - Targeted Column Selection in Watchlist Filtering
**Learning:** Functions like `GetNewTracks` and `FilterExistingTracks` perform bulk lookups against large tables (`acquisitions`, `tracks`, `jobitems`). By default, GORM selects all columns (`*`), which increases memory allocation and database I/O, especially when only identification fields (`artist`, `title`) are needed.
**Action:** Use `.Select("column1, column2")` before `.Find()` to fetch only necessary fields. This optimization reduced `GetNewTracks` latency by ~66% and `FilterExistingTracks` latency by ~51% in synthetic benchmarks.

## 2026-03-25 - Redundant Session Lookups
**Learning:** Found a recurring pattern where API handlers were manually performing database queries to look up user sessions from cookies, even when those handlers were already protected by an `AuthMiddleware` that populates `c.Locals("user")`. This resulted in one unnecessary database roundtrip per request.
**Action:** Replaced manual session lookups with `user, ok := c.Locals("user").(database.User)` in several handlers (`ArtistsHandler`, `StatsHandler`, `WatchlistPreviewHandler`). This optimization reduces the database load and latency for these endpoints by eliminating one query per request.

## 2026-04-01 - Redundant Initial Dashboard Load Queries
**Learning:** The dashboard's initial synchronous render was performing four separate database queries to fetch stats, recent jobs, watchlists, and quality profiles. However, these variables were never actually used in the `index.html` template, as the dashboard relies on HTMX to load this data asynchronously via partials.
**Action:** Removed the redundant database queries from `DashboardHandler.RenderIndex` and updated the fiber render map. This reduces the database load by 4 queries per dashboard visit and improves the initial page load time.
