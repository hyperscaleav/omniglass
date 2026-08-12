-- migrate:up

-- One-time data backfill: location_type adopts the registry-fork primitive
-- (#703, ADR-0095), so the shipped rows become official rows the platform owns
-- and the boot seed can write them AUTHORITATIVELY. That is what makes a
-- withdrawal reach an estate: `on conflict (name) do nothing` left a shipped
-- value frozen at whatever the first boot wrote, so removing one from
-- location_types.yaml reached new installs only.
--
-- Two statements, and they belong in ONE migration rather than two. An estate
-- that ran the shadow write without the official flip would resolve reads
-- through the shadow while UpdateLocationType still took the direct-write leg
-- (that leg is chosen by `official`), so an operator's next edit would land on
-- the row and be masked by the stale shadow. The pair is the ownership change;
-- half of it is a corruption.
--
-- Telling an operator EDIT from a shipped value is the whole difficulty, and
-- the answer is not to compare columns against what this release ships. That
-- comparison cannot see the case the epic was filed about: a row holding a
-- value a PREVIOUS release shipped and this one withdrew (floor's name rule)
-- differs from today's YAML and looks exactly like an edit, so preserving it
-- would defeat the withdrawal this migration exists to enable. A row edited to
-- a value a later release happens to ship has the mirror problem, and neither
-- is decidable from the row.
--
-- The audit trail decides it instead, because it is a RECORD rather than an
-- inference: every operator write of a registry row goes through the gateway
-- and writes its audit row in the same transaction. A row with no `create` and
-- no `update` audit of its own has never been touched by an operator, so
-- whatever it holds is what some release shipped, and it is reconciled by the
-- next boot. A row that HAS one is the operator's version and is captured as a
-- shadow first, so the authoritative seed writes the official row underneath it
-- and their version keeps resolving on top.
--
-- `create` counts as well as `update` for a case that looks unlikely and is
-- reachable today: shipped location types are not official yet, so an operator
-- can DELETE one and create their own row under the same name. That row is
-- theirs from birth with no update to its name, and the seed is about to claim
-- the name. Capturing it is the same act as capturing an edit.
--
-- Bounded by the shipped names as of this release, which is a point-in-time
-- snapshot and correct in a migration that runs exactly once: an operator's own
-- location type must never receive a shadow, since nothing seeds it and the
-- overlay would mask its next direct write. The names are the four
-- location_types.yaml ships; a type added to that file later arrives on an
-- estate as a plain official insert with no ownership question to settle.
--
-- Known imprecision, and it is deliberately biased toward preserving: a
-- location type property or metric contract write also audits against
-- `location_type`, keyed on whichever reference the caller used. Addressed by
-- uuid it looks like an edit of the row here, and the row gets a shadow holding
-- the values it already had. Reads are identical either way; the cost is that
-- the row reads as forked until `:restore`. The reverse mistake, reverting an
-- edit, is not recoverable from the console at all.
--
-- Idempotent: the insert is ON CONFLICT DO NOTHING on the shadow's own key, and
-- the update writes a value it may already hold.

insert into registry_shadow (registry, row_id, image)
select 'location_type',
       lt.id,
       jsonb_build_object(
           'display_name', lt.display_name,
           'icon', lt.icon,
           'allowed_parent_types', to_jsonb(coalesce(lt.allowed_parent_types, '{}'::text[])),
           'label_rule', to_jsonb(lt.label_rule),
           'name_rule', lt.name_rule)
  from location_type lt
 where lt.name in ('campus', 'building', 'floor', 'room')
   and exists (select 1
                 from audit_log a
                where a.resource = 'location_type'
                  and a.resource_id = lt.id::text
                  and a.verb in ('create', 'update'))
    on conflict (registry, row_id) do nothing;

update location_type
   set official = true
 where name in ('campus', 'building', 'floor', 'room');

-- migrate:down

-- Hands the shipped rows back to the operator, which is what "before this
-- migration" meant: under the old ownership model an operator's version of a
-- shipped row IS the row, so the shadow's values are written BACK onto it
-- before the shadows go. Dropping them without the copy-back would revert every
-- edit this migration was written to preserve, and leaving them would overlay a
-- row the old code writes directly.
--
-- nullif on name_rule matters: the image spells "no rule" as JSON null, and
-- storing that literal in the column would read back as a rule with an empty
-- stem (a POSITIONAL type), which is a different answer from no rule at all.

update location_type lt
   set display_name         = coalesce(s.image ->> 'display_name', lt.display_name),
       icon                 = coalesce(s.image ->> 'icon', lt.icon),
       allowed_parent_types = coalesce(
           (select array(select jsonb_array_elements_text(s.image -> 'allowed_parent_types'))),
           lt.allowed_parent_types),
       label_rule           = s.image ->> 'label_rule',
       name_rule            = nullif(s.image -> 'name_rule', 'null'::jsonb)
  from registry_shadow s
 where s.registry = 'location_type'
   and s.row_id = lt.id;

delete from registry_shadow where registry = 'location_type';

update location_type
   set official = false
 where name in ('campus', 'building', 'floor', 'room');
