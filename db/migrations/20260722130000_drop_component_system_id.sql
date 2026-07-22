-- migrate:up

-- The component's system pointer is retired. system_member now carries the
-- relation, the four cascade resolvers seed their system band from it, and
-- nothing reads this column any more. Membership was backfilled from it in
-- 20260722120100 before this drop, so the information it held survives.
--
-- It was a single pointer, which could never say that a shared device belongs to
-- more than one system, and it was write-once with no re-home path. Both of those
-- stop being true with the relation in its own table.
alter table component drop column if exists system_id;

-- migrate:down

alter table component add column if not exists system_id uuid references system (id) on delete restrict;
create index if not exists component_system_idx on component (system_id);
