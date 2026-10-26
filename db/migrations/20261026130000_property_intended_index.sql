-- migrate:up
-- #456: the intended rows' own index. An intended row is a command's (its
-- command_id lineage), so a series holds few of them beside every observation
-- of its key, and property_series_idx (component, type, instance, ts desc)
-- carries no provenance: a read of the newest intended value on a series
-- walked that series' observed rows and filtered them. Reconciliation's told
-- (latestIntendedSQL), the settlement verdict's intended read and a command's
-- judged series (intendedSeries) each make that read; this partial index
-- answers it with a probe, and costs an insert only an intended row pays.
-- TestToldReadsOnlyTheInstancesCommandsNamed explains the told read and fails
-- if it filters an observed row.
--
-- Deliberately NOT concurrently, for the reasons
-- 20260813100000_property_system_owner_index.sql gives (dbmate's
-- transaction, and no INVALID index left behind on a failed build).
CREATE INDEX IF NOT EXISTS property_intended_idx
    ON public.property USING btree (component_id, property_type_id, instance, ts DESC)
    WHERE (component_id IS NOT NULL AND provenance = 'intended');

-- migrate:down
DROP INDEX IF EXISTS public.property_intended_idx;
