## 2025-05-15 - [Batching and Pre-fetching in Artist Sync]
**Learning:** The `ArtistTrackingService.SyncDiscography` method exhibited an N+1 query problem by checking for existing releases in a loop. Pre-fetching existing records into a map for $O(1)$ lookups and using GORM's `CreateInBatches` significantly improves performance for artists with large discographies.
**Action:** Always check for N+1 query patterns in synchronization services and use hash maps for existence checks and batching for persistence.
