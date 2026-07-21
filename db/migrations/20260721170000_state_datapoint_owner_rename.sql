-- migrate:up

-- state_datapoint addresses its owner by NAME (the estate address, not the uuid),
-- and a name is renameable: the console offers it on every entity. The original
-- FKs cascaded on delete but said nothing about update, so once an owner had any
-- recorded state the rename it was offered failed on the foreign key.
--
-- Health made that reachable for every system: a system records its opening
-- verdict the moment it is created, so from the first write on, a rename would have
-- been refused. ON UPDATE CASCADE is what the name-as-address model always meant:
-- the history follows the entity it belongs to instead of pinning its name.
alter table state_datapoint drop constraint if exists state_datapoint_component_id_fkey;
alter table state_datapoint add constraint state_datapoint_component_id_fkey
    foreign key (component_id) references component (name) on update cascade on delete cascade;

alter table state_datapoint drop constraint if exists state_datapoint_system_id_fkey;
alter table state_datapoint add constraint state_datapoint_system_id_fkey
    foreign key (system_id) references system (name) on update cascade on delete cascade;

alter table state_datapoint drop constraint if exists state_datapoint_location_id_fkey;
alter table state_datapoint add constraint state_datapoint_location_id_fkey
    foreign key (location_id) references location (name) on update cascade on delete cascade;

alter table state_datapoint drop constraint if exists state_datapoint_node_id_fkey;
alter table state_datapoint add constraint state_datapoint_node_id_fkey
    foreign key (node_id) references node (name) on update cascade on delete cascade;

-- migrate:down

alter table state_datapoint drop constraint if exists state_datapoint_component_id_fkey;
alter table state_datapoint add constraint state_datapoint_component_id_fkey
    foreign key (component_id) references component (name) on delete cascade;

alter table state_datapoint drop constraint if exists state_datapoint_system_id_fkey;
alter table state_datapoint add constraint state_datapoint_system_id_fkey
    foreign key (system_id) references system (name) on delete cascade;

alter table state_datapoint drop constraint if exists state_datapoint_location_id_fkey;
alter table state_datapoint add constraint state_datapoint_location_id_fkey
    foreign key (location_id) references location (name) on delete cascade;

alter table state_datapoint drop constraint if exists state_datapoint_node_id_fkey;
alter table state_datapoint add constraint state_datapoint_node_id_fkey
    foreign key (node_id) references node (name) on delete cascade;
