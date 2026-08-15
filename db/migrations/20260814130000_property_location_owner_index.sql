-- migrate:up
-- #632: the location arc's owner index on `property`.
--
-- The fleet projection reads the CURRENT health verdict for every location and
-- every system in one pass, as a correlated subquery per row (the same
-- latest-by-id read subtreeSystemHealth already makes, hoisted fleet-wide).
-- The system arc got its index in #725 (`property_system_owner_idx`), with the
-- column order measured there: `property_type_id` leads because the read
-- filters it by equality, which leaves the rest of the range ordered by
-- (location_id, id desc), exactly the ORDER BY, so the scan needs no sort.
-- Nothing indexed `location_id` at all, so the per-location verdict subquery
-- planned as a sequential scan of the telemetry lane, the largest table in the
-- system: an fleet of a few hundred locations scanned it a few hundred times
-- per canvas paint. That is precisely the cost the projection exists to avoid,
-- so the projection was reintroducing it.
--
-- Partial on `location_id is not null` so the telemetry lane pays nothing: a
-- component-owned insert never touches this index, and a location-owned write
-- happens only on a health transition. This mirrors #725's system-arc index
-- exactly; the same access-path guard asserts the planner reaches both
-- (TestFleetProjectionVerdictLookupsAreIndexed), verified failing first.
--
-- Deliberately NOT `create index concurrently`, for #725's reasons: a
-- concurrent build cannot run in dbmate's transaction, and a failed one leaves
-- an INVALID index that a retry's `if not exists` silently skips. The SHARE
-- lock it takes instead blocks inserts for milliseconds at current sizes.
CREATE INDEX IF NOT EXISTS property_location_owner_idx
    ON public.property USING btree (property_type_id, location_id, id DESC)
    WHERE (location_id IS NOT NULL);

-- migrate:down
DROP INDEX IF EXISTS property_location_owner_idx;
