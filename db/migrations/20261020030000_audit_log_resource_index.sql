-- migrate:up

-- An alarm's timeline reads the audit rows of one alarm (#950): its raise,
-- acknowledgement and clear, keyed by the alarm's id. Until now the audit log
-- was only read newest first, across every resource. The index is partial,
-- alarm rows only, the one resource read this way.
--
-- Upgrade cost: the build scans the whole audit log once and indexes only its
-- alarm rows, inside the migration's transaction, holding a lock that blocks
-- audit writes (every audited write) for the length of the scan. On a large
-- log, schedule the upgrade for a quiet window; there is no CONCURRENTLY
-- migration precedent here, and dbmate wraps each migration in a transaction.
CREATE INDEX IF NOT EXISTS audit_log_alarm_idx ON public.audit_log (resource_id) WHERE resource = 'alarm';

-- migrate:down

DROP INDEX IF EXISTS public.audit_log_alarm_idx;
