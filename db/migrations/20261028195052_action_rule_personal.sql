-- migrate:up

-- Personal action rules (#1341, #1353): a rule a user owns, whose only
-- recipient is its owner, on the channels it names. owner_principal_id NULL
-- is a platform rule (today's); a personal rule goes with its owner (ON DELETE
-- CASCADE) and names no targets. Names are unique among platform rules, and
-- per owner among personal rules, so two users can each have a rule called
-- "my-displays".
ALTER TABLE public.action_rule ADD COLUMN IF NOT EXISTS owner_principal_id uuid;
ALTER TABLE public.action_rule ADD COLUMN IF NOT EXISTS channels text[] NOT NULL DEFAULT '{}';

DO $$
BEGIN
    ALTER TABLE public.action_rule ADD CONSTRAINT action_rule_owner_principal_id_fkey
        FOREIGN KEY (owner_principal_id) REFERENCES public.principal(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$
BEGIN
    ALTER TABLE public.action_rule ADD CONSTRAINT action_rule_channels_check CHECK (
        (owner_principal_id IS NULL AND cardinality(channels) = 0)
        OR (owner_principal_id IS NOT NULL AND cardinality(channels) > 0
            AND channels <@ ARRAY['inbox', 'email']::text[]));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

ALTER TABLE public.action_rule DROP CONSTRAINT IF EXISTS action_rule_name_key;
CREATE UNIQUE INDEX IF NOT EXISTS action_rule_platform_name_key ON public.action_rule (name) WHERE owner_principal_id IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS action_rule_owner_name_key ON public.action_rule (owner_principal_id, name) WHERE owner_principal_id IS NOT NULL;

-- migrate:down

DROP INDEX IF EXISTS public.action_rule_owner_name_key;
DROP INDEX IF EXISTS public.action_rule_platform_name_key;
ALTER TABLE public.action_rule DROP CONSTRAINT IF EXISTS action_rule_channels_check;
ALTER TABLE public.action_rule DROP COLUMN IF EXISTS channels;
ALTER TABLE public.action_rule DROP COLUMN IF EXISTS owner_principal_id;
