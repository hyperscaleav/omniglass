-- migrate:up

-- A grant can exclude its own scope root from the modify actions: the holder can
-- create under, and update/delete within, the subtree, but cannot update or
-- delete the root entity itself (a deploy/integrator grant that must not modify
-- the boundary of its own scope). Read and create-placement still include the
-- root. The default false preserves every existing grant's inclusive behavior.
alter table principal_grant add column if not exists exclude_root boolean not null default false;

-- migrate:down
alter table principal_grant drop column if exists exclude_root;
