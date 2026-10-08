# ops/db/init/migrations/

## Responsibility
Incremental SQL migrations for evolving schema on existing databases.

## Design
| File | Purpose |
|------|----------|
| `2025_12_17_001_add_spotify_columns.sql` | Adds provider, external_id, snapshot_id to sources |
| `2025_12_17_002_add_musicbrainz_fields.sql` | MusicBrainz ID fields |
| `2025_12_17_003_add_cover_art_fields.sql` | Cover art metadata |
| `2025_12_17_004_add_schedules.sql` | Schedule table support |
| `2026_03_04_006_artist_tracking.sql` | Artist tracking fields |
| `2026_03_04_007_library_and_track.sql` | Library and track tables |
| `2026_03_22_001_add_phase8_fields.sql` | Phase 8 feature fields |
| `2026_03_22_002_convert_enums_to_text.sql` | Enum to text conversion |
| `2026_07_15_002_drop_mbid_unique_constraint.sql` | Replaces the global unique index on music_brainz_id with a composite owner+mbid index |
| `2026_10_02_001_acoustid_score_is_nullable.sql` | Makes acoustid_score nullable so "never scored" and "scored zero" stop being the same fact |
| `2026_10_07_001_artist_provenance.sql` | Adds country and artist_type to monitored_artists (DJI-589) |

## Flow
- Applied manually or via docker-entrypoint-initdb.d to existing databases
- All use `ADD COLUMN IF NOT EXISTS` pattern for idempotency

## Integration
- **Backend**: AutoMigrate picks up same changes; migrations ensure DB matches
- **Strategy**: See `MIGRATION_POLICY.md` — AutoMigrate is primary, these are bootstrap