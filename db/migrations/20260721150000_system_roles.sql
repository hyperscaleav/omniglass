-- migrate:up

-- A system_role is a slot a system needs filled: a main display, a table
-- microphone. It is declared on the standard (and inherited live by every
-- conforming system) or directly on a system (ad-hoc, which is how a one-off
-- system gets roles at all), on the same exclusive-arc pattern property_value
-- uses for its owners.
create table if not exists system_role (
    id           uuid        primary key default uuidv7(),
    owner_kind   text        not null,
    standard_id  text        references standard (id) on delete cascade,
    system_id    text        references system (name) on delete cascade,
    name         text        not null,
    display_name text        not null,
    -- How many assigned components must satisfy the role. Staffing is visible
    -- without health: a role wanting 2 with 1 assigned is under-staffed today.
    -- The health verdict it feeds (impact) lands with the rollup that reads it.
    quorum       integer     not null default 1,
    created_at   timestamptz not null default now(),
    updated_at   timestamptz not null default now(),
    constraint system_role_owner_kind_check check (owner_kind in ('standard', 'system')),
    constraint system_role_quorum_check check (quorum >= 1),
    constraint system_role_owner_arc_check check (
           (owner_kind = 'standard' and standard_id is not null and system_id is null)
        or (owner_kind = 'system'   and system_id   is not null and standard_id is null)
    ),
    -- NULLS NOT DISTINCT: the arc leaves one owner column NULL, and the default
    -- NULLS DISTINCT would let duplicates through it.
    constraint system_role_name_key unique nulls not distinct (owner_kind, standard_id, system_id, name)
);
create index if not exists system_role_standard_idx on system_role (standard_id) where standard_id is not null;
create index if not exists system_role_system_idx on system_role (system_id) where system_id is not null;

-- What a role requires. The set is conjunctive: a component must provide EVERY
-- listed capability to fill the role.
create table if not exists role_capability (
    id            uuid        primary key default uuidv7(),
    role_id       uuid        not null references system_role (id) on delete cascade,
    capability_id text        not null references capability (id) on delete cascade,
    created_at    timestamptz not null default now(),
    unique (role_id, capability_id)
);
create index if not exists role_capability_capability_idx on role_capability (capability_id);

-- A component's own capability facts, layered over the ones its product declares:
-- present=true adds a capability the product does not claim, present=false
-- suppresses one it does. This is what lets a productless component be staffed
-- while the assignment guard stays strict, and it mirrors the contract-plus-
-- override shape declared properties already use.
create table if not exists component_capability (
    id            uuid        primary key default uuidv7(),
    component_id  text        not null references component (name) on delete cascade,
    capability_id text        not null references capability (id) on delete cascade,
    present       boolean     not null default true,
    created_at    timestamptz not null default now(),
    updated_at    timestamptz not null default now(),
    unique (component_id, capability_id)
);
create index if not exists component_capability_capability_idx on component_capability (capability_id);

-- Who fills the role, in this system. on delete restrict on the component: a
-- component staffing a role is load-bearing, so it cannot be deleted out from
-- under the system without the operator unassigning it first.
create table if not exists role_assignment (
    id           uuid        primary key default uuidv7(),
    system_id    text        not null references system (name) on delete cascade,
    role_id      uuid        not null references system_role (id) on delete cascade,
    component_id text        not null references component (name) on delete restrict,
    created_at   timestamptz not null default now(),
    unique (system_id, role_id, component_id)
);
create index if not exists role_assignment_system_idx on role_assignment (system_id);
create index if not exists role_assignment_component_idx on role_assignment (component_id);

-- migrate:down

drop table if exists role_assignment;
drop table if exists component_capability;
drop table if exists role_capability;
drop table if exists system_role;
