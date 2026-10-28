-- migrate:up
-- #417: the metric table's index for a sample a system, a location or a node
-- owns (a calc rule's output on a system or a location, today). The retention
-- prune keeps the latest row of every series, so for each old row it asks
-- whether a newer row of the same series exists. The only owner index on metric
-- leads with component_id (metric_owner_idx), so for any other owner that probe
-- was a scan of the whole table per row. This answers it with a probe, keyed on
-- the one arc column such a row fills (coalesce), the way property_system_owner_idx
-- and property_location_owner_idx serve the property lane.
-- TestPruneSamplesReachesAnIndexOnEveryArc explains the statements and fails if
-- one stops reaching an index.
--
-- Partial on component_id is null, so a component's sample (every node probe and
-- driver poll) pays nothing for it. Deliberately NOT concurrently, for the
-- reasons 20260813100000_property_system_owner_index.sql gives (dbmate's
-- transaction, and no INVALID index left behind on a failed build).
CREATE INDEX IF NOT EXISTS metric_arc_owner_idx
    ON public.metric USING btree (metric_type_id, (COALESCE(system_id, location_id, node_id)), instance, ts DESC)
    WHERE (component_id IS NULL);

-- migrate:down
DROP INDEX IF EXISTS public.metric_arc_owner_idx;
