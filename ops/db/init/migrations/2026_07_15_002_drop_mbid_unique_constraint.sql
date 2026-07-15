-- Drop the global unique constraint on musicbrainz_id
-- Replace with a unique constraint on (owner_user_id, musicbrainz_id) combo
-- This allows the same MBID to be tracked by different owners
-- Note: PostgreSQL treats NULL as distinct in unique indexes, so multiple
-- NULL-owner rows with the same MBID are allowed at DB level.
-- The service layer provides application-level enforcement for this case.

DROP INDEX IF EXISTS idx_monitored_artists_musicbrainz_id;

-- Create composite unique index: same MBID allowed for different owners
-- NULL owner_user_id rows are treated as distinct by PostgreSQL unique indexes,
-- allowing multiple global (NULL-owner) artists with the same MBID
CREATE UNIQUE INDEX IF NOT EXISTS idx_monitored_artists_owner_mbid
    ON monitored_artists(owner_user_id, musicbrainz_id);
