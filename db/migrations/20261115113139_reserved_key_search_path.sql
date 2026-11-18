-- migrate:up
-- A reserved key is never shadowed (#1447, ADR-0318). ResolveNameLane refuses
-- a reserved key (a node-probe, platform or command-lifecycle key, the names
-- writerclass declares in code) that both local and og hold, so the ingest
-- registry writes neither; the search path below now agrees, so no reader
-- serves an operator's row of a name a later release reserved as the node's.
--
-- reserved_key mirrors writerclass.ReservedNames(): the boot seed reconciles it
-- to the code's list on every start, so the list is generated, never restated
-- here. Pure DDL: the table ships empty.
CREATE TABLE IF NOT EXISTS public.reserved_key (
  name text PRIMARY KEY
);

-- The three signal catalogs share one name space (ADR-1307): a local row in any
-- of them shadows an og row of the same name in every one, unless the name is
-- reserved and og holds it too, when neither tier answers. A reserved name
-- matches case-insensitively, as writerclass.Reserved does.
DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['metric_type', 'property_type', 'event_type']
  LOOP
    EXECUTE format($f$
      CREATE OR REPLACE FUNCTION %1$I() RETURNS SETOF %2$I LANGUAGE sql STABLE AS $b$
        SELECT t.* FROM %2$I t
        WHERE (t.namespace = 'local'
               AND NOT (EXISTS (SELECT 1 FROM reserved_key r WHERE r.name = lower(t.name))
                        AND (EXISTS (SELECT 1 FROM metric_type o WHERE o.namespace = 'og' AND o.name = t.name)
                             OR EXISTS (SELECT 1 FROM property_type o WHERE o.namespace = 'og' AND o.name = t.name)
                             OR EXISTS (SELECT 1 FROM event_type o WHERE o.namespace = 'og' AND o.name = t.name))))
           OR (t.namespace = 'og'
               AND NOT EXISTS (SELECT 1 FROM metric_type l WHERE l.namespace = 'local' AND l.name = t.name)
               AND NOT EXISTS (SELECT 1 FROM property_type l WHERE l.namespace = 'local' AND l.name = t.name)
               AND NOT EXISTS (SELECT 1 FROM event_type l WHERE l.namespace = 'local' AND l.name = t.name))
      $b$ $f$, t || '_in_effect', t);
  END LOOP;
END $$;

-- migrate:down
DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['metric_type', 'property_type', 'event_type']
  LOOP
    EXECUTE format($f$
      CREATE OR REPLACE FUNCTION %1$I() RETURNS SETOF %2$I LANGUAGE sql STABLE AS $b$
        SELECT t.* FROM %2$I t
        WHERE t.namespace = 'local'
           OR (t.namespace = 'og'
               AND NOT EXISTS (SELECT 1 FROM metric_type l WHERE l.namespace = 'local' AND l.name = t.name)
               AND NOT EXISTS (SELECT 1 FROM property_type l WHERE l.namespace = 'local' AND l.name = t.name)
               AND NOT EXISTS (SELECT 1 FROM event_type l WHERE l.namespace = 'local' AND l.name = t.name))
      $b$ $f$, t || '_in_effect', t);
  END LOOP;
END $$;
DROP TABLE IF EXISTS public.reserved_key;
