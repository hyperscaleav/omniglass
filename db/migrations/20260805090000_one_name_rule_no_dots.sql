-- migrate:up

-- One name rule, no dots (#586). A name is a single kebab token on every table, so
-- every name that carried a dot becomes its hyphenated form: icmp.rtt-avg is now
-- icmp-rtt-avg. This is a one-time backfill of operator data, not DDL, so it belongs
-- in the backfill bucket rather than beside a schema change.
--
-- It converts EVERY dotted name in the three keyspace-shaped catalogs, not just the
-- eight the seed ships, because an operator may have created their own. The seed's
-- own rows are also rewritten here rather than left to the boot upsert: the upsert
-- keys on name, so a renamed seed row would insert a second row beside the old one
-- and orphan every sample pointing at the original.
--
-- Idempotent by construction: replace() on a name with no dot is a no-op, and the
-- guard below re-runs clean.

do $$
declare
  collided text;
begin
  -- A collision is possible in principle (a.b and a-b both existing), and silently
  -- picking a winner would lose data. Refuse instead, and name the pair.
  for collided in
    select t.name from (
      select name from property_type where name like '%.%'
      union all select name from event_type where name like '%.%'
      union all select name from command_type where name like '%.%'
    ) t
    where exists (
      select 1 from property_type p where p.name = replace(t.name, '.', '-')
      union all select 1 from event_type e where e.name = replace(t.name, '.', '-')
      union all select 1 from command_type c where c.name = replace(t.name, '.', '-')
    )
  loop
    raise exception 'name collision converting % to %: both would exist. Rename one by hand first.',
      collided, replace(collided, '.', '-');
  end loop;
end $$;

update property_type set name = replace(name, '.', '-') where name like '%.%';
update event_type    set name = replace(name, '.', '-') where name like '%.%';
update command_type  set name = replace(name, '.', '-') where name like '%.%';

-- The ceiling drops from 128 to 100 with the second rule's retirement. Converting a
-- dot to a hyphen does not change a name's length, so nothing can newly exceed it,
-- but a name that was already over 100 under the old keyspace ceiling would now be
-- illegal. Report rather than truncate.
do $$
declare
  toolong text;
begin
  select name into toolong from (
    select name from property_type
    union all select name from event_type
    union all select name from command_type
  ) t where length(name) > 100 limit 1;
  if toolong is not null then
    raise exception 'name % exceeds the 100 character ceiling that now applies to every table', toolong;
  end if;
end $$;

-- migrate:down

-- No down. The dotted form is retired vocabulary, and reversing would have to guess
-- which hyphens were once dots.
select 1;
