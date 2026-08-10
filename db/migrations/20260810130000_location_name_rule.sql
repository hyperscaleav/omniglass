-- migrate:up

-- #687: a location_type opts in to generating the names of the locations it
-- classifies, and a location records the ordinal it was minted from.
--
-- name_rule is NULLABLE and null is the opt-in's "off": no rule means an
-- operator names every location of this type, which is the state every row is
-- in when this migration runs and the state the seeded campus, building and
-- room stay in. There is no separate boolean beside it, so "generates" and "the
-- rule" cannot disagree; the rule's presence IS the opt-in.
--
-- It is jsonb holding a DECLARATION, not a template: {"stem": "...",
-- "bare_first": <bool>}, decoded straight into the nameMint that
-- db/migrations/20260810120000_name_pen_spreads.sql's generator already tests
-- allocation against. An empty stem is a POSITIONAL type, whose ordinal
-- genuinely is its name (a floor called "1"). See ADR-0102 for why a
-- declaration rather than the label rule's text/template: a name has to satisfy
-- validateEntityName and lands in a unique index other rows reference, so it is
-- refused at rule-edit time by minting from it, which a template's output
-- cannot be.
--
-- The CHECK is a shape floor, not a schema: it refuses a scalar or an array
-- where an object belongs, so a hand-written row cannot make the decoder the
-- first thing to notice. The field-level rules (a legal stem, a mintable name)
-- are the gateway's, because they are the same rules the mint enforces and
-- there is one implementation of those (no DB logic).
--
-- location.ordinal is the column 20260810120000 deliberately withheld from this
-- table, for the reason it states: a location could not generate a name yet, so
-- the column had no writer and "a column no writer can fill is a fact waiting to
-- be read wrongly". This migration is that writer arriving. Nullable and
-- >= 1-checked exactly as component.ordinal and system.ordinal are: a name an
-- operator typed, and a name an operator renamed, have no number the platform
-- owns, and absent is how that is written down.
--
-- No backfill, deliberately, and the argument is the one both name-pen
-- migrations before this record: every location that exists when this runs was
-- named by an operator (nothing could generate one), so it holds the pen and
-- carries no ordinal. The defaults ARE the correct values for an existing row.
--
-- Constraints are dropped before they are added, since Postgres has no
-- ADD CONSTRAINT IF NOT EXISTS, so a re-run against a partially-migrated
-- database is a no-op rather than a duplicate-constraint error.

alter table location_type add column if not exists name_rule jsonb;

alter table location_type drop constraint if exists location_type_name_rule_check;
alter table location_type add constraint location_type_name_rule_check
    check (name_rule is null or jsonb_typeof(name_rule) = 'object');

alter table location add column if not exists ordinal integer;

alter table location drop constraint if exists location_ordinal_check;
alter table location add constraint location_ordinal_check check (ordinal is null or ordinal >= 1);

-- migrate:down

alter table location drop constraint if exists location_ordinal_check;
alter table location drop column if exists ordinal;
alter table location_type drop constraint if exists location_type_name_rule_check;
alter table location_type drop column if exists name_rule;
