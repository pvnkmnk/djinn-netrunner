-- DJI-641: the boot-time bootstrap must require proof, not just an address.
--
-- BOOTSTRAP_ADMIN_EMAIL names an account and proves nothing about who holds it.
-- Gating registration on BOOTSTRAP_ADMIN_SECRET closed the registration trigger,
-- but a boot has no enrollment code to compare: it promoted whatever account sat
-- at the configured address. Observed on a live stack - an account registered
-- with a wrong code stayed `user`, and the next restart of ops-web made it
-- `admin`. The hole was deferred by a restart, not closed.
--
-- This column is that proof. Registration writes it when the code matches, and
-- the boot promotes only accounts that carry it, so the two triggers cannot
-- disagree about who the operator meant.
--
-- Nullable with no DEFAULT on purpose: NULL is the meaningful value "never
-- proved anything", which is every account that exists today and every account
-- registration creates by default. A DEFAULT would let a bare INSERT invent a
-- proof, which is the failure mode the acoustid_score migration in this
-- directory also documents.

ALTER TABLE users ADD COLUMN IF NOT EXISTS bootstrap_enrolled_at TIMESTAMPTZ;

COMMENT ON COLUMN users.bootstrap_enrolled_at IS
    'When this account presented BOOTSTRAP_ADMIN_SECRET at registration; NULL = never proved it, so the boot-time bootstrap leaves it alone';
