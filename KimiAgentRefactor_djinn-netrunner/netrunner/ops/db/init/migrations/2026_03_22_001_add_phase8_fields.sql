-- Migration: add Phase 8 fields (AcoustID fingerprinting + library quotas)
-- Phase 8 introduced:
--   - tracks.fingerprint          — AcoustID audio fingerprint
--   - acquisitions.acoustid_score — AcoustID confidence score (0-100)
--   - libraries.max_size_bytes    — Optional per-library disk quota cap
--   - libraries.quota_alert_at     — Alert threshold percentage (default 80)
--
-- This script is for Postgres bootstrap via docker-entrypoint-initdb.d.
-- For existing deployments, GORM AutoMigrate handles these columns automatically.

-- Add fingerprint column to tracks
ALTER TABLE tracks ADD COLUMN IF NOT EXISTS fingerprint TEXT;

COMMENT ON COLUMN tracks.fingerprint IS 'AcoustID audio fingerprint for track identification';

-- Add acoustid_score column to acquisitions
ALTER TABLE acquisitions ADD COLUMN IF NOT EXISTS acoustid_score INT DEFAULT 0;

COMMENT ON COLUMN acquisitions.acoustid_score IS 'AcoustID confidence score (0-100) from fingerprint lookup';

-- Add library quota columns
ALTER TABLE libraries ADD COLUMN IF NOT EXISTS max_size_bytes BIGINT;

COMMENT ON COLUMN libraries.max_size_bytes IS 'Optional disk quota cap for this library (nil = unlimited)';

ALTER TABLE libraries ADD COLUMN IF NOT EXISTS quota_alert_at INT DEFAULT 80;

COMMENT ON COLUMN libraries.quota_alert_at IS 'Alert threshold percentage (1-100) for disk quota warnings';
