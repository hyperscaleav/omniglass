-- migrate:up

-- Since when a system calc rule's system has had no role-bound members
-- (#997 review): the rule writes nothing then, so the system keeps its last
-- calculated value, and the state names it unsupported. Null while it has
-- members, and for a component rule.
ALTER TABLE public.calc_rule_state ADD COLUMN IF NOT EXISTS no_members_since timestamp with time zone;

-- migrate:down

ALTER TABLE public.calc_rule_state DROP COLUMN IF EXISTS no_members_since;
