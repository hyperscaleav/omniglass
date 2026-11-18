-- migrate:up

-- A role as a notification target (#1987). A target of kind role reaches every
-- person holding the role, directly or through a group grant, read at each
-- alarm transition as a user_group target reads its members, on the channels
-- it names (inbox, email). A bound role cannot be deleted from under a target
-- (RESTRICT), as a bound group cannot. The kind CHECK is replaced whole (drop,
-- then add) so it holds every kind to its own columns.
--
-- official marks a target the boot seed ships: like an official action rule,
-- its seed-owned fields are read-only and it cannot be deleted, so the seed
-- never silently undoes an operator's edit on the next boot. Carrying official
-- makes the table a catalog (#1431, #1432): a shipped row lives in og, an
-- operator's in local, a name is unique within its namespace, and a bare name
-- resolves along the search path, so an operator's target may take a shipped
-- name and shadow it.
ALTER TABLE public.notification_target ADD COLUMN IF NOT EXISTS role_id uuid;
ALTER TABLE public.notification_target ADD COLUMN IF NOT EXISTS official boolean NOT NULL DEFAULT false;
ALTER TABLE public.notification_target ADD COLUMN IF NOT EXISTS namespace text NOT NULL DEFAULT 'local';

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'notification_target_namespace_check') THEN
        ALTER TABLE public.notification_target ADD CONSTRAINT notification_target_namespace_check
            CHECK (namespace ~ '^[a-z0-9][a-z0-9-]*$');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'notification_target_og_official_check') THEN
        ALTER TABLE public.notification_target ADD CONSTRAINT notification_target_og_official_check
            CHECK (namespace <> 'og' OR official);
    END IF;
    ALTER TABLE public.notification_target DROP CONSTRAINT IF EXISTS notification_target_name_key;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'notification_target_namespace_name_key') THEN
        ALTER TABLE public.notification_target ADD CONSTRAINT notification_target_namespace_name_key UNIQUE (namespace, name);
    END IF;
END $$;

-- What a bare name resolves to: the operator's local rows, then the og rows no
-- local row of the same name shadows.
CREATE OR REPLACE FUNCTION public.notification_target_in_effect() RETURNS SETOF public.notification_target
    LANGUAGE sql STABLE AS $$
    SELECT t.* FROM public.notification_target t
    WHERE t.namespace = 'local'
       OR (t.namespace = 'og' AND NOT EXISTS (
            SELECT 1 FROM public.notification_target l WHERE l.namespace = 'local' AND l.name = t.name))
$$;

DO $$
BEGIN
    ALTER TABLE public.notification_target ADD CONSTRAINT notification_target_role_id_fkey
        FOREIGN KEY (role_id) REFERENCES public.role(id) ON DELETE RESTRICT;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

ALTER TABLE public.notification_target DROP CONSTRAINT IF EXISTS notification_target_kind_check;
ALTER TABLE public.notification_target ADD CONSTRAINT notification_target_kind_check CHECK (
    (kind = 'webhook' AND url IS NOT NULL AND user_group_id IS NULL AND user_id IS NULL AND role_id IS NULL
      AND cardinality(channels) = 0 AND cardinality(addresses) = 0)
    OR (kind = 'email' AND url IS NULL AND user_group_id IS NULL AND user_id IS NULL AND role_id IS NULL
      AND secret_id IS NULL AND cardinality(channels) = 0 AND cardinality(addresses) > 0)
    OR (kind = 'user_group' AND user_group_id IS NOT NULL AND url IS NULL AND user_id IS NULL AND role_id IS NULL
      AND secret_id IS NULL AND cardinality(addresses) = 0 AND cardinality(channels) > 0
      AND channels <@ ARRAY['inbox', 'email'])
    OR (kind = 'user' AND user_id IS NOT NULL AND url IS NULL AND user_group_id IS NULL AND role_id IS NULL
      AND secret_id IS NULL AND cardinality(addresses) = 0 AND cardinality(channels) > 0
      AND channels <@ ARRAY['inbox', 'email'])
    OR (kind = 'role' AND role_id IS NOT NULL AND url IS NULL AND user_group_id IS NULL AND user_id IS NULL
      AND secret_id IS NULL AND cardinality(addresses) = 0 AND cardinality(channels) > 0
      AND channels <@ ARRAY['inbox', 'email']));

CREATE INDEX IF NOT EXISTS notification_target_role_idx ON public.notification_target (role_id)
    WHERE role_id IS NOT NULL;

-- migrate:down

DROP FUNCTION IF EXISTS public.notification_target_in_effect();
DROP INDEX IF EXISTS public.notification_target_role_idx;
DELETE FROM public.action_rule_target WHERE target_id IN (SELECT id FROM public.notification_target WHERE kind = 'role');
DELETE FROM public.notification_target WHERE kind = 'role';
ALTER TABLE public.notification_target DROP CONSTRAINT IF EXISTS notification_target_namespace_name_key;
ALTER TABLE public.notification_target DROP CONSTRAINT IF EXISTS notification_target_og_official_check;
ALTER TABLE public.notification_target DROP CONSTRAINT IF EXISTS notification_target_namespace_check;
ALTER TABLE public.notification_target DROP COLUMN IF EXISTS namespace;
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'notification_target_name_key') THEN
        ALTER TABLE public.notification_target ADD CONSTRAINT notification_target_name_key UNIQUE (name);
    END IF;
END $$;
ALTER TABLE public.notification_target DROP CONSTRAINT IF EXISTS notification_target_kind_check;
ALTER TABLE public.notification_target ADD CONSTRAINT notification_target_kind_check CHECK (
    (kind = 'webhook' AND url IS NOT NULL AND user_group_id IS NULL AND user_id IS NULL
      AND cardinality(channels) = 0 AND cardinality(addresses) = 0)
    OR (kind = 'email' AND url IS NULL AND user_group_id IS NULL AND user_id IS NULL
      AND secret_id IS NULL AND cardinality(channels) = 0 AND cardinality(addresses) > 0)
    OR (kind = 'user_group' AND user_group_id IS NOT NULL AND url IS NULL AND user_id IS NULL
      AND secret_id IS NULL AND cardinality(addresses) = 0 AND cardinality(channels) > 0
      AND channels <@ ARRAY['inbox', 'email'])
    OR (kind = 'user' AND user_id IS NOT NULL AND url IS NULL AND user_group_id IS NULL
      AND secret_id IS NULL AND cardinality(addresses) = 0 AND cardinality(channels) > 0
      AND channels <@ ARRAY['inbox', 'email']));
ALTER TABLE public.notification_target DROP CONSTRAINT IF EXISTS notification_target_role_id_fkey;
ALTER TABLE public.notification_target DROP COLUMN IF EXISTS official;
ALTER TABLE public.notification_target DROP COLUMN IF EXISTS role_id;
