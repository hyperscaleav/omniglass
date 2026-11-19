-- migrate:up
-- #2082 (ADR-2038): a rule fires on a component and endpoint and clears there;
-- while it has fired and not cleared, that endpoint is one of its firings.
-- Condition retires as a noun. The rename carries the rule's in-force table and
-- every constraint and index named for it (a constraint's backing index renames
-- with the constraint).
--
-- The whole migration is one transaction, so it is guarded once: a database
-- that already has the event_rule_firing table is left as it is.

DO $$
DECLARE
  r record;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.tables
                 WHERE table_schema = 'public' AND table_name = 'event_rule_condition') THEN
    RETURN;
  END IF;

  ALTER TABLE public.event_rule_condition RENAME TO event_rule_firing;

  FOR r IN
    SELECT con.conname
      FROM pg_constraint con
      JOIN pg_class cls ON cls.oid = con.conrelid
      JOIN pg_namespace n ON n.oid = cls.relnamespace
     WHERE n.nspname = 'public' AND cls.relname = 'event_rule_firing'
       AND con.conname ~ 'event_rule_condition'
  LOOP
    EXECUTE format('ALTER TABLE public.event_rule_firing RENAME CONSTRAINT %I TO %I', r.conname,
      replace(r.conname, 'event_rule_condition', 'event_rule_firing'));
  END LOOP;
  FOR r IN
    SELECT c.relname
      FROM pg_class c
      JOIN pg_namespace n ON n.oid = c.relnamespace
      JOIN pg_index i ON i.indexrelid = c.oid
      JOIN pg_class t ON t.oid = i.indrelid
     WHERE n.nspname = 'public' AND c.relkind = 'i' AND t.relname = 'event_rule_firing'
       AND c.relname ~ 'event_rule_condition'
  LOOP
    EXECUTE format('ALTER INDEX public.%I RENAME TO %I', r.relname,
      replace(r.relname, 'event_rule_condition', 'event_rule_firing'));
  END LOOP;
END $$;

-- The generic pair a rule writes renames in place, so every rule and every
-- event already pointing at it keeps its id: condition-started is rule-fired,
-- condition-ended is rule-cleared. A fresh database has neither row (the boot
-- seed lands the new names), and one that already has a new name is left as it
-- is.
UPDATE public.event_type SET name = 'rule-fired'
 WHERE name = 'condition-started'
   AND NOT EXISTS (SELECT 1 FROM public.event_type WHERE name = 'rule-fired');
UPDATE public.event_type SET name = 'rule-cleared'
 WHERE name = 'condition-ended'
   AND NOT EXISTS (SELECT 1 FROM public.event_type WHERE name = 'rule-cleared');

-- The alarm's one-open-row index is keyed on the alarm's identity
-- (component, dedup_key), not on a condition.
ALTER INDEX IF EXISTS public.alarm_open_condition_key RENAME TO alarm_open_identity_key;

-- migrate:down
ALTER INDEX IF EXISTS public.alarm_open_identity_key RENAME TO alarm_open_condition_key;

UPDATE public.event_type SET name = 'condition-started'
 WHERE name = 'rule-fired'
   AND NOT EXISTS (SELECT 1 FROM public.event_type WHERE name = 'condition-started');
UPDATE public.event_type SET name = 'condition-ended'
 WHERE name = 'rule-cleared'
   AND NOT EXISTS (SELECT 1 FROM public.event_type WHERE name = 'condition-ended');

DO $$
DECLARE
  r record;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.tables
                 WHERE table_schema = 'public' AND table_name = 'event_rule_firing') THEN
    RETURN;
  END IF;

  FOR r IN
    SELECT con.conname
      FROM pg_constraint con
      JOIN pg_class cls ON cls.oid = con.conrelid
      JOIN pg_namespace n ON n.oid = cls.relnamespace
     WHERE n.nspname = 'public' AND cls.relname = 'event_rule_firing'
       AND con.conname ~ 'event_rule_firing'
  LOOP
    EXECUTE format('ALTER TABLE public.event_rule_firing RENAME CONSTRAINT %I TO %I', r.conname,
      replace(r.conname, 'event_rule_firing', 'event_rule_condition'));
  END LOOP;
  FOR r IN
    SELECT c.relname
      FROM pg_class c
      JOIN pg_namespace n ON n.oid = c.relnamespace
      JOIN pg_index i ON i.indexrelid = c.oid
      JOIN pg_class t ON t.oid = i.indrelid
     WHERE n.nspname = 'public' AND c.relkind = 'i' AND t.relname = 'event_rule_firing'
       AND c.relname ~ 'event_rule_firing'
  LOOP
    EXECUTE format('ALTER INDEX public.%I RENAME TO %I', r.relname,
      replace(r.relname, 'event_rule_firing', 'event_rule_condition'));
  END LOOP;

  ALTER TABLE public.event_rule_firing RENAME TO event_rule_condition;
END $$;
