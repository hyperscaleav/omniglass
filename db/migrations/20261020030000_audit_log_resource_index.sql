-- migrate:up

-- An alarm's timeline reads the audit rows of one resource (#950): the
-- alarm's raise, acknowledgement and clear, keyed by resource and id. Until
-- now the audit log was only read newest first, across every resource.
CREATE INDEX IF NOT EXISTS audit_log_resource_idx ON public.audit_log (resource, resource_id);

-- migrate:down

DROP INDEX IF EXISTS public.audit_log_resource_idx;
