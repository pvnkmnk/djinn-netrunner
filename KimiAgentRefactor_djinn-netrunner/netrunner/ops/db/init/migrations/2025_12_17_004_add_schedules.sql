-- Migration: add schedules table for cron-like automatic syncs

CREATE TABLE IF NOT EXISTS schedules (
    id BIGSERIAL PRIMARY KEY,
    source_id BIGINT NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    cron_expr TEXT NOT NULL,
    timezone TEXT NOT NULL DEFAULT 'UTC',
    next_run_at TIMESTAMPTZ,
    last_run_at TIMESTAMPTZ,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(source_id, cron_expr)
);

CREATE INDEX IF NOT EXISTS idx_schedules_due ON schedules(enabled, next_run_at) WHERE enabled = TRUE;
CREATE INDEX IF NOT EXISTS idx_schedules_source ON schedules(source_id);

COMMENT ON TABLE schedules IS 'Cron-like schedules for enqueueing sync jobs per source';
