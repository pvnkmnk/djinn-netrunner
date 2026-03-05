-- Migration: add cover art fields to acquisitions for downloaded artwork tracking

ALTER TABLE acquisitions
    ADD COLUMN IF NOT EXISTS cover_art_url TEXT,
    ADD COLUMN IF NOT EXISTS cover_art_path TEXT,
    ADD COLUMN IF NOT EXISTS cover_art_etag TEXT,
    ADD COLUMN IF NOT EXISTS image_hash TEXT,
    ADD COLUMN IF NOT EXISTS cover_last_checked TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_acq_cover_art_path ON acquisitions(cover_art_path);

COMMENT ON COLUMN acquisitions.cover_art_url IS 'Source URL of cover art (e.g., CAA)';
COMMENT ON COLUMN acquisitions.cover_art_path IS 'Local filesystem path to saved cover image';
COMMENT ON COLUMN acquisitions.cover_art_etag IS 'ETag or validator for conditional requests';
COMMENT ON COLUMN acquisitions.image_hash IS 'Hash of the image contents for dedupe';
COMMENT ON COLUMN acquisitions.cover_last_checked IS 'Timestamp when cover art was last fetched/verified';
