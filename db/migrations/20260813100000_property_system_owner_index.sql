-- migrate:up
-- #725: the system arc's owner index on `property`.
--
-- Both health reads (SystemVerdicts, the bulk read behind GET /systems:health,
-- and locationVerdict, the rollup a recompute pays per location) filter the
-- series by property type and a set of system ids, then take the newest row per
-- system. The only owner index on this table led with `component_id` and is
-- partial on it, so neither read could reach an index and both scanned the whole
-- series: measured at 1,521,600 property rows (334 MB), the bulk read over 1,500
-- systems took 51 ms and the rollup over one room took 45 ms, both of them
-- parallel sequential scans.
--
-- Column order is the reads' own: `property_type_id` leads because both filter
-- it by equality, which leaves the rest of the range ordered by
-- (system_id, id desc), exactly the ORDER BY, so the scan needs no sort. The
-- reverse order (system_id first) was measured too and is slower once a system
-- carries properties other than health (11.3 ms against 15.0 ms), because it
-- reads every system-owned row rather than only the health ones.
--
-- Partial on `system_id is not null` so the telemetry lane pays nothing: a
-- component-owned insert never touches this index. Measured, 150,000
-- component-owned inserts cost 2237 ms with the index and 2237 ms without it,
-- while system-owned inserts (a health transition, and only on a transition)
-- went from 327 ms to 408 ms per 30,000. Postgres proves the predicate from the
-- reads' own `system_id in (...)`, which is asserted rather than assumed:
-- TestHealthReadsReachTheSystemOwnerIndex explains the statements the gateway
-- really issues and fails if either stops reaching this index.
--
-- Deliberately NOT `create index concurrently`. A concurrent build would keep
-- the telemetry lane writing during the build, but it cannot run in dbmate's
-- transaction (so the migration loses its atomicity), and a failed concurrent
-- build leaves an INVALID index behind that the retry's `if not exists` then
-- silently skips, which is the same class of silently-unusable index this slice
-- exists to close. The build takes a SHARE lock that blocks inserts for its
-- duration, measured at 90 ms over 18,000 indexed rows and 282 ms over 78,000,
-- both on a 1.5M-row table. Revisit when `property` is large enough for that to
-- be seconds rather than milliseconds; the trade flips then, and it is a new
-- migration when it does.
CREATE INDEX IF NOT EXISTS property_system_owner_idx
    ON public.property USING btree (property_type_id, system_id, id DESC)
    WHERE (system_id IS NOT NULL);

-- migrate:down
DROP INDEX IF EXISTS property_system_owner_idx;
