-- migrate:up
-- Every catalog that carries `official` gains a namespace (#318, #1431): `og` is
-- the platform's and only the boot seed writes it; `local` is the operator's
-- default; an operator may use any other. A namespace is one kebab segment, like
-- a name. An `og` row is always official. Uniqueness stays on `name` until the
-- search path lands (#1432).
DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY[
    'metric_type', 'property_type', 'event_type', 'command_type',
    'component_type', 'system_type', 'location_type', 'secret_type',
    'severity_level', 'notification_type', 'event_rule', 'action_rule', 'calc_rule',
    'vendor', 'product', 'driver', 'standard', 'role']
  LOOP
    EXECUTE format('ALTER TABLE %I ADD COLUMN IF NOT EXISTS namespace text NOT NULL DEFAULT %L', t, 'local');
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = t || '_namespace_check') THEN
      EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK (namespace ~ %L)',
        t, t || '_namespace_check', '^[a-z0-9][a-z0-9-]*$');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = t || '_og_official_check') THEN
      EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I CHECK (namespace <> %L OR official) NOT VALID',
        t, t || '_og_official_check', 'og');
    END IF;
  END LOOP;
END $$;

-- migrate:down
DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY[
    'metric_type', 'property_type', 'event_type', 'command_type',
    'component_type', 'system_type', 'location_type', 'secret_type',
    'severity_level', 'notification_type', 'event_rule', 'action_rule', 'calc_rule',
    'vendor', 'product', 'driver', 'standard', 'role']
  LOOP
    EXECUTE format('ALTER TABLE %I DROP CONSTRAINT IF EXISTS %I', t, t || '_og_official_check');
    EXECUTE format('ALTER TABLE %I DROP CONSTRAINT IF EXISTS %I', t, t || '_namespace_check');
    EXECUTE format('ALTER TABLE %I DROP COLUMN IF EXISTS namespace', t);
  END LOOP;
END $$;
