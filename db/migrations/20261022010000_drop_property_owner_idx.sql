-- migrate:up
-- #971: property_owner_idx (state_owner_idx before the #588 rename) and
-- property_series_idx (#944) have identical definitions: btree on
-- (component_id, property_type_id, instance, ts DESC), partial on
-- component_id IS NOT NULL. Every property insert maintained the same index
-- twice. property_series_idx is the name the series reads and their EXPLAIN
-- tests (TestSeriesNewestIsAnIndexProbe) reference, so it stays.
-- TestNoDuplicateIndexes refuses a twin on any table from here on.
DROP INDEX IF EXISTS public.property_owner_idx;

-- migrate:down
CREATE INDEX IF NOT EXISTS property_owner_idx
    ON public.property USING btree (component_id, property_type_id, instance, ts DESC)
    WHERE (component_id IS NOT NULL);
