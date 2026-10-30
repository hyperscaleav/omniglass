-- migrate:up
-- One-time backfill (#1431): a row the platform shipped (`official`) moves to
-- the `og` namespace; every other row is the operator's and stays `local`, the
-- column's default. Then the og-is-official check is validated.
DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY[
    'metric_type', 'property_type', 'event_type', 'command_type',
    'component_type', 'system_type', 'location_type', 'secret_type',
    'severity_level', 'notification_type', 'event_rule', 'action_rule', 'calc_rule',
    'vendor', 'product', 'driver', 'standard', 'role']
  LOOP
    EXECUTE format('UPDATE %I SET namespace = %L WHERE official AND namespace <> %L', t, 'og', 'og');
    EXECUTE format('ALTER TABLE %I VALIDATE CONSTRAINT %I', t, t || '_og_official_check');
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
    EXECUTE format('UPDATE %I SET namespace = %L WHERE namespace = %L', t, 'local', 'og');
  END LOOP;
END $$;
