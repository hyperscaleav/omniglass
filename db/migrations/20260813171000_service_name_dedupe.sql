-- migrate:up

-- One-time backfill ahead of the uniqueness floor
-- (20260813172000_service_name_unique.sql). service.name was never unique and
-- carried no index, so an existing estate may hold two service accounts under
-- one name; the floor cannot land on top of them.
--
-- The oldest row of each duplicated name KEEPS it, and every other row is
-- suffixed with its own principal_id. principal.id is uuidv7 (init.sql, the
-- Postgres 18 builtin), which sorts by creation time, so "oldest" is the
-- smallest id and the account an operator has been calling by that name for
-- longest is the one that still answers to it.
--
-- The suffix is the WHOLE principal id, not a prefix of it and not a counter.
-- A counter can collide with a name that already exists (`svc`, `svc`, `svc-2`
-- renames the second `svc` onto the third row), and a prefix can collide by
-- birthday; the full id is the primary key of the row being renamed, so the
-- result is unique by construction rather than by luck. It is ugly on purpose:
-- a renamed account should be obvious, and the id says exactly which row it is.
--
-- Idempotent: the second run finds no duplicates left to rename and changes
-- nothing.
UPDATE service s
   SET name = s.name || '-' || s.principal_id::text
 WHERE EXISTS (
         SELECT 1 FROM service other
          WHERE other.name = s.name
            AND other.principal_id < s.principal_id
       );

-- migrate:down

-- Not reversible. Which rows were renamed is not recoverable from the row shape
-- alone once the suffix is part of the name, and the floor's own down (applied
-- first going backwards) already drops the constraint that required this, so a
-- rolled-back database is consistent, just not restored to its duplicates.
