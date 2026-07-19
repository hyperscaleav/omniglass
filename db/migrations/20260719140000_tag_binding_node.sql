-- Node tags (N2): make node a taggable owner kind, alongside global / component
-- / system / location. A node is estate-wide (not a scope tree), so its effective
-- tags are its direct bindings, no cascade. The owner arc gains a node_id leg;
-- the two CHECK constraints are dropped and re-added (Postgres cannot alter a
-- CHECK in place). node_id references node(principal_id), ON DELETE CASCADE so a
-- node purge drops its bindings.
-- migrate:up
alter table tag_binding add column if not exists node_id uuid references node (principal_id) on delete cascade;

alter table tag_binding drop constraint if exists tag_binding_owner_kind_check;
alter table tag_binding add constraint tag_binding_owner_kind_check
  check (owner_kind in ('global', 'component', 'system', 'location', 'node'));

alter table tag_binding drop constraint if exists tag_binding_owner_arc;
alter table tag_binding add constraint tag_binding_owner_arc check (
    (owner_kind = 'global'    and component_id is null     and system_id is null    and location_id is null and node_id is null) or
    (owner_kind = 'component' and component_id is not null and system_id is null    and location_id is null and node_id is null) or
    (owner_kind = 'system'    and system_id is not null    and component_id is null and location_id is null and node_id is null) or
    (owner_kind = 'location'  and location_id is not null  and component_id is null and system_id is null   and node_id is null) or
    (owner_kind = 'node'      and node_id is not null      and component_id is null and system_id is null   and location_id is null)
);

create unique index if not exists tag_binding_node_key on tag_binding (tag_id, node_id) where owner_kind = 'node';
create index if not exists tag_binding_node_idx on tag_binding (node_id);

-- migrate:down
drop index if exists tag_binding_node_idx;
drop index if exists tag_binding_node_key;

alter table tag_binding drop constraint if exists tag_binding_owner_arc;
alter table tag_binding add constraint tag_binding_owner_arc check (
    (owner_kind = 'global'    and component_id is null     and system_id is null    and location_id is null) or
    (owner_kind = 'component' and component_id is not null and system_id is null    and location_id is null) or
    (owner_kind = 'system'    and system_id is not null    and component_id is null and location_id is null) or
    (owner_kind = 'location'  and location_id is not null  and component_id is null and system_id is null)
);

alter table tag_binding drop constraint if exists tag_binding_owner_kind_check;
alter table tag_binding add constraint tag_binding_owner_kind_check
  check (owner_kind in ('global', 'component', 'system', 'location'));

alter table tag_binding drop column if exists node_id;
