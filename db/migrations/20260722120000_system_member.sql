-- migrate:up

-- Membership: the binding of a component to a system. A component that does a job
-- in a system is a member of it, and the role it fills is what that membership
-- DOES, one level down. Before this table the two facts lived apart: a single
-- component.system_id pointer (write-once, never read by health or authorization)
-- and role_assignment (many-to-many, what the component actually does), with
-- nothing reconciling them, so a fully staffed system could report zero components.
--
-- Membership is many-valued on purpose. A shared device (the rack DSP serving three
-- rooms, a codec spanning a divisible room) genuinely belongs to each of them, and
-- the health engine already fans out over exactly that shape.
--
-- is_primary marks which membership answers a question asked WITHOUT a system in
-- hand ("show me this component's config"). It is a default for context-free
-- callers, not a resolution rule: config that matters is resolved per membership.
-- A component with one membership never has to think about it.
--
-- Cascade on BOTH ends: a membership is a binding between two things, and it is
-- meaningless once either of them is gone. It deliberately does not restrict the
-- component the way role_assignment does. That restrict is load-bearing, since
-- deleting a component that fills a job would silently break a system's health,
-- but a membership carrying no role is an inventory fact, and refusing the delete
-- for it would add a step to every component removal while protecting nothing that
-- role_assignment does not already protect one level up.
create table if not exists system_member (
    id           uuid        primary key default uuidv7(),
    system_id    text        not null references system (name) on delete cascade,
    component_id text        not null references component (name) on delete cascade,
    is_primary   boolean     not null default false,
    created_at   timestamptz not null default now(),
    updated_at   timestamptz not null default now(),
    unique (system_id, component_id)
);
create index if not exists system_member_system_idx on system_member (system_id);
create index if not exists system_member_component_idx on system_member (component_id);

-- At most one primary per component, enforced by the database rather than by the
-- write path: "which one is the default" is a question that must have exactly one
-- answer, and a partial unique index is what makes a second one impossible instead
-- of merely unlikely.
create unique index if not exists system_member_one_primary_idx
    on system_member (component_id) where is_primary;

-- migrate:down

drop index if exists system_member_one_primary_idx;
drop table if exists system_member;
