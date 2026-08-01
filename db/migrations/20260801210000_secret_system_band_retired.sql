-- migrate:up
-- ADR-0052: secrets lose the system band (a shared device has one credential;
-- the room it serves is the wrong owner). The resolver has had no system arm
-- since that decision, so a system-owned secret resolves onto nothing: any
-- existing rows are unreadable dead data and are removed rather than stranded
-- behind the narrowed CHECK (irreversible for those rows; they had no reader
-- to lose). The owner-arc CHECK keeps its shape; with the kind unreachable its
-- system arm is inert, and the down migration restores reachability.
DELETE FROM secret WHERE owner_kind = 'system';
ALTER TABLE secret DROP CONSTRAINT IF EXISTS secret_owner_kind_check;
ALTER TABLE secret ADD CONSTRAINT secret_owner_kind_check
    CHECK ((owner_kind = ANY (ARRAY['platform'::text, 'component'::text, 'location'::text])));

-- migrate:down
ALTER TABLE secret DROP CONSTRAINT IF EXISTS secret_owner_kind_check;
ALTER TABLE secret ADD CONSTRAINT secret_owner_kind_check
    CHECK ((owner_kind = ANY (ARRAY['platform'::text, 'component'::text, 'system'::text, 'location'::text])));
