-- migrate:up

-- A fanned sample names its conduit by id as well as by name (#1282,
-- ADR-1282): source keeps "<conduit>/<endpoint>" as it was written (history,
-- as source_rule does), source_conduit_id the conduit component itself, so the
-- one link from a fanned value back to the component that reported it
-- survives a rename, and a read renders the conduit's current name. No
-- foreign key: a deleted conduit's samples keep its id as history, as a
-- deleted calc rule's keep source_rule_id. Null on every other row. Nullable
-- with no default, so the add rewrites nothing.
ALTER TABLE public.metric ADD COLUMN IF NOT EXISTS source_conduit_id uuid;
ALTER TABLE public.property ADD COLUMN IF NOT EXISTS source_conduit_id uuid;
ALTER TABLE public.event ADD COLUMN IF NOT EXISTS source_conduit_id uuid;

-- migrate:down

ALTER TABLE public.event DROP COLUMN IF EXISTS source_conduit_id;
ALTER TABLE public.property DROP COLUMN IF EXISTS source_conduit_id;
ALTER TABLE public.metric DROP COLUMN IF EXISTS source_conduit_id;
