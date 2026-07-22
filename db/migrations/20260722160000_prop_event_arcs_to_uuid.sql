-- migrate:up

-- property_value and event keyed their owner arc by name, taken from the
-- collection-era tables without the pattern being questioned. Neither needs it:
-- nothing resolves a property or an event BY the owner's name, so the reference
-- had no reason to be the mutable column. Pointing at the uuid makes a rename a
-- single-row update.
--
-- The node arm keeps its name reference here and converts with the rest of the
-- collection tier, so the arc CHECK below still names node_id.

-- property_value ------------------------------------------------------------
alter table property_value drop constraint if exists property_value_owner_arc_check;
alter table property_value drop constraint if exists property_value_series_key;
alter table property_value
    add column if not exists component_uuid uuid,
    add column if not exists system_uuid    uuid,
    add column if not exists location_uuid  uuid;
update property_value v set component_uuid = c.id from component c where c.name = v.component_id;
update property_value v set system_uuid    = s.id from system    s where s.name = v.system_id;
update property_value v set location_uuid  = l.id from location  l where l.name = v.location_id;
alter table property_value drop column component_id, drop column system_id, drop column location_id;
alter table property_value rename column component_uuid to component_id;
alter table property_value rename column system_uuid    to system_id;
alter table property_value rename column location_uuid  to location_id;
alter table property_value
    add constraint property_value_component_id_fkey foreign key (component_id) references component (id) on delete cascade,
    add constraint property_value_system_id_fkey    foreign key (system_id)    references system    (id) on delete cascade,
    add constraint property_value_location_id_fkey  foreign key (location_id)  references location  (id) on delete cascade;
create index if not exists property_value_component_idx on property_value (component_id, property_name) where component_id is not null;
-- NULLS NOT DISTINCT is required exactly as before: the arc leaves three of the
-- four owner columns null, and under the default those nulls make every row
-- unique, so duplicates would slip through.
alter table property_value add constraint property_value_series_key unique nulls not distinct
    (owner_kind, component_id, system_id, location_id, node_id, property_name, instance, provenance);
alter table property_value add constraint property_value_owner_arc_check check (
       (owner_kind = 'component' and component_id is not null and system_id is null and location_id is null and node_id is null)
    or (owner_kind = 'system'    and system_id    is not null and component_id is null and location_id is null and node_id is null)
    or (owner_kind = 'location'  and location_id  is not null and component_id is null and system_id is null and node_id is null)
    or (owner_kind = 'node'      and node_id      is not null and component_id is null and system_id is null and location_id is null)
);

-- event ---------------------------------------------------------------------
alter table event drop constraint if exists event_owner_arc_check;
alter table event
    add column if not exists component_uuid uuid,
    add column if not exists system_uuid    uuid,
    add column if not exists location_uuid  uuid;
update event e set component_uuid = c.id from component c where c.name = e.component_id;
update event e set system_uuid    = s.id from system    s where s.name = e.system_id;
update event e set location_uuid  = l.id from location  l where l.name = e.location_id;
alter table event drop column component_id, drop column system_id, drop column location_id;
alter table event rename column component_uuid to component_id;
alter table event rename column system_uuid    to system_id;
alter table event rename column location_uuid  to location_id;
alter table event
    add constraint event_component_id_fkey foreign key (component_id) references component (id) on delete cascade,
    add constraint event_system_id_fkey    foreign key (system_id)    references system    (id) on delete cascade,
    add constraint event_location_id_fkey  foreign key (location_id)  references location  (id) on delete cascade;
create index if not exists event_owner_idx on event (component_id, key, instance, ts desc) where component_id is not null;
alter table event add constraint event_owner_arc_check check (
       (owner_kind = 'component' and component_id is not null and system_id is null and location_id is null and node_id is null)
    or (owner_kind = 'system'    and system_id    is not null and component_id is null and location_id is null and node_id is null)
    or (owner_kind = 'location'  and location_id  is not null and component_id is null and system_id is null and node_id is null)
    or (owner_kind = 'node'      and node_id      is not null and component_id is null and system_id is null and location_id is null)
);

-- migrate:down

-- One way, as with the migrations it follows: reversing would resolve uuids back
-- to names this migration no longer records.
select 1;
