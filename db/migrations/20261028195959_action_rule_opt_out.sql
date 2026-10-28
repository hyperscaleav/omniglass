-- migrate:up

-- Group opt-out (#1341, #1354): an admin may mark a platform rule
-- opt_out_allowed (default false, so an on-call page cannot be opted out of
-- unless the admin says so), and a member of a group the rule targets may
-- then opt out of it for themselves, and back in. An opt-out is one row per
-- person per rule, gone with either.
ALTER TABLE public.action_rule ADD COLUMN IF NOT EXISTS opt_out_allowed boolean NOT NULL DEFAULT false;

CREATE TABLE IF NOT EXISTS public.action_rule_opt_out (
    rule_id uuid NOT NULL REFERENCES public.action_rule(id) ON DELETE CASCADE,
    principal_id uuid NOT NULL REFERENCES public.principal(id) ON DELETE CASCADE,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    PRIMARY KEY (rule_id, principal_id)
);
CREATE INDEX IF NOT EXISTS action_rule_opt_out_principal_idx ON public.action_rule_opt_out (principal_id);

-- migrate:down

DROP TABLE IF EXISTS public.action_rule_opt_out;
ALTER TABLE public.action_rule DROP COLUMN IF EXISTS opt_out_allowed;
