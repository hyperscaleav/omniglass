-- migrate:up
-- #733 / #728: an alarm carries an acknowledgement, the record that a HUMAN has
-- seen it.
--
-- It is deliberately orthogonal to `cleared_at`. An alarm's raised state belongs
-- to its CONDITION (ADR-0075: the one-open invariant is per (component,
-- dedup_key), and the raiser owns the key), while the acknowledgement is a fact
-- about a person, so the two are separate nullable columns and neither write
-- touches the other: an alarm can be raised and unacknowledged (nobody has
-- looked, the queue an operator works), raised and acknowledged (somebody is on
-- it, still broken), or cleared without ever being acknowledged (it came and
-- went). That is why this is not a `status` enum: an enum would force the two
-- facts into one column and make "acknowledged" and "cleared" mutually
-- exclusive, which they are not.
--
-- acknowledged_by is the principal, ON DELETE SET NULL so purging a principal
-- leaves the acknowledgement time intact rather than cascading the incident
-- away; the durable "who" survives the purge in audit_log, which denormalizes
-- the actor's label at write time. The read resolves the current label through
-- principal_label(), the same lookup the audit write uses.
--
-- No index. The acknowledgement is read and filtered per component, alongside
-- the existing alarm_active_idx partial index on (component_id) where the row is
-- open, so the unacknowledged queue is a filter over one component's small open
-- set rather than an estate-wide scan. A cross-estate alarm queue would need one;
-- there is no such route (#728 kept the read component-scoped), and it is a new
-- migration when there is.
ALTER TABLE alarm ADD COLUMN IF NOT EXISTS acknowledged_at timestamp with time zone;
ALTER TABLE alarm ADD COLUMN IF NOT EXISTS acknowledged_by uuid;

DO $$
BEGIN
    ALTER TABLE alarm ADD CONSTRAINT alarm_acknowledged_by_fkey
        FOREIGN KEY (acknowledged_by) REFERENCES principal(id) ON DELETE SET NULL;
EXCEPTION
    WHEN duplicate_object THEN NULL;
END
$$;

-- migrate:down
ALTER TABLE alarm DROP CONSTRAINT IF EXISTS alarm_acknowledged_by_fkey;
ALTER TABLE alarm DROP COLUMN IF EXISTS acknowledged_by;
ALTER TABLE alarm DROP COLUMN IF EXISTS acknowledged_at;
