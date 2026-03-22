-- Migration: Convert legacy PostgreSQL ENUM types to text for GORM compatibility
-- Background: 01-schema.sql created columns using ENUM types (jobstate, jobtype, jobitemstatus).
-- GORM models use string fields and cannot ALTER ENUM columns without explicit casting.
-- The schema uses column name "jobtype" but GORM expects "job_type".
-- This migration normalizes all job state columns to text so AutoMigrate can manage them.

BEGIN;

-- 0. Remove GORM's duplicate job_type column if it exists (created by partial AutoMigrate).
ALTER TABLE jobs DROP COLUMN IF EXISTS job_type;

-- 1. Drop objects that reference the ENUM types before altering columns.
-- Functions with ::jobstate casts or jobtype comparisons must go first.
DROP TRIGGER IF EXISTS job_queued_wakeup ON jobs;
DROP FUNCTION IF EXISTS notify_worker_wakeup();
DROP FUNCTION IF EXISTS claim_next_job(TEXT, jobtype[]);
DROP FUNCTION IF EXISTS claim_next_job(TEXT, TEXT[]);

-- 2. Drop the check constraint that references the jobstate ENUM type.
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS valid_state_transitions;

-- 3. Drop partial indexes that reference the ENUM type.
DROP INDEX IF EXISTS idx_jobs_running_heartbeat;
DROP INDEX IF EXISTS idx_jobs_state_requested;
DROP INDEX IF EXISTS idx_jobitems_claimable;

-- 4. Rename jobs.jobtype → jobs.job_type and convert from ENUM to text.
ALTER TABLE jobs ALTER COLUMN jobtype TYPE text USING jobtype::text;
ALTER TABLE jobs RENAME COLUMN jobtype TO job_type;

-- 5. Convert jobs.state from jobstate ENUM to text.
ALTER TABLE jobs ALTER COLUMN state TYPE text USING state::text;

-- 6. Convert jobitems.status from jobitemstatus ENUM to text.
ALTER TABLE jobitems ALTER COLUMN status TYPE text USING status::text;

-- 7. Drop column defaults that reference the ENUM types, then drop types.
ALTER TABLE jobs ALTER COLUMN state DROP DEFAULT;
ALTER TABLE jobitems ALTER COLUMN status DROP DEFAULT;
DROP TYPE IF EXISTS jobstate;
DROP TYPE IF EXISTS jobtype;
DROP TYPE IF EXISTS jobitemstatus;

-- 8. Recreate the state validation as a text-based check constraint.
ALTER TABLE jobs ADD CONSTRAINT valid_state_transitions CHECK (
    (state = 'queued' AND started_at IS NULL) OR
    (state = 'running' AND started_at IS NOT NULL) OR
    (state IN ('succeeded', 'failed', 'cancelled') AND finished_at IS NOT NULL)
);

-- 9. Recreate partial indexes (now with text comparison).
CREATE INDEX idx_jobs_running_heartbeat ON jobs(state, heartbeat_at) WHERE state = 'running';
CREATE INDEX idx_jobs_state_requested ON jobs(state, requested_at) WHERE state = 'queued';
CREATE INDEX idx_jobitems_claimable ON jobitems(job_id, status) WHERE status = 'queued';

-- 10. Recreate notify_worker_wakeup (uses NEW.job_type, not jobtype).
CREATE OR REPLACE FUNCTION notify_worker_wakeup()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.state = 'queued' THEN
        PERFORM pg_notify('opswakeup', json_build_object(
            'event', 'job_queued',
            'job_id', NEW.id,
            'jobtype', NEW.job_type
        )::text);
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER job_queued_wakeup
    AFTER INSERT ON jobs
    FOR EACH ROW
    EXECUTE FUNCTION notify_worker_wakeup();

-- 11. Recreate claim_next_job with TEXT[] parameter.
CREATE OR REPLACE FUNCTION claim_next_job(
    p_worker_id TEXT,
    p_jobtypes TEXT[] DEFAULT NULL
)
RETURNS BIGINT AS $$
DECLARE
    v_job_id BIGINT;
BEGIN
    UPDATE jobs
    SET state = 'running',
        started_at = NOW(),
        heartbeat_at = NOW(),
        worker_id = p_worker_id,
        attempt = attempt + 1
    WHERE id = (
        SELECT id
        FROM jobs
        WHERE state = 'queued'
          AND (p_jobtypes IS NULL OR job_type = ANY(p_jobtypes))
        ORDER BY requested_at ASC
        FOR UPDATE SKIP LOCKED
        LIMIT 1
    )
    RETURNING id INTO v_job_id;

    RETURN v_job_id;
END;
$$ LANGUAGE plpgsql;

COMMIT;
