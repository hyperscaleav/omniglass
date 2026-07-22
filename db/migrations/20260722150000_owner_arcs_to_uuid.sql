-- migrate:up

-- Return the owner arcs to the uuid primary key. 20260722140000 moved them to
-- name, which was the wrong direction: a foreign key onto a mutable column buys
-- nothing and costs a rewrite of every referencing row whenever an operator
-- renames something. The uuid is immutable, so a rename becomes a single-row
-- update and `on update cascade` is not needed anywhere.
--
-- The name remains a unique, friendly alias. It is simply not what anything
-- points at. The API accepts either form and resolves it at the edge.

-- tag_binding: three arcs, name back to uuid.
alter table tag_binding drop constraint if exists tag_binding_owner_arc;
alter table tag_binding add column if not exists component_uuid uuid;
alter table tag_binding add column if not exists system_uuid uuid;
alter table tag_binding add column if not exists location_uuid uuid;
update tag_binding b set component_uuid = e.id from component e where e.name = b.component_id;
update tag_binding b set system_uuid = e.id from system e where e.name = b.system_id;
update tag_binding b set location_uuid = e.id from location e where e.name = b.location_id;
alter table tag_binding drop column component_id, drop column system_id, drop column location_id;
alter table tag_binding rename column component_uuid to component_id;
alter table tag_binding rename column system_uuid to system_id;
alter table tag_binding rename column location_uuid to location_id;
alter table tag_binding add constraint tag_binding_component_id_fkey foreign key (component_id) references component (id) on delete cascade;
create index if not exists tag_binding_component_idx on tag_binding (component_id);
alter table tag_binding add constraint tag_binding_system_id_fkey foreign key (system_id) references system (id) on delete cascade;
create index if not exists tag_binding_system_idx on tag_binding (system_id);
alter table tag_binding add constraint tag_binding_location_id_fkey foreign key (location_id) references location (id) on delete cascade;
create index if not exists tag_binding_location_idx on tag_binding (location_id);
create unique index if not exists tag_binding_component_key_uuid on tag_binding (tag_id, component_id) where owner_kind = 'component';
create unique index if not exists tag_binding_system_key_uuid on tag_binding (tag_id, system_id) where owner_kind = 'system';
create unique index if not exists tag_binding_location_key_uuid on tag_binding (tag_id, location_id) where owner_kind = 'location';
alter table tag_binding add constraint tag_binding_owner_arc check (
    (owner_kind = 'global' and component_id is null and system_id is null and location_id is null and node_id is null)
    or (owner_kind = 'component' and component_id is not null and system_id is null and location_id is null and node_id is null)
    or (owner_kind = 'system' and system_id is not null and component_id is null and location_id is null and node_id is null)
    or (owner_kind = 'location' and location_id is not null and component_id is null and system_id is null and node_id is null)
    or (owner_kind = 'node' and node_id is not null and component_id is null and system_id is null and location_id is null)
);

-- variable: three arcs, name back to uuid.
alter table variable drop constraint if exists variable_owner_arc;
alter table variable add column if not exists component_uuid uuid;
alter table variable add column if not exists system_uuid uuid;
alter table variable add column if not exists location_uuid uuid;
update variable b set component_uuid = e.id from component e where e.name = b.component_id;
update variable b set system_uuid = e.id from system e where e.name = b.system_id;
update variable b set location_uuid = e.id from location e where e.name = b.location_id;
alter table variable drop column component_id, drop column system_id, drop column location_id;
alter table variable rename column component_uuid to component_id;
alter table variable rename column system_uuid to system_id;
alter table variable rename column location_uuid to location_id;
alter table variable add constraint variable_component_id_fkey foreign key (component_id) references component (id) on delete cascade;
create index if not exists variable_component_idx on variable (component_id);
alter table variable add constraint variable_system_id_fkey foreign key (system_id) references system (id) on delete cascade;
create index if not exists variable_system_idx on variable (system_id);
alter table variable add constraint variable_location_id_fkey foreign key (location_id) references location (id) on delete cascade;
create index if not exists variable_location_idx on variable (location_id);
create unique index if not exists variable_component_key_uuid on variable (name, component_id) where owner_kind = 'component';
create unique index if not exists variable_system_key_uuid on variable (name, system_id) where owner_kind = 'system';
create unique index if not exists variable_location_key_uuid on variable (name, location_id) where owner_kind = 'location';
alter table variable add constraint variable_owner_arc check (
    (owner_kind = 'global' and component_id is null and system_id is null and location_id is null)
    or (owner_kind = 'component' and component_id is not null and system_id is null and location_id is null)
    or (owner_kind = 'system' and system_id is not null and component_id is null and location_id is null)
    or (owner_kind = 'location' and location_id is not null and component_id is null and system_id is null)
);

-- secret: three arcs, name back to uuid.
alter table secret drop constraint if exists secret_owner_arc;
alter table secret add column if not exists component_uuid uuid;
alter table secret add column if not exists system_uuid uuid;
alter table secret add column if not exists location_uuid uuid;
update secret b set component_uuid = e.id from component e where e.name = b.component_id;
update secret b set system_uuid = e.id from system e where e.name = b.system_id;
update secret b set location_uuid = e.id from location e where e.name = b.location_id;
alter table secret drop column component_id, drop column system_id, drop column location_id;
alter table secret rename column component_uuid to component_id;
alter table secret rename column system_uuid to system_id;
alter table secret rename column location_uuid to location_id;
alter table secret add constraint secret_component_id_fkey foreign key (component_id) references component (id) on delete cascade;
create index if not exists secret_component_idx on secret (component_id);
alter table secret add constraint secret_system_id_fkey foreign key (system_id) references system (id) on delete cascade;
create index if not exists secret_system_idx on secret (system_id);
alter table secret add constraint secret_location_id_fkey foreign key (location_id) references location (id) on delete cascade;
create index if not exists secret_location_idx on secret (location_id);
create unique index if not exists secret_component_key_uuid on secret (name, component_id) where owner_kind = 'component';
create unique index if not exists secret_system_key_uuid on secret (name, system_id) where owner_kind = 'system';
create unique index if not exists secret_location_key_uuid on secret (name, location_id) where owner_kind = 'location';
alter table secret add constraint secret_owner_arc check (
    (owner_kind = 'global' and component_id is null and system_id is null and location_id is null)
    or (owner_kind = 'component' and component_id is not null and system_id is null and location_id is null)
    or (owner_kind = 'system' and system_id is not null and component_id is null and location_id is null)
    or (owner_kind = 'location' and location_id is not null and component_id is null and system_id is null)
);

-- migrate:down

-- One way, the same as the migration it corrects: reversing would resolve uuids
-- back to names this migration no longer records, and a rename since would make
-- that resolution wrong rather than merely absent.
select 1;
