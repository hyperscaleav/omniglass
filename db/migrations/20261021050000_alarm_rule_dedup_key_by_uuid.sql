-- migrate:up

-- One-time data migration (#977): an event rule's name became renameable,
-- so a rule-raised alarm's dedup key names the rule by its uuid
-- ("rule:<uuid>" or "rule:<uuid>:<instance>") instead of its name. Every
-- rule-raised alarm, open or cleared, is rewritten through raised_by_rule_id;
-- an alarm whose rule was deleted (raised_by_rule_id null) and a hand-raised
-- alarm keep the key they have. A name never looks like a uuid and holds no
-- colon, so a key already rewritten matches no name and a second run changes
-- nothing. No DDL.
UPDATE public.alarm a
SET dedup_key = 'rule:' || r.id::text || substr(a.dedup_key, length('rule:' || r.name) + 1)
FROM public.event_rule r
WHERE r.id = a.raised_by_rule_id
  AND (a.dedup_key = 'rule:' || r.name OR a.dedup_key LIKE 'rule:' || r.name || ':%');

-- migrate:down

-- Back to the name: a binary from before #977 keys a rule's alarms by it.
UPDATE public.alarm a
SET dedup_key = 'rule:' || r.name || substr(a.dedup_key, length('rule:' || r.id::text) + 1)
FROM public.event_rule r
WHERE r.id = a.raised_by_rule_id
  AND (a.dedup_key = 'rule:' || r.id::text OR a.dedup_key LIKE 'rule:' || r.id::text || ':%');
