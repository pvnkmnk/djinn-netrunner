-- Migration: acoustid_score means "the confidence measured", and NULL means unmeasured
--
-- Phase 8 declared this column `INT DEFAULT 0`. The application never ran a
-- fingerprint lookup: the runtime image shipped no fpcalc from the project's
-- first commit, so every acquisition in every deployment stored the default and
-- reported a score of zero that nobody had ever measured. The default is what
-- made that possible for any writer that omitted the column, and the column
-- read as populated while being permanently empty.
--
-- The semantics are now: NULL = the lookup produced no measurement (no
-- fingerprint, no API key, the lookup failed, or AcoustID had no match);
-- a number = the 0-100 confidence AcoustID actually returned, zero included.
-- Dropping the default is what keeps a bare INSERT from inventing a
-- measurement. The Go model is `*int`, and AutoMigrate is additive so it will
-- not drop a column default on an existing database - see the matching
-- ALTER in backend/internal/database/migrate.go, which is how already-deployed
-- databases lose it.

ALTER TABLE acquisitions ALTER COLUMN acoustid_score DROP DEFAULT;

COMMENT ON COLUMN acquisitions.acoustid_score IS
    'AcoustID confidence score (0-100) from a fingerprint lookup; NULL = never scored (no fingerprint, no API key, failed lookup, or no match)';
