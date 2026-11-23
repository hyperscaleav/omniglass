-- migrate:up
-- Single sign-on through an OIDC identity provider (#2362, ADR-2361). An
-- identity at the provider is a credential of kind oidc: its identifier is
-- `iss|sub` exactly as the id_token carries them, it holds no Omniglass
-- secret (so no secret_hash and no expiry), and a unique index makes one
-- identity one person. linked_by records who made the link, so granting a
-- person owner can drop every link someone else made for them.
--
-- The link also keeps the provider's latest refresh token, sealed with the
-- install's key (refresh_sealed), the time the provider last accepted it, and
-- the moment past which, with no accepted refresh since, a credential through
-- the link is refused (recheck_grace_until). A session or API token minted
-- through a link names it (via_link); deleting the link ends them by cascade.
ALTER TABLE public.credential DROP CONSTRAINT IF EXISTS credential_kind_check;
ALTER TABLE public.credential ADD CONSTRAINT credential_kind_check
    CHECK (kind = ANY (ARRAY['bearer'::text, 'password'::text, 'oidc'::text]));
ALTER TABLE public.credential ADD COLUMN IF NOT EXISTS identifier text;
ALTER TABLE public.credential ALTER COLUMN secret_hash DROP NOT NULL;
ALTER TABLE public.credential DROP CONSTRAINT IF EXISTS credential_secret_shape_check;
ALTER TABLE public.credential ADD CONSTRAINT credential_secret_shape_check
    CHECK (CASE WHEN kind = 'oidc' THEN identifier IS NOT NULL AND secret_hash IS NULL
                ELSE secret_hash IS NOT NULL END);
CREATE UNIQUE INDEX IF NOT EXISTS credential_kind_identifier_key
    ON public.credential (kind, identifier) WHERE identifier IS NOT NULL;
ALTER TABLE public.credential ADD COLUMN IF NOT EXISTS linked_by uuid REFERENCES public.users (id) ON DELETE SET NULL;
ALTER TABLE public.credential ADD COLUMN IF NOT EXISTS refresh_sealed jsonb;
ALTER TABLE public.credential ADD COLUMN IF NOT EXISTS refresh_accepted_at timestamptz;
ALTER TABLE public.credential ADD COLUMN IF NOT EXISTS recheck_grace_until timestamptz;
ALTER TABLE public.credential ADD COLUMN IF NOT EXISTS via_link uuid REFERENCES public.credential (id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS credential_via_link_idx ON public.credential (via_link) WHERE via_link IS NOT NULL;

-- oidc_state is one sign-in between the redirect to the provider and its
-- callback. Only hashes are kept: the state's, and the browser binding's (the
-- value in the short-lived cookie start sets, from which the PKCE verifier
-- and the nonce are derived, so neither is stored). provider names the
-- provider the flow is for; link_user_id and link_session_hash mark a
-- self-link flow, bound to the session that started it. A row is consumed
-- once.
CREATE TABLE IF NOT EXISTS public.oidc_state (
    state_hash bytea PRIMARY KEY,
    binding_hash bytea NOT NULL,
    provider text NOT NULL,
    link_user_id uuid REFERENCES public.users (id) ON DELETE CASCADE,
    link_session_hash bytea,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- migrate:down
DROP TABLE IF EXISTS public.oidc_state;
DROP INDEX IF EXISTS public.credential_via_link_idx;
DELETE FROM public.credential WHERE kind = 'oidc';
ALTER TABLE public.credential DROP COLUMN IF EXISTS via_link;
ALTER TABLE public.credential DROP COLUMN IF EXISTS recheck_grace_until;
ALTER TABLE public.credential DROP COLUMN IF EXISTS refresh_accepted_at;
ALTER TABLE public.credential DROP COLUMN IF EXISTS refresh_sealed;
ALTER TABLE public.credential DROP COLUMN IF EXISTS linked_by;
DROP INDEX IF EXISTS public.credential_kind_identifier_key;
ALTER TABLE public.credential DROP CONSTRAINT IF EXISTS credential_secret_shape_check;
ALTER TABLE public.credential ALTER COLUMN secret_hash SET NOT NULL;
ALTER TABLE public.credential DROP COLUMN IF EXISTS identifier;
ALTER TABLE public.credential DROP CONSTRAINT IF EXISTS credential_kind_check;
ALTER TABLE public.credential ADD CONSTRAINT credential_kind_check
    CHECK (kind = ANY (ARRAY['bearer'::text, 'password'::text]));
