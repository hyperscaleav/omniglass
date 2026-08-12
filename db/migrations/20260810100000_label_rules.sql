-- migrate:up

-- #682: a label rule renders a label.
--
-- Three additions, one per tier of the same idea.
--
--  1. The PEN on each labelled entity (display_name_generated), mirroring
--     name_generated's polarity: true means the platform owns the field. The
--     column keeps the display_name word rather than taking the label word
--     early; the rename of the whole family belongs to #613, which sweeps every
--     table carrying it, and naming the pen label_generated before that lands
--     would leave the pair internally inconsistent.
--
--  2. The RULE on each classifier (label_rule), nullable, where null means
--     "this tier has no opinion, ask the next one out". Same three-state
--     discipline component_type.stem already uses: null defers, a value
--     decides, and there is no separate boolean that could disagree with it.
--
--  3. The GLOBAL tier, one row per entity kind, in its own small table.
--
-- Defaulting the pen FALSE is the load-bearing choice on the entity columns.
-- Every row that predates this migration has a display_name an operator either
-- typed or left empty, and neither is a value the platform may claim and start
-- overwriting on the next unrelated write. The pen is handed to the platform
-- only by a write that goes through the generator: a create with no label, or a
-- clear. name_generated shipped with exactly the same reasoning.

alter table component add column if not exists display_name_generated boolean not null default false;
alter table system    add column if not exists display_name_generated boolean not null default false;
alter table location  add column if not exists display_name_generated boolean not null default false;

alter table component_type add column if not exists label_rule text;
alter table system_type    add column if not exists label_rule text;
alter table location_type  add column if not exists label_rule text;
alter table product        add column if not exists label_rule text;
alter table standard       add column if not exists label_rule text;

-- The global tier. One row per entity kind, and two columns rather than one,
-- which is the part worth reading twice.
--
-- default_template is boot-seed space: the rule this release ships, rewritten
-- authoritatively on every start (ON CONFLICT DO UPDATE) so a later release can
-- improve it. template is operator space: null until an operator overrides, and
-- nulling it again is restore-to-defaults.
--
-- One column could not be both. An authoritative boot seed over a single column
-- would silently stomp an operator's rule on the next restart, and a
-- seed-if-absent would freeze the shipped default at whatever the first boot
-- wrote. That is the same problem the registry fork (#655, ADR-0095) solves for
-- a shipped registry row, and the same answer: shipped values and operator
-- values live apart and reads resolve one over the other. It is expressed as
-- two columns rather than a registry_shadow row because this table's key is an
-- entity kind rather than a uuid, and because three rows do not earn an overlay
-- table of their own.
create table if not exists label_rule (
    entity_kind      text primary key,
    default_template text        not null default '',
    template         text,
    created_at       timestamptz not null default now(),
    updated_at       timestamptz not null default now()
);

-- The kinds that carry a label rule, as a constraint rather than a convention:
-- a rule for an entity kind nothing renders is a row with no reader, and the
-- resolver would never ask for it. Dropped first so a re-run is a no-op rather
-- than a duplicate-constraint error (Postgres has no ADD CONSTRAINT IF NOT
-- EXISTS). Node is deliberately absent: its name is a NATS subject kept
-- globally unique and it has no type and no parent, so it is not a labelled
-- entity in this sense.
alter table label_rule drop constraint if exists label_rule_entity_kind_check;
alter table label_rule add constraint label_rule_entity_kind_check
    check (entity_kind in ('component', 'system', 'location'));

-- migrate:down

drop table if exists label_rule;

alter table standard       drop column if exists label_rule;
alter table product        drop column if exists label_rule;
alter table location_type  drop column if exists label_rule;
alter table system_type    drop column if exists label_rule;
alter table component_type drop column if exists label_rule;

alter table location  drop column if exists display_name_generated;
alter table system    drop column if exists display_name_generated;
alter table component drop column if exists display_name_generated;
