-- migrate:up
-- The search path (#1432, ADR-0318). A catalog's name is unique within its
-- namespace, not across them, so an operator's local/x and a shipped og/x can
-- both exist. severity_level keeps its install-wide unique name (levels are
-- overridden, never shadowed), so alarm and event_rule keep their name keys.
--
-- <table>_in_effect() is what a bare name resolves to: the operator's local
-- rows, then the og rows no local row of the same name shadows. Its body is
-- re-read on every call, so a column added to the table later is returned too.
DO $$
DECLARE
  t text;
  c text;
BEGIN
  FOREACH t IN ARRAY ARRAY[
    'metric_type', 'property_type', 'event_type', 'command_type',
    'component_type', 'system_type', 'location_type', 'secret_type',
    'notification_type', 'event_rule', 'calc_rule',
    'vendor', 'product', 'driver', 'standard', 'role']
  LOOP
    FOR c IN
      SELECT con.conname FROM pg_constraint con
      JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = ANY (con.conkey)
      WHERE con.conrelid = t::regclass AND con.contype = 'u'
        AND array_length(con.conkey, 1) = 1 AND a.attname = 'name'
    LOOP
      EXECUTE format('ALTER TABLE %I DROP CONSTRAINT %I', t, c);
    END LOOP;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = t || '_namespace_name_key') THEN
      EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I UNIQUE (namespace, name)', t, t || '_namespace_name_key');
    END IF;
  END LOOP;
END $$;

DROP INDEX IF EXISTS public.action_rule_platform_name_key;
CREATE UNIQUE INDEX IF NOT EXISTS action_rule_platform_name_key
  ON public.action_rule (namespace, name) WHERE owner_principal_id IS NULL;

-- The general rule: local, then the og rows local does not shadow.
DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY[
    'command_type', 'component_type', 'system_type', 'location_type', 'secret_type',
    'notification_type', 'event_rule', 'action_rule', 'calc_rule',
    'vendor', 'product', 'driver', 'standard']
  LOOP
    EXECUTE format($f$
      CREATE OR REPLACE FUNCTION %1$I() RETURNS SETOF %2$I LANGUAGE sql STABLE AS $b$
        SELECT t.* FROM %2$I t
        WHERE t.namespace = 'local'
           OR (t.namespace = 'og' AND NOT EXISTS (
                SELECT 1 FROM %2$I l WHERE l.namespace = 'local' AND l.name = t.name))
      $b$ $f$, t || '_in_effect', t);
  END LOOP;
END $$;

-- The three signal catalogs share one name space (ADR-1307): a local row in any
-- of them shadows an og row of the same name in every one.
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

-- Roles refuse ambiguity rather than shadow (#318 ruling): a bare role name
-- resolves only when exactly one of local and og holds it.
CREATE OR REPLACE FUNCTION role_in_effect() RETURNS SETOF role LANGUAGE sql STABLE AS $b$
  SELECT t.* FROM role t
  WHERE t.namespace IN ('local', 'og')
    AND NOT EXISTS (SELECT 1 FROM role o
                    WHERE o.namespace IN ('local', 'og') AND o.name = t.name AND o.id <> t.id)
$b$;

-- A shadow arises only when a release ships a name an operator already holds
-- (#1432): an operator's own create or rename never takes a name og holds, so
-- it is refused as the name's unique violation always was.
CREATE OR REPLACE FUNCTION catalog_local_name_free() RETURNS trigger LANGUAGE plpgsql AS $f$
DECLARE held boolean;
BEGIN
  IF NEW.namespace = 'local' AND (TG_OP = 'INSERT' OR NEW.name IS DISTINCT FROM OLD.name OR NEW.namespace IS DISTINCT FROM OLD.namespace) THEN
    -- EXECUTE does not set FOUND, so the answer is selected into a variable.
    EXECUTE format('SELECT EXISTS (SELECT 1 FROM %I WHERE namespace = %L AND name = $1 AND id <> $2)', TG_TABLE_NAME, 'og')
      INTO held USING NEW.name, NEW.id;
    IF held THEN
      RAISE EXCEPTION 'og/% already holds the name %', NEW.name, NEW.name
        USING ERRCODE = 'unique_violation', CONSTRAINT = TG_TABLE_NAME || '_namespace_name_key';
    END IF;
  END IF;
  RETURN NEW;
END $f$;

DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY[
    'metric_type', 'property_type', 'event_type', 'command_type',
    'component_type', 'system_type', 'location_type', 'secret_type',
    'notification_type', 'event_rule', 'action_rule', 'calc_rule',
    'vendor', 'product', 'driver', 'standard', 'role']
  LOOP
    EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I', t || '_local_name_free', t);
    EXECUTE format('CREATE TRIGGER %I BEFORE INSERT OR UPDATE OF name, namespace ON %I
      FOR EACH ROW EXECUTE FUNCTION catalog_local_name_free()', t || '_local_name_free', t);
  END LOOP;
END $$;

-- migrate:down
DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY[
    'metric_type', 'property_type', 'event_type', 'command_type',
    'component_type', 'system_type', 'location_type', 'secret_type',
    'notification_type', 'event_rule', 'action_rule', 'calc_rule',
    'vendor', 'product', 'driver', 'standard', 'role']
  LOOP
    EXECUTE format('DROP FUNCTION IF EXISTS %I()', t || '_in_effect');
    EXECUTE format('DROP TRIGGER IF EXISTS %I ON %I', t || '_local_name_free', t);
  END LOOP;
  FOREACH t IN ARRAY ARRAY[
    'metric_type', 'property_type', 'event_type', 'command_type',
    'component_type', 'system_type', 'location_type', 'secret_type',
    'notification_type', 'event_rule', 'calc_rule',
    'vendor', 'product', 'driver', 'standard', 'role']
  LOOP
    EXECUTE format('ALTER TABLE %I DROP CONSTRAINT IF EXISTS %I', t, t || '_namespace_name_key');
    EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I UNIQUE (name)', t, t || '_name_key');
  END LOOP;
END $$;
DROP INDEX IF EXISTS public.action_rule_platform_name_key;
CREATE UNIQUE INDEX IF NOT EXISTS action_rule_platform_name_key
  ON public.action_rule (name) WHERE owner_principal_id IS NULL;
DROP FUNCTION IF EXISTS catalog_local_name_free();
