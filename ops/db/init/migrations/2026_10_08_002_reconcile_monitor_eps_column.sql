-- DJI-618 / DJI-635: the SQL bootstrap and the Go model disagreed about one column name.
--
-- 2026_03_04_006_artist_tracking.sql created monitored_artists.monitor_eps.
-- GORM's AutoMigrate derived monitor_e_ps from MonitoredArtist.MonitorEPs,
-- because the field carried no `column:` tag and GORM's naming strategy does not
-- read "EP" as an initialism. AutoMigrate is additive, so a database that ran
-- both ended up with two boolean columns: every write made through GORM landed
-- in monitor_e_ps, while monitor_eps stayed at its NOT NULL DEFAULT TRUE and
-- anything reading the SQL name saw a setting nobody had chosen.
--
-- The model now carries `gorm:"column:monitor_eps"`, so this file only repairs
-- databases that already drifted. The application-written value wins: GORM has
-- been writing monitor_e_ps, so that one is the live value and the bootstrap's
-- column is the stale copy.
--
-- Two places reconcile this and they must not diverge. This file covers a
-- database bootstrapped from ops/db/init/; database.Migrate covers every start of
-- the binaries, which is how a deployment that has only ever run ops-web and
-- ops-worker gets repaired. Both run before the AutoMigrate pass over
-- MonitoredArtist, or AutoMigrate sees no monitor_eps and adds it back.
--
-- Written as a DO block rather than ADD COLUMN IF NOT EXISTS because the repair
-- is conditional in two directions and Postgres has no conditional RENAME.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'monitored_artists' AND column_name = 'monitor_e_ps'
    ) THEN
        IF EXISTS (
            SELECT 1 FROM information_schema.columns
            WHERE table_name = 'monitored_artists' AND column_name = 'monitor_eps'
        ) THEN
            UPDATE monitored_artists SET monitor_eps = monitor_e_ps;
            ALTER TABLE monitored_artists DROP COLUMN monitor_e_ps;
        ELSE
            ALTER TABLE monitored_artists RENAME COLUMN monitor_e_ps TO monitor_eps;
        END IF;
    END IF;
END $$;

COMMENT ON COLUMN monitored_artists.monitor_eps IS
    'Whether this artist''s EPs are monitored; the Go model tags MonitorEPs with this name, so the two schema sources agree (DJI-618)';
