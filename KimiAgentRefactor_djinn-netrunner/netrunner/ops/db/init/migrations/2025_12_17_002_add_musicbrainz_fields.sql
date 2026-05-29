-- Migration: add MusicBrainz enrichment fields to acquisitions

ALTER TABLE acquisitions
    ADD COLUMN IF NOT EXISTS mb_recording_id TEXT,
    ADD COLUMN IF NOT EXISTS mb_release_id TEXT,
    ADD COLUMN IF NOT EXISTS mb_artist_id TEXT,
    ADD COLUMN IF NOT EXISTS enrichment_confidence NUMERIC(3,2),
    ADD COLUMN IF NOT EXISTS enriched_year INT,
    ADD COLUMN IF NOT EXISTS enriched_genre TEXT;

CREATE INDEX IF NOT EXISTS idx_acq_mb_recording ON acquisitions(mb_recording_id);
CREATE INDEX IF NOT EXISTS idx_acq_mb_release ON acquisitions(mb_release_id);
CREATE INDEX IF NOT EXISTS idx_acq_mb_artist ON acquisitions(mb_artist_id);

COMMENT ON COLUMN acquisitions.mb_recording_id IS 'MusicBrainz recording MBID';
COMMENT ON COLUMN acquisitions.mb_release_id IS 'MusicBrainz release MBID';
COMMENT ON COLUMN acquisitions.mb_artist_id IS 'MusicBrainz artist MBID';
COMMENT ON COLUMN acquisitions.enrichment_confidence IS 'Confidence score (0.00-1.00) for applied enrichment';
COMMENT ON COLUMN acquisitions.enriched_year IS 'Year from enrichment if available';
COMMENT ON COLUMN acquisitions.enriched_genre IS 'Genre from enrichment if available';
