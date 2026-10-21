-- migrate:up

-- A calculated sample names its calc rule by id as well as by name (#999):
-- source_rule keeps the name it was written under (history, as an event's
-- source_rule does), source_rule_id the rule itself, so a value shown on a
-- component or a system links to the rule that derived it across a rename.
-- No foreign key: a deleted rule's samples keep its id as history. Null for
-- every other row (observed samples, and the health rollup, which is no calc
-- rule). Nullable with no default, so the add rewrites nothing.
ALTER TABLE public.metric ADD COLUMN IF NOT EXISTS source_rule_id uuid;
ALTER TABLE public.property ADD COLUMN IF NOT EXISTS source_rule_id uuid;

-- migrate:down

ALTER TABLE public.property DROP COLUMN IF EXISTS source_rule_id;
ALTER TABLE public.metric DROP COLUMN IF EXISTS source_rule_id;
