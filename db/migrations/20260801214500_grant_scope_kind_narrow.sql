-- migrate:up
-- #461: scope_kind='group' (a group as the scope TARGET) is unbuilt and every
-- code path refuses it; stop the schema and the published enums offering it
-- until group-as-scope ships with its slice. The grant SUBJECT (group_id, a
-- group HOLDING a grant) is built and unaffected. No row can exist with the
-- value (no writer ever accepted it), so no data guard is needed beyond the
-- narrowed CHECK itself.
ALTER TABLE principal_grant DROP CONSTRAINT IF EXISTS principal_grant_scope_kind_check;
ALTER TABLE principal_grant ADD CONSTRAINT principal_grant_scope_kind_check
    CHECK ((scope_kind = ANY (ARRAY['all'::text, 'location'::text, 'system'::text, 'component'::text])));

-- migrate:down
ALTER TABLE principal_grant DROP CONSTRAINT IF EXISTS principal_grant_scope_kind_check;
ALTER TABLE principal_grant ADD CONSTRAINT principal_grant_scope_kind_check
    CHECK ((scope_kind = ANY (ARRAY['all'::text, 'location'::text, 'system'::text, 'component'::text, 'group'::text])));
