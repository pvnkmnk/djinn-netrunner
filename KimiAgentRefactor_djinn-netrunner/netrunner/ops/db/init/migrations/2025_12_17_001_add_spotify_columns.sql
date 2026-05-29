-- Migration: add provider-specific columns to sources for Spotify
-- Never modify base schema; forward-only migration

ALTER TABLE sources
    ADD COLUMN IF NOT EXISTS provider TEXT,
    ADD COLUMN IF NOT EXISTS external_id TEXT,
    ADD COLUMN IF NOT EXISTS snapshot_id TEXT;

CREATE INDEX IF NOT EXISTS idx_sources_provider_external
    ON sources(provider, external_id);

COMMENT ON COLUMN sources.provider IS 'Optional provider name (e.g., spotify)';
COMMENT ON COLUMN sources.external_id IS 'External provider ID (e.g., Spotify playlist ID)';
COMMENT ON COLUMN sources.snapshot_id IS 'Provider change token/snapshot (e.g., Spotify snapshot_id)';
