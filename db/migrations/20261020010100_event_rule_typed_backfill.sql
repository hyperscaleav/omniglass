-- migrate:up

-- One-time backfill (#948): every rule written before rules named their
-- event types gets the generic pair, condition-started when its condition
-- starts and condition-ended when it ends. The boot seed owns those two
-- types and runs after migrations, so on an upgrade with rules to backfill
-- they are created here first (insert-if-absent; the next boot's seed takes
-- them over authoritatively). A database with no rules gets nothing here.
INSERT INTO public.event_type (name, label, description, official)
SELECT v.name, v.label, '', true
FROM (VALUES ('condition-started', 'Condition Started'), ('condition-ended', 'Condition Ended')) AS v(name, label)
WHERE EXISTS (SELECT 1 FROM public.event_rule WHERE fire_event_type_id IS NULL)
ON CONFLICT (name) DO NOTHING;

UPDATE public.event_rule
SET fire_event_type_id = (SELECT id FROM public.event_type WHERE name = 'condition-started'),
    clear_event_type_id = (SELECT id FROM public.event_type WHERE name = 'condition-ended')
WHERE fire_event_type_id IS NULL;

-- migrate:down

-- Back to a schema where every rule raises an alarm: an informational rule
-- takes the least severe level, so the restored NOT NULL holds, and is
-- disabled with it, so a rule its author meant never to page does not start
-- raising alarms on a rollback.
UPDATE public.event_rule
SET severity = (SELECT name FROM public.severity_level ORDER BY rank LIMIT 1), enabled = false
WHERE severity IS NULL;
