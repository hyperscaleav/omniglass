-- migrate:up

-- A node's command pull reads only the commands still issued (#1630): a
-- terminal one, timed out or failed by its settlement, is never handed to a
-- node. command_deliverable_idx (#1631) narrows to the same term, so it holds
-- only the rows a pull can deliver, not every unexecuted terminal command
-- ever issued. The rebuild is a drop and a create, idempotent either way.
DROP INDEX IF EXISTS public.command_deliverable_idx;
CREATE INDEX command_deliverable_idx ON public.command (component_id, ts)
    WHERE owner_kind = 'component' AND executed_at IS NULL AND status = 'issued';

-- migrate:down

DROP INDEX IF EXISTS public.command_deliverable_idx;
CREATE INDEX command_deliverable_idx ON public.command (component_id, ts)
    WHERE owner_kind = 'component' AND executed_at IS NULL;
