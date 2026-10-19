-- migrate:up
-- #928: the property lane's per-series index, the twin of metric_owner_idx.
--
-- The event rule paths read one series' newest sample (the reconcile pass,
-- the sweep's re-judge, a rule write's prime of one series, an arriving
-- sample's newest check): component, type and instance by equality, then
-- ts desc. The only component index on this table was
-- property_component_idx (component_id, property_type_id), so each lookup
-- read the series' whole history and sorted it. This index answers it with
-- a probe, exactly as metric_owner_idx (component_id, metric_type_id,
-- instance, ts desc) does on the metric lane; TestSeriesNewestIsAnIndexProbe
-- explains the statement the gateway runs and fails if either lane stops
-- reaching its index.
--
-- property_component_idx is a prefix of it, so it is dropped rather than
-- kept: every read it served is served by this one, and the telemetry lane
-- pays for one component index per insert, not two. Partial on
-- component_id like both of its predecessors, so a system, location or node
-- owned sample never touches it.
--
-- Deliberately NOT concurrently, for the reasons
-- 20260813100000_property_system_owner_index.sql gives (dbmate's
-- transaction, and no INVALID index left behind on a failed build).
CREATE INDEX IF NOT EXISTS property_series_idx
    ON public.property USING btree (component_id, property_type_id, instance, ts DESC)
    WHERE (component_id IS NOT NULL);
DROP INDEX IF EXISTS public.property_component_idx;

-- migrate:down
CREATE INDEX IF NOT EXISTS property_component_idx
    ON public.property USING btree (component_id, property_type_id)
    WHERE (component_id IS NOT NULL);
DROP INDEX IF EXISTS public.property_series_idx;
