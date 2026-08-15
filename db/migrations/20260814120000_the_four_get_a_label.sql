-- migrate:up

-- #613, the last four: `tag`, `variable`, `secret` and `interface` gain the
-- friendly string an operator reads.
--
-- Each of the four is key-bearing, so an operator types its name under the one
-- entity name rule (one segment, lowercase, digits, hyphens). ADR-0076 tightened
-- that rule and cited PagerDuty freezing a field's name and routing all renaming
-- pressure to the label beside it. The rule landed; the pressure valve did not,
-- so `cost_center` stopped being creatable and there was nowhere to write
-- "Cost Center". This is the valve.
--
-- `interface` is the strongest of the four rather than the weakest. Its name is
-- SERVER-derived (InterfaceSpec carries no Name; the column is set from
-- spec.Type), so on a component with three `ssh` interfaces an operator has no
-- way at all to say which is which. The label is the only operator-typed string
-- an interface has.
--
-- text, nullable, no default, exactly as the other twenty-three carry it after
-- ADR-0118: unset is SQL NULL and nothing else, enforced on the write side by
-- labelOrNull / labelPatch (internal/storage/label_unset.go) rather than by a
-- CHECK. No uniqueness, no pattern, no reserved words: a label is a label, and
-- the name goes on being the identifier.
--
-- No backfill. Existing rows come out unset and every surface renders their name
-- verbatim, never a re-cased or prettified version of it.
alter table tag       add column if not exists label text;
alter table variable  add column if not exists label text;
alter table secret    add column if not exists label text;
alter table interface add column if not exists label text;

-- migrate:down

alter table interface drop column if exists label;
alter table secret    drop column if exists label;
alter table variable  drop column if exists label;
alter table tag       drop column if exists label;
