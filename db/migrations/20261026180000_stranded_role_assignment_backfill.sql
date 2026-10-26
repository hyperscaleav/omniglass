-- migrate:up

-- One-time data backfill (#506, ADR-0506): before this release a standard
-- change repointed system.standard_id and left every assignment on the old
-- standard's roles. Those rows have been invisible since: the roles read and
-- health resolve a system's roles from its own declarations and its current
-- standard only, so no verdict and no visible staffing counts them. What they
-- still did was wedge their component (it could not be unassigned, unbound or
-- deleted). A standard change now carries or releases its staffing in its own
-- transaction; this releases the strands an earlier one left, the same release
-- the change makes for a role its new standard does not declare. The
-- membership is untouched, so each component stays in its system, free.
-- Idempotent: a second run finds nothing.
DELETE FROM public.system_role_assignment ra
USING public.system s, public.system_role r
WHERE s.id = ra.system_id
  AND r.id = ra.role_id
  AND NOT (r.owner_kind = 'system' AND r.system_id = s.id)
  AND NOT (r.owner_kind = 'standard' AND r.standard_id IS NOT DISTINCT FROM s.standard_id);

-- migrate:down

-- Nothing to restore: the released rows pointed at roles their systems no
-- longer have, so putting them back would only wedge their components again.
SELECT 1;
