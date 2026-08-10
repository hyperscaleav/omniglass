-- migrate:up

-- One-time backfill of the ordinal column added by
-- 20260810090000_component_ordinal.sql. Existing rows already carry both facts
-- this needs: name_generated says which names the platform owns, and those
-- names still have the digits it minted into them.
--
-- This is the one place the string surgery #681 removes from the Go is
-- legitimate, and the reason is the three-bucket rule: transforming existing
-- operator data is a one-time migration, run once, never a rule the running
-- system keeps applying. From here on the number is written at mint time.
--
-- A row this cannot read a number off is LEFT NULL rather than given one. A
-- name_generated row whose name has no trailing "-<digits>" is not evidence of
-- an ordinal the platform owns; it is a row whose number we cannot recover,
-- and NULL is exactly what "no ordinal the platform owns" means everywhere
-- else in this column. Inventing one (1, or a max-plus-one) would put a digit
-- on a bare render that appears nowhere in the entity's name, which is the
-- defect #654 closed. Such a row degrades the way every un-numbered row does:
-- its bare render leaves the segment alone, and the next :move, reclassify or
-- :resetName re-mints and records a real number.
--
-- The length guard is the same refusal in its other form: a digit run too long
-- to be an integer is not an ordinal this generator ever minted, and a
-- backfill must not fail the whole migration over one unreadable row.
--
-- Idempotent: "ordinal is null" makes a second run a no-op.

update component
   set ordinal = substring(name from '-([0-9]+)$')::integer
 where name_generated
   and ordinal is null
   and name ~ '-[0-9]+$'
   and length(substring(name from '-([0-9]+)$')) <= 9;

-- migrate:down

-- Not meaningfully reversible, and it does not need to be: the schema
-- migration's own down (applied after this one, going backwards) drops the
-- column outright, taking these values with it. Restoring NULLs here first
-- would be work no one can observe.
