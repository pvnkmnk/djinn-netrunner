-- Provenance for a monitored artist: the two MusicBrainz fields that
-- disambiguate same-named entities.
--
-- These were decoded by the search service and used in the picker, then
-- discarded by every writer -- so the disambiguation was available at the
-- moment of choosing and unavailable at the moment of checking. Existing rows
-- are left empty on purpose; the CLI backfill fills what still resolves, and a
-- row whose entity no longer exists stays blank rather than being invented.
--
-- Idempotent, matching every other file in this directory. The column is
-- artist_type rather than type because `type` is a loaded word in SQL dialects
-- and buys nothing here.

ALTER TABLE monitored_artists ADD COLUMN IF NOT EXISTS country TEXT;
ALTER TABLE monitored_artists ADD COLUMN IF NOT EXISTS artist_type TEXT;