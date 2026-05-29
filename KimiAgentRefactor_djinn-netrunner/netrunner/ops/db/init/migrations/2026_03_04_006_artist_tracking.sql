-- Migration: Artist Tracking System (Lidarr-like features)

-- Update jobtype ENUM to include artist tracking jobs
ALTER TYPE jobtype ADD VALUE 'artist_scan';
ALTER TYPE jobtype ADD VALUE 'release_monitor';

-- Create quality_profiles table
CREATE TABLE IF NOT EXISTS quality_profiles (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name TEXT UNIQUE NOT NULL,
    description TEXT,
    prefer_lossless BOOLEAN NOT NULL DEFAULT TRUE,
    allowed_formats TEXT[] NOT NULL DEFAULT '{FLAC,MP3,AAC,OGG}',
    min_bitrate INTEGER NOT NULL DEFAULT 320,
    prefer_bitrate INTEGER,
    prefer_scene_releases BOOLEAN NOT NULL DEFAULT FALSE,
    prefer_web_releases BOOLEAN NOT NULL DEFAULT TRUE,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT valid_min_bitrate CHECK (min_bitrate >= 64 AND min_bitrate <= 2000),
    CONSTRAINT valid_prefer_bitrate CHECK (prefer_bitrate IS NULL OR (prefer_bitrate >= 64 AND prefer_bitrate <= 2000))
);

CREATE INDEX IF NOT EXISTS idx_quality_profiles_name ON quality_profiles(name);

-- Create monitored_artists table
CREATE TABLE IF NOT EXISTS monitored_artists (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    musicbrainz_id TEXT UNIQUE NOT NULL,
    name TEXT NOT NULL,
    sort_name TEXT,
    disambiguation TEXT,
    quality_profile_id UUID NOT NULL REFERENCES quality_profiles(id) ON DELETE RESTRICT,
    monitored BOOLEAN NOT NULL DEFAULT TRUE,
    monitor_new_releases BOOLEAN NOT NULL DEFAULT TRUE,
    monitor_albums BOOLEAN NOT NULL DEFAULT TRUE,
    monitor_eps BOOLEAN NOT NULL DEFAULT TRUE,
    monitor_singles BOOLEAN NOT NULL DEFAULT FALSE,
    monitor_compilations BOOLEAN NOT NULL DEFAULT FALSE,
    monitor_live BOOLEAN NOT NULL DEFAULT FALSE,
    last_scan_date TIMESTAMPTZ,
    last_release_check TIMESTAMPTZ,
    total_releases INTEGER NOT NULL DEFAULT 0,
    acquired_releases INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    owner_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    CONSTRAINT valid_total_releases CHECK (total_releases >= 0),
    CONSTRAINT valid_acquired_releases CHECK (acquired_releases >= 0),
    CONSTRAINT acquired_not_greater_than_total CHECK (acquired_releases <= total_releases)
);

CREATE INDEX IF NOT EXISTS idx_monitored_artists_musicbrainz_id ON monitored_artists(musicbrainz_id);
CREATE INDEX IF NOT EXISTS idx_monitored_artists_name ON monitored_artists(name);
CREATE INDEX IF NOT EXISTS idx_monitored_artists_owner ON monitored_artists(owner_user_id);

-- Create tracked_releases table
CREATE TABLE IF NOT EXISTS tracked_releases (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    artist_id UUID NOT NULL REFERENCES monitored_artists(id) ON DELETE CASCADE,
    musicbrainz_release_group_id TEXT NOT NULL,
    musicbrainz_release_id TEXT,
    title TEXT NOT NULL,
    release_type TEXT NOT NULL, -- album, ep, single, compilation, live, soundtrack, other
    release_date DATE,
    release_status TEXT NOT NULL DEFAULT 'official', -- official, promotion, bootleg, pseudo-release
    status TEXT NOT NULL DEFAULT 'wanted', -- wanted, searching, downloading, imported, ignored, failed
    monitored BOOLEAN NOT NULL DEFAULT TRUE,
    job_id BIGINT REFERENCES jobs(id) ON DELETE SET NULL,
    acquired_date TIMESTAMPTZ,
    file_path TEXT,
    acquired_format TEXT,
    acquired_bitrate INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(artist_id, musicbrainz_release_group_id)
);

CREATE INDEX IF NOT EXISTS idx_tracked_releases_artist_id ON tracked_releases(artist_id);
CREATE INDEX IF NOT EXISTS idx_tracked_releases_status ON tracked_releases(status);
CREATE INDEX IF NOT EXISTS idx_tracked_releases_job_id ON tracked_releases(job_id);

-- Add updated_at triggers
CREATE TRIGGER quality_profiles_updated_at
    BEFORE UPDATE ON quality_profiles
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER monitored_artists_updated_at
    BEFORE UPDATE ON monitored_artists
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER tracked_releases_updated_at
    BEFORE UPDATE ON tracked_releases
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Insert default quality profiles
INSERT INTO quality_profiles (name, description, prefer_lossless, allowed_formats, min_bitrate, is_default)
VALUES 
('Standard', 'High quality MP3 or better', false, '{MP3,FLAC,AAC,OGG}', 320, true),
('Lossless', 'FLAC only for audiophiles', true, '{FLAC}', 0, false),
('Any', 'Accept any quality', false, '{MP3,FLAC,AAC,OGG,M4A}', 128, false)
ON CONFLICT (name) DO NOTHING;
