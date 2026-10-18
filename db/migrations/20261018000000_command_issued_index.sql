-- migrate:up

-- Settlement runs while a command is issued: on every observation of a
-- property a command type targets (by component and type) and on the
-- server's sweep (by type and age). Partial indexes keep both to the
-- handful of issued rows rather than the whole command log (ADR-0141).
CREATE INDEX IF NOT EXISTS command_issued_component_idx ON public.command (component_id, command_type_id) WHERE status = 'issued';
CREATE INDEX IF NOT EXISTS command_issued_age_idx ON public.command (command_type_id, ts) WHERE status = 'issued';

-- migrate:down

DROP INDEX IF EXISTS public.command_issued_age_idx;
DROP INDEX IF EXISTS public.command_issued_component_idx;
