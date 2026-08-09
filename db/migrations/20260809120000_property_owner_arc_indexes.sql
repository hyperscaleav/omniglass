-- migrate:up

-- #632: the estate projection reads the CURRENT health verdict for every location
-- and every system in one pass, as a correlated subquery per row (the same
-- latest-by-id read subtreeSystemHealth already makes, hoisted estate-wide).
--
-- property carried exactly one owner-arc index before this: property_component_idx
-- (component_id, property_type_id), added by 20260805150000 when the value store
-- retired. Nothing indexed system_id or location_id, so those two subqueries
-- planned as sequential scans of the telemetry lane, which is the largest table in
-- the system: an estate of a few hundred locations and systems scanned it a few
-- hundred times per canvas paint. That is precisely the cost the projection exists
-- to avoid, so the projection was reintroducing it.
--
-- The column order mirrors property_component_idx deliberately: owner first, then
-- property_type_id, which is the shape both the health reads and the projection
-- filter by. The trailing id ordering the subquery needs is satisfied from the
-- heap for the handful of rows one owner's health series holds, health being
-- recorded transition-only (edges, never samples), so a third index column would
-- pay for itself only on an owner that broke thousands of times.
--
-- Partial on NOT NULL, matching the owner-arc CHECK: a property row carries
-- exactly one of the four owner columns, so most rows are irrelevant to each of
-- these and a partial index keeps them out of it.

create index if not exists property_system_idx on property using btree (system_id, property_type_id)
	where system_id is not null;

create index if not exists property_location_idx on property using btree (location_id, property_type_id)
	where location_id is not null;

-- migrate:down

drop index if exists property_system_idx;
drop index if exists property_location_idx;
