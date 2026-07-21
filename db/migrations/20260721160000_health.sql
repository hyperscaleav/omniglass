-- migrate:up

-- An alarm is component-local and names the capabilities it degrades. Capability
-- is the routing key for health: a role requires capabilities, a component
-- provides them, and an alarm takes some away, so an alarm reaches a system only
-- through the roles whose requirements it breaks.
create table if not exists alarm (
    id           uuid        primary key default uuidv7(),
    component_id text        not null references component (name) on delete cascade,
    severity     text        not null,
    message      text        not null default '',
    raised_at    timestamptz not null default now(),
    -- Null while the alarm is active. Clearing keeps the row, so the record of
    -- what was wrong and when survives the fix.
    cleared_at   timestamptz,
    created_at   timestamptz not null default now(),
    updated_at   timestamptz not null default now(),
    constraint alarm_severity_check check (severity in ('info', 'warning', 'critical'))
);
-- The active set is the hot read: every health recompute asks "what is wrong with
-- this component right now".
create index if not exists alarm_active_idx on alarm (component_id) where cleared_at is null;

create table if not exists alarm_capability (
    id            uuid        primary key default uuidv7(),
    alarm_id      uuid        not null references alarm (id) on delete cascade,
    capability_id text        not null references capability (id) on delete cascade,
    created_at    timestamptz not null default now(),
    unique (alarm_id, capability_id)
);
create index if not exists alarm_capability_capability_idx on alarm_capability (capability_id);

-- What an impaired role means for its system. It lives on the role because the
-- same broken component matters differently depending on the slot it was filling:
-- a dead confidence monitor is not a dead main display.
alter table system_role add column if not exists impact text not null default 'degraded';
alter table system_role drop constraint if exists system_role_impact_check;
alter table system_role add constraint system_role_impact_check check (impact in ('outage', 'degraded', 'none'));

-- migrate:down

alter table system_role drop constraint if exists system_role_impact_check;
alter table system_role drop column if exists impact;
drop table if exists alarm_capability;
drop table if exists alarm;
