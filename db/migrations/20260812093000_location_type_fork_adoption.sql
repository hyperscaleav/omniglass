-- migrate:up

-- One-time data backfill: location_type adopts the registry-fork primitive
-- (#703, ADR-0095), so the shipped rows become official rows the platform owns
-- and the boot seed can write them AUTHORITATIVELY. That is what makes a
-- withdrawal reach an estate: `on conflict (name) do nothing` left a shipped
-- value frozen at whatever the first boot wrote, so removing one from
-- location_types.yaml reached new installs only.
--
-- The flip is the whole migration. Nothing is preserved ahead of it because
-- there is nothing yet to preserve: Omniglass has no releases and no operator
-- data, so no estate holds an edit on a shipped location type (architect
-- ruling, 2026-08-12). An earlier draft told an edit from a shipped value by
-- the audit trail and captured it into registry_shadow first; ADR-0106 records
-- why that half was removed, and states plainly that the FIRST release changes
-- the calculus, so whoever ships one owes the estates it creates a migration
-- that reasons about their data rather than this one.
--
-- Bounded by the four names location_types.yaml ships as of this release, which
-- is a point-in-time snapshot and correct in a migration that runs exactly
-- once: an operator's own location type must never be claimed by the platform,
-- since nothing seeds it and an official flag would only take it away from
-- them. A type added to that file later arrives on an estate as a plain
-- official insert, with no ownership question to settle.
--
-- Idempotent: the update writes a value the row may already hold.

update location_type
   set official = true
 where name in ('campus', 'building', 'floor', 'room');

-- migrate:down

-- Hands the shipped rows back to the operator, which is what "before this
-- migration" meant: under the old ownership model an operator's version of a
-- shipped row IS the row, written in place, and the seed left it alone.

update location_type
   set official = false
 where name in ('campus', 'building', 'floor', 'room');
