-- migrate:up
-- Identity provider groups map to Omniglass groups (#2363, ADR-2361 point
-- 4). A mapping keys a value of the configured groups claim, by the issuer
-- that sends it, to a user_group. A membership gains its source: `local`
-- (an admin added it, the default for every existing row) or `idp` (a
-- sign-in or a re-check refresh synced it from a mapping), and an idp row
-- names the issuer whose claim granted it, so the sync replaces only that
-- issuer's idp rows and never a local one.
CREATE TABLE IF NOT EXISTS public.user_group_idp_mapping (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    group_id uuid NOT NULL REFERENCES public.user_group (id) ON DELETE CASCADE,
    issuer text NOT NULL CHECK (issuer <> ''),
    claim_value text NOT NULL CHECK (claim_value <> ''),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (group_id, issuer, claim_value)
);
CREATE INDEX IF NOT EXISTS user_group_idp_mapping_claim_idx
    ON public.user_group_idp_mapping (issuer, claim_value);

ALTER TABLE public.user_group_member ADD COLUMN IF NOT EXISTS source text NOT NULL DEFAULT 'local';
ALTER TABLE public.user_group_member ADD COLUMN IF NOT EXISTS issuer text;
ALTER TABLE public.user_group_member DROP CONSTRAINT IF EXISTS user_group_member_source_check;
ALTER TABLE public.user_group_member ADD CONSTRAINT user_group_member_source_check
    CHECK (CASE WHEN source = 'idp' THEN issuer IS NOT NULL AND issuer <> ''
                WHEN source = 'local' THEN issuer IS NULL
                ELSE false END);
CREATE INDEX IF NOT EXISTS user_group_member_idp_idx
    ON public.user_group_member (user_id, issuer) WHERE source = 'idp';

-- migrate:down
DROP INDEX IF EXISTS public.user_group_member_idp_idx;
ALTER TABLE public.user_group_member DROP CONSTRAINT IF EXISTS user_group_member_source_check;
ALTER TABLE public.user_group_member DROP COLUMN IF EXISTS issuer;
ALTER TABLE public.user_group_member DROP COLUMN IF EXISTS source;
DROP TABLE IF EXISTS public.user_group_idp_mapping;
