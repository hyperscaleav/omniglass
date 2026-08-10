-- migrate:up

-- One-time backfill of the label pen added by 20260810100000_label_rules.sql
-- (#682), the counterpart of 20260810091000_component_ordinal_backfill.sql for
-- the analogous column on the name side.
--
-- The column defaults FALSE, which is right for a new column and wrong for
-- every row that already exists. The migration that added it justified the
-- default by saying an existing row's display_name was "either typed or left
-- empty, and neither is a value the platform may claim". That conflates the two
-- cases, and the second is exactly the one that is safe to claim: a row with no
-- label has nothing to protect, and it is the majority.
--
-- Left alone, such a row is inert forever. stampComponentLabel returns at its
-- first line when the pen is false, and #685's bulk recompute cannot repair it
-- either, since by construction a recompute only touches rows the platform
-- already owns. On an upgraded estate the whole feature would apply to rows
-- created after the upgrade and to nothing else.
--
-- So: claim the pen exactly where there is no operator value behind it, and
-- leave it where there is. Both spellings of "no label" count. NULL is what
-- every create writes (nullize turns an empty display_name into NULL), and the
-- empty string is what the previous UpdateComponent wrote for a caller clearing
-- the field, which under the new semantics is the gesture that hands the pen
-- back anyway.
--
-- This writes the PEN and not the label. Rendering needs the rule engine, which
-- is Go and not SQL (logic lives in Go, never the database), so these rows get
-- their labels from the next write that touches them or from #685's recompute,
-- which can now see them. That is the same division the ordinal backfill kept.
--
-- Idempotent: a second run matches the same rows and writes the same value.

update component
   set display_name_generated = true
 where not display_name_generated
   and (display_name is null or display_name = '');

update system
   set display_name_generated = true
 where not display_name_generated
   and (display_name is null or display_name = '');

update location
   set display_name_generated = true
 where not display_name_generated
   and (display_name is null or display_name = '');

-- migrate:down

-- Not meaningfully reversible, and it does not need to be: the schema
-- migration's own down (applied after this one, going backwards) drops the
-- column outright, taking these values with it.
