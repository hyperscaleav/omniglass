-- migrate:up

-- One-time data backfill (#1155): command-settled, command-failed and
-- command-timed-out join the command-lifecycle class (ADR-1155, amending
-- ADR-1100), so only a command's own outcome writes one. Before this release
-- they were ordinary names: an operator could create such a type, a push or an
-- event rule could write events of it, and the boot seed's upsert now takes the
-- type over. An event of these types that no command points at
-- (command.settled_event_id) is moved, whole, into retired_sample with its
-- reason, as the #1100 backfill did for command-issued, so no read serves it
-- and its record survives. An event another row references (a derived event's
-- source, a sample's lineage) stays. Idempotent: a second run finds nothing.
WITH settled AS (
    SELECT DISTINCT settled_event_id AS id FROM public.command WHERE settled_event_id IS NOT NULL
), referenced AS (
    SELECT source_event_id AS id FROM public.event WHERE source_event_id IS NOT NULL
    UNION SELECT event_id FROM public.property WHERE event_id IS NOT NULL
    UNION SELECT event_id FROM public.metric WHERE event_id IS NOT NULL
    UNION SELECT caused_event_id FROM public.command WHERE caused_event_id IS NOT NULL
), gone AS (
    DELETE FROM public.event ev USING public.event_type t
    WHERE t.id = ev.event_type_id
      AND t.name IN ('command-settled', 'command-failed', 'command-timed-out')
      AND NOT EXISTS (SELECT 1 FROM settled s WHERE s.id = ev.id)
      AND NOT EXISTS (SELECT 1 FROM referenced r WHERE r.id = ev.id)
    RETURNING ev.*
)
INSERT INTO public.retired_sample (lane, sample_id, sample, reason)
SELECT 'event', g.id, to_jsonb(g), 'a command lifecycle event no command settled' FROM gone g
ON CONFLICT (lane, sample_id) DO NOTHING;

-- migrate:down

-- The events this backfill retired go back where they were, ids and all.
INSERT INTO public.event OVERRIDING SYSTEM VALUE
SELECT (jsonb_populate_record(NULL::public.event, r.sample)).*
FROM public.retired_sample r
WHERE r.lane = 'event' AND r.reason = 'a command lifecycle event no command settled'
ON CONFLICT (id) DO NOTHING;
DELETE FROM public.retired_sample
WHERE lane = 'event' AND reason = 'a command lifecycle event no command settled';
