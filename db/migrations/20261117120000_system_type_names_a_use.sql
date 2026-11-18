-- migrate:up
-- One-time backfill (#2067, ADR-2034): a system type names what a system is
-- for, never the space it sits in, so the shipped `room` system type becomes
-- `collaboration`.
--
-- A system type's name is the boot seed's key, and a system and each child
-- type hold it by uuid. Renaming the row in place keeps that uuid, so every
-- system typed under it and every child type stays attached, and the next
-- boot's seed updates the row instead of inserting a second one beside an
-- orphan. Where the new name is already taken (a database the new seed has
-- run on) the row is left alone. A fresh database has no old row, so this
-- does nothing there.
UPDATE system_type
   SET name = 'collaboration'
 WHERE namespace = 'og' AND name = 'room'
   AND NOT EXISTS (SELECT 1 FROM system_type WHERE namespace = 'og' AND name = 'collaboration');

-- migrate:down
-- A rename of seeded data has nothing to restore: the previous binary's seed
-- would insert its own row under the old name.
SELECT 1;
