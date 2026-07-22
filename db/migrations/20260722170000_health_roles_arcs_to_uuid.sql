-- migrate:up

-- The health and roles cluster, from name keys to the uuid primary key. Grouped
-- by subsystem rather than table age: state_datapoint mirrors event and looks
-- like it belonged with that conversion, but the health recompute reads it
-- alongside role_assignment, alarm, and system_member, and that recompute is the
-- last code that deserves a second pass.
--
-- Every on-update-cascade goes with the conversion: nothing references a mutable
-- column any more, so a rename is a single-row update.

-- role_assignment -----------------------------------------------------------
alter table role_assignment add column if not exists component_uuid uuid, add column if not exists system_uuid uuid;
update role_assignment r set component_uuid = c.id from component c where c.name = r.component_id;
update role_assignment r set system_uuid    = s.id from system    s where s.name = r.system_id;
alter table role_assignment drop column component_id, drop column system_id;
alter table role_assignment rename column component_uuid to component_id;
alter table role_assignment rename column system_uuid    to system_id;
alter table role_assignment alter column component_id set not null, alter column system_id set not null;
-- restrict on the component is load-bearing: a component filling a job cannot be
-- deleted out from under the system that depends on it.
alter table role_assignment
    add constraint role_assignment_component_id_fkey foreign key (component_id) references component (id) on delete restrict,
    add constraint role_assignment_system_id_fkey    foreign key (system_id)    references system    (id) on delete cascade,
    add constraint role_assignment_system_id_role_id_component_id_key unique (system_id, role_id, component_id);
create index if not exists role_assignment_component_idx on role_assignment (component_id);
create index if not exists role_assignment_system_idx on role_assignment (system_id);

-- system_member --------------------------------------------------------------
alter table system_member add column if not exists component_uuid uuid, add column if not exists system_uuid uuid;
update system_member m set component_uuid = c.id from component c where c.name = m.component_id;
update system_member m set system_uuid    = s.id from system    s where s.name = m.system_id;
alter table system_member drop column component_id, drop column system_id;
alter table system_member rename column component_uuid to component_id;
alter table system_member rename column system_uuid    to system_id;
alter table system_member alter column component_id set not null, alter column system_id set not null;
alter table system_member
    add constraint system_member_component_id_fkey foreign key (component_id) references component (id) on delete cascade,
    add constraint system_member_system_id_fkey    foreign key (system_id)    references system    (id) on delete cascade,
    add constraint system_member_system_id_component_id_key unique (system_id, component_id);
create index if not exists system_member_component_idx on system_member (component_id);
create index if not exists system_member_system_idx on system_member (system_id);
-- At most one primary per component, still enforced by the database rather than
-- the write path.
create unique index if not exists system_member_one_primary_idx on system_member (component_id) where is_primary;

-- system_role ----------------------------------------------------------------
alter table system_role drop constraint if exists system_role_owner_arc_check;
alter table system_role drop constraint if exists system_role_name_key;
alter table system_role add column if not exists system_uuid uuid;
update system_role r set system_uuid = s.id from system s where s.name = r.system_id;
alter table system_role drop column system_id;
alter table system_role rename column system_uuid to system_id;
alter table system_role add constraint system_role_system_id_fkey foreign key (system_id) references system (id) on delete cascade;
create index if not exists system_role_system_idx on system_role (system_id) where system_id is not null;
-- NULLS NOT DISTINCT as before: the arc leaves one of the two owner columns null.
alter table system_role add constraint system_role_name_key unique nulls not distinct (owner_kind, standard_id, system_id, name);
alter table system_role add constraint system_role_owner_arc_check check (
       (owner_kind = 'standard' and standard_id is not null and system_id is null)
    or (owner_kind = 'system'   and system_id   is not null and standard_id is null)
);

-- component_capability -------------------------------------------------------
alter table component_capability drop constraint if exists component_capability_component_id_capability_id_key;
alter table component_capability add column if not exists component_uuid uuid;
update component_capability cc set component_uuid = c.id from component c where c.name = cc.component_id;
alter table component_capability drop column component_id;
alter table component_capability rename column component_uuid to component_id;
alter table component_capability alter column component_id set not null;
alter table component_capability
    add constraint component_capability_component_id_fkey foreign key (component_id) references component (id) on delete cascade,
    add constraint component_capability_component_id_capability_id_key unique (component_id, capability_id);

-- alarm ----------------------------------------------------------------------
alter table alarm add column if not exists component_uuid uuid;
update alarm a set component_uuid = c.id from component c where c.name = a.component_id;
alter table alarm drop column component_id;
alter table alarm rename column component_uuid to component_id;
alter table alarm alter column component_id set not null;
alter table alarm add constraint alarm_component_id_fkey foreign key (component_id) references component (id) on delete cascade;
-- The active set is the hot read for every health recompute.
create index if not exists alarm_active_idx on alarm (component_id) where cleared_at is null;

-- state_datapoint ------------------------------------------------------------
alter table state_datapoint drop constraint if exists state_datapoint_owner_arc_check;
alter table state_datapoint
    add column if not exists component_uuid uuid,
    add column if not exists system_uuid    uuid,
    add column if not exists location_uuid  uuid;
update state_datapoint d set component_uuid = c.id from component c where c.name = d.component_id;
update state_datapoint d set system_uuid    = s.id from system    s where s.name = d.system_id;
update state_datapoint d set location_uuid  = l.id from location  l where l.name = d.location_id;
alter table state_datapoint drop column component_id, drop column system_id, drop column location_id;
alter table state_datapoint rename column component_uuid to component_id;
alter table state_datapoint rename column system_uuid    to system_id;
alter table state_datapoint rename column location_uuid  to location_id;
alter table state_datapoint
    add constraint state_datapoint_component_id_fkey foreign key (component_id) references component (id) on delete cascade,
    add constraint state_datapoint_system_id_fkey    foreign key (system_id)    references system    (id) on delete cascade,
    add constraint state_datapoint_location_id_fkey  foreign key (location_id)  references location  (id) on delete cascade;
create index if not exists state_datapoint_owner_idx on state_datapoint (component_id, key, instance, ts desc) where component_id is not null;
alter table state_datapoint add constraint state_datapoint_owner_arc_check check (
       (owner_kind = 'component' and component_id is not null and system_id is null and location_id is null and node_id is null)
    or (owner_kind = 'system'    and system_id    is not null and component_id is null and location_id is null and node_id is null)
    or (owner_kind = 'location'  and location_id  is not null and component_id is null and system_id is null and node_id is null)
    or (owner_kind = 'node'      and node_id      is not null and component_id is null and system_id is null and location_id is null)
);

-- migrate:down

-- A real reversal, not a stub. Rolling back further re-adds the name-based
-- foreign keys these columns used to carry, and that cannot be done while they
-- hold uuids, so a no-op down breaks the chain rather than merely skipping work.
--
-- The direction is recoverable: every arc points at a row that still exists, so
-- its current name is the right answer. If the entity was renamed after the
-- forward migration, the reversal writes the new name, which is correct.

alter table state_datapoint drop constraint if exists state_datapoint_owner_arc_check;
alter table state_datapoint add column if not exists component_name text, add column if not exists system_name text, add column if not exists location_name text;
update state_datapoint d set component_name = c.name from component c where c.id = d.component_id;
update state_datapoint d set system_name    = s.name from system    s where s.id = d.system_id;
update state_datapoint d set location_name  = l.name from location  l where l.id = d.location_id;
alter table state_datapoint drop column component_id, drop column system_id, drop column location_id;
alter table state_datapoint rename column component_name to component_id;
alter table state_datapoint rename column system_name    to system_id;
alter table state_datapoint rename column location_name  to location_id;
alter table state_datapoint
    add constraint state_datapoint_component_id_fkey foreign key (component_id) references component (name) on update cascade on delete cascade,
    add constraint state_datapoint_system_id_fkey    foreign key (system_id)    references system    (name) on update cascade on delete cascade,
    add constraint state_datapoint_location_id_fkey  foreign key (location_id)  references location  (name) on update cascade on delete cascade;
create index if not exists state_datapoint_owner_idx on state_datapoint (component_id, key, instance, ts desc) where component_id is not null;
alter table state_datapoint add constraint state_datapoint_owner_arc_check check (
       (owner_kind = 'component' and component_id is not null and system_id is null and location_id is null and node_id is null)
    or (owner_kind = 'system'    and system_id    is not null and component_id is null and location_id is null and node_id is null)
    or (owner_kind = 'location'  and location_id  is not null and component_id is null and system_id is null and node_id is null)
    or (owner_kind = 'node'      and node_id      is not null and component_id is null and system_id is null and location_id is null)
);

alter table alarm add column if not exists component_name text;
update alarm a set component_name = c.name from component c where c.id = a.component_id;
alter table alarm drop column component_id;
alter table alarm rename column component_name to component_id;
alter table alarm alter column component_id set not null;
alter table alarm add constraint alarm_component_id_fkey foreign key (component_id) references component (name) on delete cascade;
create index if not exists alarm_active_idx on alarm (component_id) where cleared_at is null;

alter table component_capability drop constraint if exists component_capability_component_id_capability_id_key;
alter table component_capability add column if not exists component_name text;
update component_capability cc set component_name = c.name from component c where c.id = cc.component_id;
alter table component_capability drop column component_id;
alter table component_capability rename column component_name to component_id;
alter table component_capability alter column component_id set not null;
alter table component_capability
    add constraint component_capability_component_id_fkey foreign key (component_id) references component (name) on delete cascade,
    add constraint component_capability_component_id_capability_id_key unique (component_id, capability_id);

alter table system_role drop constraint if exists system_role_owner_arc_check;
alter table system_role drop constraint if exists system_role_name_key;
alter table system_role add column if not exists system_name text;
update system_role r set system_name = s.name from system s where s.id = r.system_id;
alter table system_role drop column system_id;
alter table system_role rename column system_name to system_id;
alter table system_role add constraint system_role_system_id_fkey foreign key (system_id) references system (name) on delete cascade;
create index if not exists system_role_system_idx on system_role (system_id) where system_id is not null;
alter table system_role add constraint system_role_name_key unique nulls not distinct (owner_kind, standard_id, system_id, name);
alter table system_role add constraint system_role_owner_arc_check check (
       (owner_kind = 'standard' and standard_id is not null and system_id is null)
    or (owner_kind = 'system'   and system_id   is not null and standard_id is null)
);

alter table system_member add column if not exists component_name text, add column if not exists system_name text;
update system_member m set component_name = c.name from component c where c.id = m.component_id;
update system_member m set system_name    = s.name from system    s where s.id = m.system_id;
alter table system_member drop column component_id, drop column system_id;
alter table system_member rename column component_name to component_id;
alter table system_member rename column system_name    to system_id;
alter table system_member alter column component_id set not null, alter column system_id set not null;
alter table system_member
    add constraint system_member_component_id_fkey foreign key (component_id) references component (name) on delete cascade,
    add constraint system_member_system_id_fkey    foreign key (system_id)    references system    (name) on delete cascade,
    add constraint system_member_system_id_component_id_key unique (system_id, component_id);
create index if not exists system_member_component_idx on system_member (component_id);
create index if not exists system_member_system_idx on system_member (system_id);
create unique index if not exists system_member_one_primary_idx on system_member (component_id) where is_primary;

alter table role_assignment add column if not exists component_name text, add column if not exists system_name text;
update role_assignment r set component_name = c.name from component c where c.id = r.component_id;
update role_assignment r set system_name    = s.name from system    s where s.id = r.system_id;
alter table role_assignment drop column component_id, drop column system_id;
alter table role_assignment rename column component_name to component_id;
alter table role_assignment rename column system_name    to system_id;
alter table role_assignment alter column component_id set not null, alter column system_id set not null;
alter table role_assignment
    add constraint role_assignment_component_id_fkey foreign key (component_id) references component (name) on delete restrict,
    add constraint role_assignment_system_id_fkey    foreign key (system_id)    references system    (name) on delete cascade,
    add constraint role_assignment_system_id_role_id_component_id_key unique (system_id, role_id, component_id);
create index if not exists role_assignment_component_idx on role_assignment (component_id);
create index if not exists role_assignment_system_idx on role_assignment (system_id);
