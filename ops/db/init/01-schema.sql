-- Djinn NETRUNNER Database Schema
-- Creates tables for job orchestration, logging, and acquisition tracking

-- Job types
CREATE TYPE jobtype AS ENUM ('sync', 'acquisition', 'import', 'index_refresh');

-- Job states (canonical state machine)
CREATE TYPE jobstate AS ENUM ('queued', 'running', 'succeeded', 'failed', 'cancelled');

-- Job item states (canonical state machine)
CREATE TYPE jobitemstatus AS ENUM ('queued', 'searching', 'downloading', 'imported', 'skipped', 'failed');

-- Jobs: durable job records with state machine + execution metadata
CREATE TABLE jobs (
    id BIGSERIAL PRIMARY KEY,
    jobtype jobtype NOT NULL,
    state jobstate NOT NULL DEFAULT 'queued',

    -- Timestamps
    requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    heartbeat_at TIMESTAMPTZ,

    -- Execution metadata
    worker_id TEXT,
    attempt INT NOT NULL DEFAULT 0,
    max_attempts INT NOT NULL DEFAULT 3,

    -- Job context
    scope_type TEXT,  -- e.g., 'playlist', 'source'
    scope_id TEXT,    -- identifier for the scope (playlist ID, source ID)
    params JSONB,     -- job-specific parameters

    -- Result summary
    summary TEXT,
    error_detail TEXT,

    -- Auditing
    created_by TEXT,

    CONSTRAINT valid_state_transitions CHECK (
        (state = 'queued' AND started_at IS NULL) OR
        (state = 'running' AND started_at IS NOT NULL) OR
        (state IN ('succeeded', 'failed', 'cancelled') AND finished_at IS NOT NULL)
    )
);

-- Indexes for efficient claiming and monitoring
CREATE INDEX idx_jobs_state_requested ON jobs(state, requested_at) WHERE state = 'queued';
CREATE INDEX idx_jobs_running_heartbeat ON jobs(state, heartbeat_at) WHERE state = 'running';
CREATE INDEX idx_jobs_scope ON jobs(scope_type, scope_id);
CREATE INDEX idx_jobs_finished_at ON jobs(finished_at DESC) WHERE finished_at IS NOT NULL;

-- Job items: durable units of work (deterministic plan)
CREATE TABLE jobitems (
    id BIGSERIAL PRIMARY KEY,
    job_id BIGINT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    status jobitemstatus NOT NULL DEFAULT 'queued',

    -- Search/acquisition metadata
    normalized_query TEXT NOT NULL,  -- normalized search term
    artist TEXT,
    album TEXT,
    track_title TEXT,

    -- slskd integration
    slskd_search_id TEXT,
    slskd_download_id TEXT,

    -- File tracking
    download_path TEXT,
    final_path TEXT,

    -- Status tracking
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    failure_reason TEXT,
    retry_count INT NOT NULL DEFAULT 0,

    -- Ordering within job
    sequence INT NOT NULL,

    UNIQUE(job_id, sequence)
);

CREATE INDEX idx_jobitems_job_status ON jobitems(job_id, status);
CREATE INDEX idx_jobitems_claimable ON jobitems(job_id, status) WHERE status = 'queued';
CREATE INDEX idx_jobitems_slskd_download ON jobitems(slskd_download_id) WHERE slskd_download_id IS NOT NULL;

-- Job logs: append-only console lines
CREATE TABLE joblogs (
    id BIGSERIAL PRIMARY KEY,
    job_id BIGINT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    ts TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    level TEXT NOT NULL DEFAULT 'INFO',  -- OK, INFO, ERR
    message TEXT NOT NULL,
    jobitem_id BIGINT REFERENCES jobitems(id) ON DELETE SET NULL
);

CREATE INDEX idx_joblogs_job_ts ON joblogs(job_id, ts DESC);
CREATE INDEX idx_joblogs_ts ON joblogs(ts);  -- for cleanup queries

-- Acquisitions: provenance and final-path record for imported items
CREATE TABLE acquisitions (
    id BIGSERIAL PRIMARY KEY,
    job_id BIGINT NOT NULL REFERENCES jobs(id),
    jobitem_id BIGINT NOT NULL REFERENCES jobitems(id),

    -- Metadata
    artist TEXT NOT NULL,
    album TEXT,
    track_title TEXT NOT NULL,

    -- File tracking
    original_path TEXT NOT NULL,
    final_path TEXT NOT NULL,
    file_size BIGINT,
    file_hash TEXT,

    -- Timestamps
    acquired_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    imported_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Source tracking
    source_user TEXT,
    source_ip TEXT
);

CREATE INDEX idx_acquisitions_job ON acquisitions(job_id);
CREATE INDEX idx_acquisitions_final_path ON acquisitions(final_path);
CREATE INDEX idx_acquisitions_imported_at ON acquisitions(imported_at DESC);

-- Sources: tracked playlists/sources for sync
CREATE TABLE sources (
    id BIGSERIAL PRIMARY KEY,
    source_type TEXT NOT NULL,  -- 'spotify_playlist', 'file_list', etc.
    source_uri TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,

    -- Sync metadata
    last_synced_at TIMESTAMPTZ,
    sync_enabled BOOLEAN NOT NULL DEFAULT TRUE,

    -- Configuration
    config JSONB,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_sources_enabled ON sources(sync_enabled) WHERE sync_enabled = TRUE;

-- Advisory lock namespaces (documented for reference)
-- Namespace 1001: per-playlist/source sync scope lock
-- Key format: hash of scope_type || scope_id
COMMENT ON TABLE jobs IS 'Advisory lock namespace 1001 used for per-scope exclusivity during job execution';
