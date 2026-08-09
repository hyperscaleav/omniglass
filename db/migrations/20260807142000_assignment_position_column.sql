-- migrate:up

-- #626 (task 6): an assignment carries its ordering position within its
-- role, a 1-based slot number unique per (system, role) that position_labels
-- (system_role, 20260807140000_role_capacity_and_positions.sql) can name and
-- :swapPositions can exchange. Nullable here: the backfill
-- (20260807143000_assignment_position_backfill.sql) fills every existing row
-- before the floor (20260807146000_assignment_position_floor.sql) makes it
-- required and adds its invariants.
alter table system_role_assignment add column if not exists position integer;

-- migrate:down

alter table system_role_assignment drop column if exists position;
