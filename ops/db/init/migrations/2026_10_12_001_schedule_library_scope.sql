-- A schedule used to be a watchlist sync and nothing else: watchlist_id was
-- NOT NULL and there was no job type at all.
--
-- It now also runs a library's scan or enrich, and the job type names the scope
-- it runs against ("sync" a watchlist, "scan"/"enrich" a library), so exactly
-- one of watchlist_id / library_id is set and watchlist_id has to be able to be
-- NULL. Every row that predates this file is a watchlist sync, which is what
-- the default and the UPDATE record.
--
-- IF EXISTS / IF NOT EXISTS keep this re-runnable, the promise every file in
-- this directory makes. Note also that `database.Migrate` performs the same
-- steps on every boot, because these scripts are not mounted by any compose
-- file (see AGENTS.md, "API & data contracts"): a deployed database gains the
-- shape from the migrator, and this file records what changed for an operator
-- applying it by hand.

ALTER TABLE IF EXISTS schedules
    ADD COLUMN IF NOT EXISTS job_type TEXT NOT NULL DEFAULT 'sync';

ALTER TABLE IF EXISTS schedules
    ADD COLUMN IF NOT EXISTS library_id UUID;

ALTER TABLE IF EXISTS schedules
    ALTER COLUMN watchlist_id DROP NOT NULL;

UPDATE schedules SET job_type = 'sync' WHERE job_type IS NULL OR job_type = '';
