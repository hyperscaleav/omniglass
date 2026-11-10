-- migrate:up

-- A node's command pull (#1631) reads the deliverable commands of its own
-- components: unexecuted component commands inside the delivery window. The
-- older command indexes are keyed for settlement (status = 'issued') or cover
-- every row ever written (command_component_ts_idx), so this partial index
-- holds only the rows a pull can deliver, searched by component. now() is
-- not immutable and cannot appear in an index predicate, so the delivery
-- window stays a range on the ts key column.
CREATE INDEX IF NOT EXISTS command_deliverable_idx ON public.command (component_id, ts)
    WHERE owner_kind = 'component' AND executed_at IS NULL;

-- migrate:down

DROP INDEX IF EXISTS public.command_deliverable_idx;
