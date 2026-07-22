-- The cascade's least-specific binding tier is renamed from 'global' to
-- 'platform'. It is the same rung at the same precedence (band 0); only the
-- name moves. 'global' was overloaded: it named both this tier and a
-- ship-with floor that never existed as a row (internal/seed writes type
-- definitions, never bindings). The floor is now 'default', a property of the
-- type declaration and off the cascade entirely, so it has no value here.
-- Postgres cannot alter a CHECK in place, so each is dropped and re-added.
-- Idempotent: the data UPDATEs are no-ops on a second run and every DDL is
-- guarded.

-- migrate:up

-- variable ---------------------------------------------------------------
alter table variable drop constraint if exists variable_owner_kind_check;
alter table variable drop constraint if exists variable_owner_arc;

update variable set owner_kind = 'platform' where owner_kind = 'global';

alter table variable add constraint variable_owner_kind_check
  check (owner_kind in ('platform', 'component', 'system', 'location'));
alter table variable add constraint variable_owner_arc check (
    (owner_kind = 'platform'  and component_id is null     and system_id is null     and location_id is null) or
    (owner_kind = 'component' and component_id is not null and system_id is null     and location_id is null) or
    (owner_kind = 'system'    and system_id is not null    and component_id is null  and location_id is null) or
    (owner_kind = 'location'  and location_id is not null  and component_id is null  and system_id is null)
);

drop index if exists variable_global_name;
create unique index if not exists variable_platform_name on variable (name) where owner_kind = 'platform';

-- secret -----------------------------------------------------------------
alter table secret drop constraint if exists secret_owner_kind_check;
alter table secret drop constraint if exists secret_owner_arc;

update secret set owner_kind = 'platform' where owner_kind = 'global';

alter table secret add constraint secret_owner_kind_check
  check (owner_kind in ('platform', 'component', 'system', 'location'));
alter table secret add constraint secret_owner_arc check (
    (owner_kind = 'platform'  and component_id is null     and system_id is null     and location_id is null) or
    (owner_kind = 'component' and component_id is not null and system_id is null     and location_id is null) or
    (owner_kind = 'system'    and system_id is not null    and component_id is null  and location_id is null) or
    (owner_kind = 'location'  and location_id is not null  and component_id is null  and system_id is null)
);

drop index if exists secret_global_name;
create unique index if not exists secret_platform_name on secret (name) where owner_kind = 'platform';

-- tag_binding ------------------------------------------------------------
alter table tag_binding drop constraint if exists tag_binding_owner_kind_check;
alter table tag_binding drop constraint if exists tag_binding_owner_arc;

update tag_binding set owner_kind = 'platform' where owner_kind = 'global';

alter table tag_binding add constraint tag_binding_owner_kind_check
  check (owner_kind in ('platform', 'component', 'system', 'location', 'node'));
alter table tag_binding add constraint tag_binding_owner_arc check (
    (owner_kind = 'platform'  and component_id is null     and system_id is null    and location_id is null and node_id is null) or
    (owner_kind = 'component' and component_id is not null and system_id is null    and location_id is null and node_id is null) or
    (owner_kind = 'system'    and system_id is not null    and component_id is null and location_id is null and node_id is null) or
    (owner_kind = 'location'  and location_id is not null  and component_id is null and system_id is null   and node_id is null) or
    (owner_kind = 'node'      and node_id is not null      and component_id is null and system_id is null   and location_id is null)
);

drop index if exists tag_binding_global_key;
create unique index if not exists tag_binding_platform_key on tag_binding (tag_id) where owner_kind = 'platform';

-- setting_override -------------------------------------------------------
-- scope is the settings cascade level. 'global' becomes 'platform'; the
-- 'code' level is never a row (ADR-0033 recomputes it in memory), so the
-- rename to 'default' has no data component here.
update setting_override set scope = 'platform' where scope = 'global';

-- migrate:down

update setting_override set scope = 'global' where scope = 'platform';

drop index if exists tag_binding_platform_key;
alter table tag_binding drop constraint if exists tag_binding_owner_arc;
alter table tag_binding drop constraint if exists tag_binding_owner_kind_check;
update tag_binding set owner_kind = 'global' where owner_kind = 'platform';
alter table tag_binding add constraint tag_binding_owner_kind_check
  check (owner_kind in ('global', 'component', 'system', 'location', 'node'));
alter table tag_binding add constraint tag_binding_owner_arc check (
    (owner_kind = 'global'    and component_id is null     and system_id is null    and location_id is null and node_id is null) or
    (owner_kind = 'component' and component_id is not null and system_id is null    and location_id is null and node_id is null) or
    (owner_kind = 'system'    and system_id is not null    and component_id is null and location_id is null and node_id is null) or
    (owner_kind = 'location'  and location_id is not null  and component_id is null and system_id is null   and node_id is null) or
    (owner_kind = 'node'      and node_id is not null      and component_id is null and system_id is null   and location_id is null)
);
create unique index if not exists tag_binding_global_key on tag_binding (tag_id) where owner_kind = 'global';

drop index if exists secret_platform_name;
alter table secret drop constraint if exists secret_owner_arc;
alter table secret drop constraint if exists secret_owner_kind_check;
update secret set owner_kind = 'global' where owner_kind = 'platform';
alter table secret add constraint secret_owner_kind_check
  check (owner_kind in ('global', 'component', 'system', 'location'));
alter table secret add constraint secret_owner_arc check (
    (owner_kind = 'global'    and component_id is null     and system_id is null     and location_id is null) or
    (owner_kind = 'component' and component_id is not null and system_id is null     and location_id is null) or
    (owner_kind = 'system'    and system_id is not null    and component_id is null  and location_id is null) or
    (owner_kind = 'location'  and location_id is not null  and component_id is null  and system_id is null)
);
create unique index if not exists secret_global_name on secret (name) where owner_kind = 'global';

drop index if exists variable_platform_name;
alter table variable drop constraint if exists variable_owner_arc;
alter table variable drop constraint if exists variable_owner_kind_check;
update variable set owner_kind = 'global' where owner_kind = 'platform';
alter table variable add constraint variable_owner_kind_check
  check (owner_kind in ('global', 'component', 'system', 'location'));
alter table variable add constraint variable_owner_arc check (
    (owner_kind = 'global'    and component_id is null     and system_id is null     and location_id is null) or
    (owner_kind = 'component' and component_id is not null and system_id is null     and location_id is null) or
    (owner_kind = 'system'    and system_id is not null    and component_id is null  and location_id is null) or
    (owner_kind = 'location'  and location_id is not null  and component_id is null  and system_id is null)
);
create unique index if not exists variable_global_name on variable (name) where owner_kind = 'global';
