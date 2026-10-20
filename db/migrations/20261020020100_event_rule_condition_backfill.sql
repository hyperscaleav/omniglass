-- migrate:up

-- One-time backfill (#949): a rule's condition in force is now its own row,
-- not the alarm it holds open. Every rule-raised alarm open at upgrade gets
-- its condition row, since it opened, so the next recovery ends the
-- condition and resolves the alarm with a clear event. The instance is the
-- tail of the alarm's dedup key ("rule:<name>" or "rule:<name>:<instance>";
-- a rule's name is create-only and holds no colon).
INSERT INTO public.event_rule_condition (rule_id, component_id, instance, since)
SELECT a.raised_by_rule_id, a.component_id,
    CASE WHEN a.dedup_key = 'rule:' || r.name THEN ''
         ELSE substr(a.dedup_key, length('rule:' || r.name || ':') + 1) END,
    a.raised_at
FROM public.alarm a
JOIN public.event_rule r ON r.id = a.raised_by_rule_id
WHERE a.cleared_at IS NULL
  AND (a.dedup_key = 'rule:' || r.name OR a.dedup_key LIKE 'rule:' || r.name || ':%')
ON CONFLICT (rule_id, component_id, instance) DO NOTHING;

-- migrate:down

-- Before #949 nothing wrote the condition table: an alarm's open state was
-- the condition. Back there, the table is empty again.
DELETE FROM public.event_rule_condition;
