-- Djinn NETRUNNER Database Functions
-- Helper functions for job management and notifications

-- Function to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER sources_updated_at
    BEFORE UPDATE ON sources
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Function to emit notifications on job log inserts
CREATE OR REPLACE FUNCTION notify_job_log()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('opsevents', json_build_object(
        'event', 'job_log',
        'job_id', NEW.job_id,
        'log_id', NEW.id
    )::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER job_log_notify
    AFTER INSERT ON joblogs
    FOR EACH ROW
    EXECUTE FUNCTION notify_job_log();

-- Function to emit notifications on job state changes
CREATE OR REPLACE FUNCTION notify_job_state_change()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.state IS DISTINCT FROM NEW.state THEN
        PERFORM pg_notify('opsevents', json_build_object(
            'event', 'job_state_change',
            'job_id', NEW.id,
            'old_state', OLD.state,
            'new_state', NEW.state
        )::text);
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER job_state_change_notify
    AFTER UPDATE ON jobs
    FOR EACH ROW
    EXECUTE FUNCTION notify_job_state_change();

-- Function to wake up workers when new jobs are queued
CREATE OR REPLACE FUNCTION notify_worker_wakeup()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.state = 'queued' THEN
        PERFORM pg_notify('opswakeup', json_build_object(
            'event', 'job_queued',
            'job_id', NEW.id,
            'jobtype', NEW.jobtype
        )::text);
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER job_queued_wakeup
    AFTER INSERT ON jobs
    FOR EACH ROW
    EXECUTE FUNCTION notify_worker_wakeup();

-- Helper function to claim next available job (SKIP LOCKED pattern)
CREATE OR REPLACE FUNCTION claim_next_job(
    p_worker_id TEXT,
    p_jobtypes jobtype[] DEFAULT NULL
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
          AND (p_jobtypes IS NULL OR jobtype = ANY(p_jobtypes))
        ORDER BY requested_at ASC
        FOR UPDATE SKIP LOCKED
        LIMIT 1
    )
    RETURNING id INTO v_job_id;

    RETURN v_job_id;
END;
$$ LANGUAGE plpgsql;

-- Helper function to claim next job item for a job (SKIP LOCKED pattern)
CREATE OR REPLACE FUNCTION claim_next_jobitem(
    p_job_id BIGINT
)
RETURNS BIGINT AS $$
DECLARE
    v_item_id BIGINT;
BEGIN
    UPDATE jobitems
    SET status = 'searching',
        started_at = NOW()
    WHERE id = (
        SELECT id
        FROM jobitems
        WHERE job_id = p_job_id
          AND status = 'queued'
        ORDER BY sequence ASC
        FOR UPDATE SKIP LOCKED
        LIMIT 1
    )
    RETURNING id INTO v_item_id;

    RETURN v_item_id;
END;
$$ LANGUAGE plpgsql;

-- Helper function to append job log
CREATE OR REPLACE FUNCTION append_job_log(
    p_job_id BIGINT,
    p_level TEXT,
    p_message TEXT,
    p_jobitem_id BIGINT DEFAULT NULL
)
RETURNS BIGINT AS $$
DECLARE
    v_log_id BIGINT;
BEGIN
    INSERT INTO joblogs(job_id, level, message, jobitem_id)
    VALUES (p_job_id, p_level, p_message, p_jobitem_id)
    RETURNING id INTO v_log_id;

    RETURN v_log_id;
END;
$$ LANGUAGE plpgsql;

-- Helper to compute advisory lock key from scope
CREATE OR REPLACE FUNCTION scope_lock_key(
    p_scope_type TEXT,
    p_scope_id TEXT
)
RETURNS BIGINT AS $$
BEGIN
    -- Use namespace 1001 for scope locks
    -- Hash the scope identifier to get a deterministic lock key
    RETURN 1001 * 1000000000::BIGINT +
           ('x' || substr(md5(p_scope_type || ':' || p_scope_id), 1, 8))::bit(32)::BIGINT;
END;
$$ LANGUAGE plpgsql IMMUTABLE;
