-- migrate:up

-- The last conversion. After this every foreign key into component, system,
-- location, and node references a primary key, and the only *_id columns holding
-- a name are the slug-keyed catalogs where the name IS the primary key
-- (property, interface_type, product, standard, capability).
--
-- Two groups, together because metric_datapoint and interface each carry an
-- estate arc AND a node arc, so converting them separately would mean two passes
-- over the same statements.
--
-- node's primary key is principal_id, its enrollment identity. Five foreign keys
-- pointed at node.name, a unique alias rather than the key. That sat behind a
-- documented exception claiming a node had no id to point at, which was wrong.

-- metric_datapoint: three estate arcs plus the node arc ------------------------
alter table metric_datapoint drop constraint if exists metric_datapoint_owner_arc_check;
alter table metric_datapoint
    add column if not exists component_uuid uuid,
    add column if not exists system_uuid    uuid,
    add column if not exists location_uuid  uuid,
    add column if not exists node_uuid      uuid;
update metric_datapoint d set component_uuid = c.id from component c where c.name = d.component_id;
update metric_datapoint d set system_uuid    = s.id from system    s where s.name = d.system_id;
update metric_datapoint d set location_uuid  = l.id from location  l where l.name = d.location_id;
update metric_datapoint d set node_uuid      = n.principal_id from node n where n.name = d.node_id;
alter table metric_datapoint drop column component_id, drop column system_id, drop column location_id, drop column node_id;
alter table metric_datapoint rename column component_uuid to component_id;
alter table metric_datapoint rename column system_uuid    to system_id;
alter table metric_datapoint rename column location_uuid  to location_id;
alter table metric_datapoint rename column node_uuid      to node_id;
alter table metric_datapoint
    add constraint metric_datapoint_component_id_fkey foreign key (component_id) references component (id) on delete cascade,
    add constraint metric_datapoint_system_id_fkey    foreign key (system_id)    references system    (id) on delete cascade,
    add constraint metric_datapoint_location_id_fkey  foreign key (location_id)  references location  (id) on delete cascade,
    add constraint metric_datapoint_node_id_fkey      foreign key (node_id)      references node (principal_id) on delete cascade;
create index if not exists metric_datapoint_owner_idx on metric_datapoint (component_id, key, instance, ts desc) where component_id is not null;
alter table metric_datapoint add constraint metric_datapoint_owner_arc_check check (
       (owner_kind = 'component' and component_id is not null and system_id is null and location_id is null and node_id is null)
    or (owner_kind = 'system'    and system_id    is not null and component_id is null and location_id is null and node_id is null)
    or (owner_kind = 'location'  and location_id  is not null and component_id is null and system_id is null and node_id is null)
    or (owner_kind = 'node'      and node_id      is not null and component_id is null and system_id is null and location_id is null)
);

-- the node arcs on the tables converted in earlier slices ---------------------
alter table state_datapoint drop constraint if exists state_datapoint_owner_arc_check;
alter table state_datapoint add column if not exists node_uuid uuid;
update state_datapoint d set node_uuid = n.principal_id from node n where n.name = d.node_id;
alter table state_datapoint drop column node_id;
alter table state_datapoint rename column node_uuid to node_id;
alter table state_datapoint add constraint state_datapoint_node_id_fkey foreign key (node_id) references node (principal_id) on delete cascade;
alter table state_datapoint add constraint state_datapoint_owner_arc_check check (
       (owner_kind = 'component' and component_id is not null and system_id is null and location_id is null and node_id is null)
    or (owner_kind = 'system'    and system_id    is not null and component_id is null and location_id is null and node_id is null)
    or (owner_kind = 'location'  and location_id  is not null and component_id is null and system_id is null and node_id is null)
    or (owner_kind = 'node'      and node_id      is not null and component_id is null and system_id is null and location_id is null)
);

alter table event drop constraint if exists event_owner_arc_check;
alter table event add column if not exists node_uuid uuid;
update event e set node_uuid = n.principal_id from node n where n.name = e.node_id;
alter table event drop column node_id;
alter table event rename column node_uuid to node_id;
alter table event add constraint event_node_id_fkey foreign key (node_id) references node (principal_id) on delete cascade;
alter table event add constraint event_owner_arc_check check (
       (owner_kind = 'component' and component_id is not null and system_id is null and location_id is null and node_id is null)
    or (owner_kind = 'system'    and system_id    is not null and component_id is null and location_id is null and node_id is null)
    or (owner_kind = 'location'  and location_id  is not null and component_id is null and system_id is null and node_id is null)
    or (owner_kind = 'node'      and node_id      is not null and component_id is null and system_id is null and location_id is null)
);

alter table property_value drop constraint if exists property_value_owner_arc_check;
alter table property_value drop constraint if exists property_value_series_key;
alter table property_value add column if not exists node_uuid uuid;
update property_value v set node_uuid = n.principal_id from node n where n.name = v.node_id;
alter table property_value drop column node_id;
alter table property_value rename column node_uuid to node_id;
alter table property_value add constraint property_value_node_id_fkey foreign key (node_id) references node (principal_id) on delete cascade;
alter table property_value add constraint property_value_series_key unique nulls not distinct
    (owner_kind, component_id, system_id, location_id, node_id, property_name, instance, provenance);
alter table property_value add constraint property_value_owner_arc_check check (
       (owner_kind = 'component' and component_id is not null and system_id is null and location_id is null and node_id is null)
    or (owner_kind = 'system'    and system_id    is not null and component_id is null and location_id is null and node_id is null)
    or (owner_kind = 'location'  and location_id  is not null and component_id is null and system_id is null and node_id is null)
    or (owner_kind = 'node'      and node_id      is not null and component_id is null and system_id is null and location_id is null)
);

-- interface: its component arc and its node arc ------------------------------
alter table interface drop constraint if exists interface_component_name_key;
alter table interface add column if not exists component_uuid uuid, add column if not exists node_uuid uuid;
update interface i set component_uuid = c.id from component c where c.name = i.component;
update interface i set node_uuid = n.principal_id from node n where n.name = i.node_name;
alter table interface drop column component, drop column node_name;
alter table interface rename column component_uuid to component;
alter table interface rename column node_uuid to node_name;
alter table interface
    add constraint interface_component_fkey foreign key (component) references component (id) on delete set null,
    add constraint interface_node_name_fkey foreign key (node_name) references node (principal_id) on delete cascade,
    add constraint interface_component_name_key unique (component, name);
create index if not exists interface_node_name_idx on interface (node_name) where node_name is not null;

-- node's own placement -------------------------------------------------------
alter table node add column if not exists location_id uuid;
update node n set location_id = l.id from location l where l.name = n.location_name;
alter table node drop column location_name;
alter table node add constraint node_location_id_fkey foreign key (location_id) references location (id) on delete set null;

-- migrate:down

-- A real reversal. Every arc points at a row that still exists, so its current
-- name is the right answer, and unwinding further re-adds name-based foreign keys
-- that cannot be created against converted columns.

alter table node add column if not exists location_name text;
update node n set location_name = l.name from location l where l.id = n.location_id;
alter table node drop column location_id;
alter table node add constraint node_location_name_fkey foreign key (location_name) references location (name) on delete set null;

alter table interface drop constraint if exists interface_component_name_key;
alter table interface add column if not exists component_name text, add column if not exists node_nm text;
update interface i set component_name = c.name from component c where c.id = i.component;
update interface i set node_nm = n.name from node n where n.principal_id = i.node_name;
alter table interface drop column component, drop column node_name;
alter table interface rename column component_name to component;
alter table interface rename column node_nm to node_name;
alter table interface
    add constraint interface_component_fkey foreign key (component) references component (name) on delete set null,
    add constraint interface_node_name_fkey foreign key (node_name) references node (name) on delete cascade,
    add constraint interface_component_name_key unique (component, name);
create index if not exists interface_node_name_idx on interface (node_name) where node_name is not null;

alter table property_value drop constraint if exists property_value_owner_arc_check;
alter table property_value drop constraint if exists property_value_series_key;
alter table property_value add column if not exists node_nm text;
update property_value v set node_nm = n.name from node n where n.principal_id = v.node_id;
alter table property_value drop column node_id;
alter table property_value rename column node_nm to node_id;
alter table property_value add constraint property_value_node_id_fkey foreign key (node_id) references node (name) on delete cascade;
alter table property_value add constraint property_value_series_key unique nulls not distinct
    (owner_kind, component_id, system_id, location_id, node_id, property_name, instance, provenance);
alter table property_value add constraint property_value_owner_arc_check check (
       (owner_kind = 'component' and component_id is not null and system_id is null and location_id is null and node_id is null)
    or (owner_kind = 'system'    and system_id    is not null and component_id is null and location_id is null and node_id is null)
    or (owner_kind = 'location'  and location_id  is not null and component_id is null and system_id is null and node_id is null)
    or (owner_kind = 'node'      and node_id      is not null and component_id is null and system_id is null and location_id is null)
);

alter table event drop constraint if exists event_owner_arc_check;
alter table event add column if not exists node_nm text;
update event e set node_nm = n.name from node n where n.principal_id = e.node_id;
alter table event drop column node_id;
alter table event rename column node_nm to node_id;
alter table event add constraint event_node_id_fkey foreign key (node_id) references node (name) on delete cascade;
alter table event add constraint event_owner_arc_check check (
       (owner_kind = 'component' and component_id is not null and system_id is null and location_id is null and node_id is null)
    or (owner_kind = 'system'    and system_id    is not null and component_id is null and location_id is null and node_id is null)
    or (owner_kind = 'location'  and location_id  is not null and component_id is null and system_id is null and node_id is null)
    or (owner_kind = 'node'      and node_id      is not null and component_id is null and system_id is null and location_id is null)
);

alter table state_datapoint drop constraint if exists state_datapoint_owner_arc_check;
alter table state_datapoint add column if not exists node_nm text;
update state_datapoint d set node_nm = n.name from node n where n.principal_id = d.node_id;
alter table state_datapoint drop column node_id;
alter table state_datapoint rename column node_nm to node_id;
alter table state_datapoint add constraint state_datapoint_node_id_fkey foreign key (node_id) references node (name) on update cascade on delete cascade;
alter table state_datapoint add constraint state_datapoint_owner_arc_check check (
       (owner_kind = 'component' and component_id is not null and system_id is null and location_id is null and node_id is null)
    or (owner_kind = 'system'    and system_id    is not null and component_id is null and location_id is null and node_id is null)
    or (owner_kind = 'location'  and location_id  is not null and component_id is null and system_id is null and node_id is null)
    or (owner_kind = 'node'      and node_id      is not null and component_id is null and system_id is null and location_id is null)
);

alter table metric_datapoint drop constraint if exists metric_datapoint_owner_arc_check;
alter table metric_datapoint
    add column if not exists component_name text,
    add column if not exists system_name    text,
    add column if not exists location_name  text,
    add column if not exists node_nm        text;
update metric_datapoint d set component_name = c.name from component c where c.id = d.component_id;
update metric_datapoint d set system_name    = s.name from system    s where s.id = d.system_id;
update metric_datapoint d set location_name  = l.name from location  l where l.id = d.location_id;
update metric_datapoint d set node_nm        = n.name from node      n where n.principal_id = d.node_id;
alter table metric_datapoint drop column component_id, drop column system_id, drop column location_id, drop column node_id;
alter table metric_datapoint rename column component_name to component_id;
alter table metric_datapoint rename column system_name    to system_id;
alter table metric_datapoint rename column location_name  to location_id;
alter table metric_datapoint rename column node_nm        to node_id;
alter table metric_datapoint
    add constraint metric_datapoint_component_id_fkey foreign key (component_id) references component (name) on delete cascade,
    add constraint metric_datapoint_system_id_fkey    foreign key (system_id)    references system    (name) on delete cascade,
    add constraint metric_datapoint_location_id_fkey  foreign key (location_id)  references location  (name) on delete cascade,
    add constraint metric_datapoint_node_id_fkey      foreign key (node_id)      references node (name) on delete cascade;
create index if not exists metric_datapoint_owner_idx on metric_datapoint (component_id, key, instance, ts desc) where component_id is not null;
alter table metric_datapoint add constraint metric_datapoint_owner_arc_check check (
       (owner_kind = 'component' and component_id is not null and system_id is null and location_id is null and node_id is null)
    or (owner_kind = 'system'    and system_id    is not null and component_id is null and location_id is null and node_id is null)
    or (owner_kind = 'location'  and location_id  is not null and component_id is null and system_id is null and node_id is null)
    or (owner_kind = 'node'      and node_id      is not null and component_id is null and system_id is null and location_id is null)
);
