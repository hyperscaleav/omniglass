-- migrate:up

-- The owner arcs on tag_binding, variable, and secret keyed their owner by uuid
-- while every table from the collection era onward keys by name. The two
-- conventions met inside single queries: the cascade resolvers had to walk chains
-- of uuids purely to match these three tables, and a component's name had to be
-- carried alongside its id to bridge them.
--
-- on update cascade is what makes a name safe as a key. Without it a rename
-- orphans every binding, which is precisely why the newer tables carry it.
--
-- The columns keep their _id suffix, matching role_assignment.component_id and
-- state_datapoint.component_id, which are likewise text referencing name.

-- tag_binding: three arcs, uuid to name.
alter table tag_binding drop constraint if exists tag_binding_owner_arc;
alter table tag_binding add column if not exists component_name text;
alter table tag_binding add column if not exists system_name text;
alter table tag_binding add column if not exists location_name text;
update tag_binding b set component_name = e.name from component e where e.id = b.component_id;
update tag_binding b set system_name = e.name from system e where e.id = b.system_id;
update tag_binding b set location_name = e.name from location e where e.id = b.location_id;
alter table tag_binding drop column component_id, drop column system_id, drop column location_id;
alter table tag_binding rename column component_name to component_id;
alter table tag_binding rename column system_name to system_id;
alter table tag_binding rename column location_name to location_id;
alter table tag_binding add constraint tag_binding_component_id_fkey foreign key (component_id) references component (name) on update cascade on delete cascade;
create index if not exists tag_binding_component_idx on tag_binding (component_id);
alter table tag_binding add constraint tag_binding_system_id_fkey foreign key (system_id) references system (name) on update cascade on delete cascade;
create index if not exists tag_binding_system_idx on tag_binding (system_id);
alter table tag_binding add constraint tag_binding_location_id_fkey foreign key (location_id) references location (name) on update cascade on delete cascade;
create index if not exists tag_binding_location_idx on tag_binding (location_id);
create unique index if not exists tag_binding_component_key_name on tag_binding (tag_id, component_id) where owner_kind = 'component';
create unique index if not exists tag_binding_system_key_name on tag_binding (tag_id, system_id) where owner_kind = 'system';
create unique index if not exists tag_binding_location_key_name on tag_binding (tag_id, location_id) where owner_kind = 'location';
alter table tag_binding add constraint tag_binding_owner_arc check (
    (owner_kind = 'global' and component_id is null and system_id is null and location_id is null and node_id is null)
    or (owner_kind = 'component' and component_id is not null and system_id is null and location_id is null and node_id is null)
    or (owner_kind = 'system' and system_id is not null and component_id is null and location_id is null and node_id is null)
    or (owner_kind = 'location' and location_id is not null and component_id is null and system_id is null and node_id is null)
    or (owner_kind = 'node' and node_id is not null and component_id is null and system_id is null and location_id is null)
);

-- variable: three arcs, uuid to name.
alter table variable drop constraint if exists variable_owner_arc;
alter table variable add column if not exists component_name text;
alter table variable add column if not exists system_name text;
alter table variable add column if not exists location_name text;
update variable b set component_name = e.name from component e where e.id = b.component_id;
update variable b set system_name = e.name from system e where e.id = b.system_id;
update variable b set location_name = e.name from location e where e.id = b.location_id;
alter table variable drop column component_id, drop column system_id, drop column location_id;
alter table variable rename column component_name to component_id;
alter table variable rename column system_name to system_id;
alter table variable rename column location_name to location_id;
alter table variable add constraint variable_component_id_fkey foreign key (component_id) references component (name) on update cascade on delete cascade;
create index if not exists variable_component_idx on variable (component_id);
alter table variable add constraint variable_system_id_fkey foreign key (system_id) references system (name) on update cascade on delete cascade;
create index if not exists variable_system_idx on variable (system_id);
alter table variable add constraint variable_location_id_fkey foreign key (location_id) references location (name) on update cascade on delete cascade;
create index if not exists variable_location_idx on variable (location_id);
create unique index if not exists variable_component_key_name on variable (name, component_id) where owner_kind = 'component';
create unique index if not exists variable_system_key_name on variable (name, system_id) where owner_kind = 'system';
create unique index if not exists variable_location_key_name on variable (name, location_id) where owner_kind = 'location';
alter table variable add constraint variable_owner_arc check (
    (owner_kind = 'global' and component_id is null and system_id is null and location_id is null)
    or (owner_kind = 'component' and component_id is not null and system_id is null and location_id is null)
    or (owner_kind = 'system' and system_id is not null and component_id is null and location_id is null)
    or (owner_kind = 'location' and location_id is not null and component_id is null and system_id is null)
);

-- secret: three arcs, uuid to name.
alter table secret drop constraint if exists secret_owner_arc;
alter table secret add column if not exists component_name text;
alter table secret add column if not exists system_name text;
alter table secret add column if not exists location_name text;
update secret b set component_name = e.name from component e where e.id = b.component_id;
update secret b set system_name = e.name from system e where e.id = b.system_id;
update secret b set location_name = e.name from location e where e.id = b.location_id;
alter table secret drop column component_id, drop column system_id, drop column location_id;
alter table secret rename column component_name to component_id;
alter table secret rename column system_name to system_id;
alter table secret rename column location_name to location_id;
alter table secret add constraint secret_component_id_fkey foreign key (component_id) references component (name) on update cascade on delete cascade;
create index if not exists secret_component_idx on secret (component_id);
alter table secret add constraint secret_system_id_fkey foreign key (system_id) references system (name) on update cascade on delete cascade;
create index if not exists secret_system_idx on secret (system_id);
alter table secret add constraint secret_location_id_fkey foreign key (location_id) references location (name) on update cascade on delete cascade;
create index if not exists secret_location_idx on secret (location_id);
create unique index if not exists secret_component_key_name on secret (name, component_id) where owner_kind = 'component';
create unique index if not exists secret_system_key_name on secret (name, system_id) where owner_kind = 'system';
create unique index if not exists secret_location_key_name on secret (name, location_id) where owner_kind = 'location';
alter table secret add constraint secret_owner_arc check (
    (owner_kind = 'global' and component_id is null and system_id is null and location_id is null)
    or (owner_kind = 'component' and component_id is not null and system_id is null and location_id is null)
    or (owner_kind = 'system' and system_id is not null and component_id is null and location_id is null)
    or (owner_kind = 'location' and location_id is not null and component_id is null and system_id is null)
);

-- migrate:down

-- One way. The reverse would have to resolve names back to uuids that the
-- forward migration no longer records, and a rename since would make that
-- resolution wrong rather than merely absent.
select 1;
