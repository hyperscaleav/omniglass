-- migrate:up

-- A role as a notification target (#1987). A target of kind role reaches every
-- person holding the role, directly or through a group grant, read at each
-- alarm transition as a group target reads its members, on the channels it
-- names (inbox, email). A bound role cannot be deleted from under a target
-- (RESTRICT), as a bound group cannot. The kind CHECK is replaced whole (drop,
-- then add) so it holds every kind to its own columns.
ALTER TABLE public.notification_target ADD COLUMN IF NOT EXISTS role_id uuid;

DO $$
BEGIN
    ALTER TABLE public.notification_target ADD CONSTRAINT notification_target_role_id_fkey
        FOREIGN KEY (role_id) REFERENCES public.role(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

ALTER TABLE public.notification_target DROP CONSTRAINT IF EXISTS notification_target_kind_check;
ALTER TABLE public.notification_target ADD CONSTRAINT notification_target_kind_check CHECK (
    (kind = 'webhook' AND url IS NOT NULL AND principal_group_id IS NULL AND principal_id IS NULL AND role_id IS NULL
        AND cardinality(channels) = 0 AND cardinality(addresses) = 0)
    OR (kind = 'email' AND url IS NULL AND principal_group_id IS NULL AND principal_id IS NULL AND role_id IS NULL
        AND secret_id IS NULL AND cardinality(channels) = 0 AND cardinality(addresses) > 0)
    OR (kind = 'principal_group' AND principal_group_id IS NOT NULL AND url IS NULL AND principal_id IS NULL AND role_id IS NULL
        AND secret_id IS NULL AND cardinality(addresses) = 0
        AND cardinality(channels) > 0 AND channels <@ ARRAY['inbox', 'email']::text[])
    OR (kind = 'principal' AND principal_id IS NOT NULL AND url IS NULL AND principal_group_id IS NULL AND role_id IS NULL
        AND secret_id IS NULL AND cardinality(addresses) = 0
        AND cardinality(channels) > 0 AND channels <@ ARRAY['inbox', 'email']::text[])
    OR (kind = 'role' AND role_id IS NOT NULL AND url IS NULL AND principal_group_id IS NULL AND principal_id IS NULL
        AND secret_id IS NULL AND cardinality(addresses) = 0
        AND cardinality(channels) > 0 AND channels <@ ARRAY['inbox', 'email']::text[]));

CREATE INDEX IF NOT EXISTS notification_target_role_idx ON public.notification_target (role_id)
    WHERE role_id IS NOT NULL;

-- migrate:down

DROP INDEX IF EXISTS public.notification_target_role_idx;
DELETE FROM public.action_rule_target WHERE target_id IN (SELECT id FROM public.notification_target WHERE kind = 'role');
DELETE FROM public.notification_target WHERE kind = 'role';
ALTER TABLE public.notification_target DROP CONSTRAINT IF EXISTS notification_target_kind_check;
ALTER TABLE public.notification_target ADD CONSTRAINT notification_target_kind_check CHECK (
    (kind = 'webhook' AND url IS NOT NULL AND principal_group_id IS NULL AND principal_id IS NULL
        AND cardinality(channels) = 0 AND cardinality(addresses) = 0)
    OR (kind = 'email' AND url IS NULL AND principal_group_id IS NULL AND principal_id IS NULL
        AND secret_id IS NULL AND cardinality(channels) = 0 AND cardinality(addresses) > 0)
    OR (kind = 'principal_group' AND principal_group_id IS NOT NULL AND url IS NULL AND principal_id IS NULL
        AND secret_id IS NULL AND cardinality(addresses) = 0
        AND cardinality(channels) > 0 AND channels <@ ARRAY['inbox', 'email']::text[])
    OR (kind = 'principal' AND principal_id IS NOT NULL AND url IS NULL AND principal_group_id IS NULL
        AND secret_id IS NULL AND cardinality(addresses) = 0
        AND cardinality(channels) > 0 AND channels <@ ARRAY['inbox', 'email']::text[]));
ALTER TABLE public.notification_target DROP CONSTRAINT IF EXISTS notification_target_role_id_fkey;
ALTER TABLE public.notification_target DROP COLUMN IF EXISTS role_id;
