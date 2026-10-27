-- migrate:up
-- The lab mark (#1195): the one row `omniglass lab` writes into the database it
-- creates, so a lab and a real deployment refuse each other's database. The
-- table is schema; its row is data the lab writes, never a migration or a
-- boot-seed row, so a real database keeps the table empty for life. One row at
-- most: the key is a constant true.
create table if not exists lab_mark (
  singleton boolean primary key default true check (singleton),
  marked_at timestamptz not null default now()
);

-- migrate:down
drop table if exists lab_mark;
