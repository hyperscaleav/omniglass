-- migrate:up

-- One-time backfill ahead of the position floor
-- (20260807146000_assignment_position_floor.sql): every existing assignment
-- gets a position, numbered per (system_id, role_id) in the order it was
-- created. created_at ties within one transaction (it defaults to now(),
-- which is transaction-start time), so id is the tie-break that keeps true
-- creation order: it is uuidv7() (init.sql:611, the Postgres 18 builtin),
-- which sorts by creation time. Idempotent: the "where position is null"
-- guard makes a re-run a no-op (internal/storage/role_capacity_migration_test.go's
-- TestPositionBackfillIdempotent runs this exact statement twice against
-- hand-inserted rows and asserts the second run changes nothing).
with numbered as (
  select id, row_number() over (partition by system_id, role_id order by created_at, id) as n
  from system_role_assignment
  where position is null
)
update system_role_assignment sra
   set position = numbered.n
  from numbered
 where sra.id = numbered.id;

-- migrate:down

-- Not meaningfully reversible: the floor migration's own down (applied
-- before this one going forward) already restores nullability, and which
-- rows were originally NULL before this backfill ran is not recoverable
-- from the row shape alone, so this down leaves the backfilled positions in
-- place rather than guess at undoing them.
